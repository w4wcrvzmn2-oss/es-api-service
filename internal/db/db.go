package db

import (
	"context"
	"database/sql"
	"es_api_service/internal/config"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Database представляет соединение с базой данных.
// Содержит и *sql.DB (legacy raw SQL), и *gorm.DB поверх того же соединения.
type Database struct {
	db   *sql.DB
	gorm *gorm.DB
}

func wrapGorm(rawDB *sql.DB) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: rawDB}), &gorm.Config{
		Logger:                                   gormlogger.Default.LogMode(gormlogger.Warn),
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			// Имена таблиц/колонок в БД — PascalCase ("OrderItem", "SupplierPriceID").
			NoLowerCase:   true,
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, err
	}
	registerPascalQuoteCallbacks(gdb)
	return gdb, nil
}

// registerPascalQuoteCallbacks квотирует PascalCase в raw SQL до отправки в PostgreSQL.
func registerPascalQuoteCallbacks(gdb *gorm.DB) {
	quote := func(db *gorm.DB) {
		if db == nil || db.Statement == nil {
			return
		}
		sql := db.Statement.SQL.String()
		if sql == "" {
			return
		}
		fixed := PreparePGSQL(sql)
		if fixed == sql {
			return
		}
		db.Statement.SQL.Reset()
		db.Statement.SQL.WriteString(fixed)
	}
	_ = gdb.Callback().Query().Before("gorm:query").Register("pg:quote_pascal", quote)
	_ = gdb.Callback().Create().Before("gorm:create").Register("pg:quote_pascal", quote)
	_ = gdb.Callback().Update().Before("gorm:update").Register("pg:quote_pascal", quote)
	_ = gdb.Callback().Delete().Before("gorm:delete").Register("pg:quote_pascal", quote)
	_ = gdb.Callback().Row().Before("gorm:row").Register("pg:quote_pascal", quote)
	_ = gdb.Callback().Raw().Before("gorm:raw").Register("pg:quote_pascal", quote)
}

func postgresDSN(server string, port int, user, password, database, sslmode string) string {
	if port == 0 {
		port = 5432
	}
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", server, port),
		Path:   database,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

func openPostgres(dsn string, maxOpen, maxIdle int) (*Database, error) {
	_ = stdlib.GetDefaultDriver()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть соединение с БД: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(10 * time.Minute)

	// Ретрай пинга: после перезагрузки сервера PostgreSQL может подниматься
	// дольше, чем стартует наш сервис (даже с зависимостью службы «Running» у PG
	// ≠ «принимает коннекты»). Ждём готовности БД до ~90с, иначе сервис остаётся
	// без базы и отдаёт 503 на все данные.
	var pingErr error
	for attempt := 0; attempt < 30; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pingErr = db.PingContext(ctx)
		cancel()
		if pingErr == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("не удалось подключиться к PostgreSQL за 30 попыток: %w", pingErr)
	}

	gdb, err := wrapGorm(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("не удалось инициализировать GORM: %w", err)
	}

	return &Database{db: db, gorm: gdb}, nil
}

// NewDatabase создаёт подключение к операционной БД (elfisa) на PostgreSQL.
func NewDatabase(cfg *config.Config) (*Database, error) {
	dsn := postgresDSN(cfg.DB.Server, cfg.DB.Port, cfg.DB.User, cfg.DB.Password, cfg.DB.Database, cfg.DB.SSLMode)
	return openPostgres(dsn, cfg.DB.MaxOpenConns, cfg.DB.MaxIdleConns)
}

// NewSourceDatabase создаёт подключение к справочнику (eplus_work) на PostgreSQL.
func NewSourceDatabase(cfg *config.Config) (*Database, error) {
	dsn := postgresDSN(cfg.SourceDB.Server, cfg.SourceDB.Port, cfg.SourceDB.User, cfg.SourceDB.Password, cfg.SourceDB.Database, cfg.SourceDB.SSLMode)
	return openPostgres(dsn, cfg.SourceDB.MaxOpenConns, cfg.SourceDB.MaxIdleConns)
}

func (d *Database) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) QueryRowsContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.gorm.WithContext(ctx).Raw(query, args...).Rows()
}

type gormExecResult struct {
	rowsAffected int64
}

func (r gormExecResult) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("LastInsertId не поддерживается через GORM-обёртку")
}

func (r gormExecResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

type ColumnInfo struct {
	Name     string
	DataType string
	IsGUID   bool
}

func (d *Database) GetTableColumns(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position
	`
	rows, err := d.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить колонки таблицы %s: %w", tableName, err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func (d *Database) GetTableColumnsWithTypes(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT
			column_name,
			data_type,
			CASE WHEN udt_name = 'uuid' THEN 1 ELSE 0 END AS is_guid
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position
	`
	rows, err := d.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить информацию о колонках таблицы %s: %w", tableName, err)
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var isGUID int
		if err := rows.Scan(&col.Name, &col.DataType, &isGUID); err != nil {
			return nil, err
		}
		col.IsGUID = isGUID == 1
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func (d *Database) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	`
	var count int
	err := d.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("не удалось проверить существование таблицы %s: %w", tableName, err)
	}
	return count > 0, nil
}

func (d *Database) GetDB() *sql.DB {
	return d.db
}

func (d *Database) GORM() *gorm.DB {
	return d.gorm
}

func (d *Database) GORMWith(ctx context.Context) *gorm.DB {
	return d.gorm.WithContext(ctx)
}

func (d *Database) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.gorm.WithContext(ctx).Raw(query, args...).Rows()
}

func (d *Database) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, opts)
}

// Transaction выполняет fn в GORM-транзакции (с PreparePGSQL / ?-плейсхолдерами).
func (d *Database) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.gorm.WithContext(ctx).Transaction(fn)
}

// ExecRaw выполняет SQL на *sql.Tx с подстановкой ?/@name → $n и PreparePGSQL.
func ExecRaw(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) (sql.Result, error) {
	q, names := prepareRawSQLTemplate(query)
	return tx.ExecContext(ctx, q, bindRawArgs(names, args...)...)
}

// RawStmt — prepared statement с именованными/@ параметрами для pgx.
type RawStmt struct {
	stmt  *sql.Stmt
	names []string
}

// PrepareRaw готовит SQL (PreparePGSQL + $n) для *sql.DB или *sql.Tx.
func PrepareRaw(ctx context.Context, p interface {
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}, query string) (*RawStmt, error) {
	q, names := prepareRawSQLTemplate(query)
	stmt, err := p.PrepareContext(ctx, q)
	if err != nil {
		return nil, err
	}
	return &RawStmt{stmt: stmt, names: names}, nil
}

func (r *RawStmt) ExecContext(ctx context.Context, args ...interface{}) (sql.Result, error) {
	return r.stmt.ExecContext(ctx, bindRawArgs(r.names, args...)...)
}

func (r *RawStmt) Close() error {
	if r == nil || r.stmt == nil {
		return nil
	}
	return r.stmt.Close()
}

// QuoteIdent оборачивает имя таблицы/колонки в двойные кавычки PostgreSQL.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func prepareRawSQL(query string, args ...interface{}) (string, []interface{}) {
	q, names := prepareRawSQLTemplate(query)
	return q, bindRawArgs(names, args...)
}

func prepareRawSQLTemplate(query string) (string, []string) {
	query = PreparePGSQL(query)
	var names []string
	var b strings.Builder
	b.Grow(len(query) + 16)
	posIdx := 0
	i := 0
	for i < len(query) {
		c := query[i]
		if c == '\'' {
			b.WriteByte(c)
			i++
			for i < len(query) {
				b.WriteByte(query[i])
				if query[i] == '\'' {
					i++
					if i < len(query) && query[i] == '\'' {
						b.WriteByte(query[i])
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < len(query) {
				b.WriteByte(query[i])
				if query[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '?' {
			posIdx++
			b.WriteString(fmt.Sprintf("$%d", posIdx))
			names = append(names, "")
			i++
			continue
		}
		if c == '@' && i+1 < len(query) && isIdentByte(query[i+1]) {
			j := i + 1
			for j < len(query) && isIdentByte(query[j]) {
				j++
			}
			name := query[i+1 : j]
			posIdx++
			b.WriteString(fmt.Sprintf("$%d", posIdx))
			names = append(names, name)
			i = j
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), names
}

func bindRawArgs(names []string, args ...interface{}) []interface{} {
	named := map[string]interface{}{}
	positional := make([]interface{}, 0, len(args))
	for _, a := range args {
		if n, ok := a.(sql.NamedArg); ok {
			named[n.Name] = n.Value
			continue
		}
		positional = append(positional, a)
	}
	out := make([]interface{}, 0, len(names))
	pi := 0
	for _, name := range names {
		if name == "" {
			if pi < len(positional) {
				out = append(out, positional[pi])
				pi++
			} else {
				out = append(out, nil)
			}
			continue
		}
		if v, ok := named[name]; ok {
			out = append(out, v)
		} else {
			out = append(out, nil)
		}
	}
	return out
}

func (d *Database) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	res := d.gorm.WithContext(ctx).Exec(query, args...)
	if res.Error != nil {
		return nil, res.Error
	}
	return gormExecResult{rowsAffected: res.RowsAffected}, nil
}

func (d *Database) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return d.db.PrepareContext(ctx, query)
}

func (d *Database) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.gorm.WithContext(ctx).Raw(query, args...).Row()
}
