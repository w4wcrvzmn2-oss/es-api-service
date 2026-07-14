package db

import (
	"context"
	"database/sql"
	"es_api_service/internal/config"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Database представляет соединение с базой данных.
// Содержит и *sql.DB (legacy raw SQL), и *gorm.DB поверх того же соединения —
// чтобы переход на GORM шёл инкрементально без второго пула коннектов.
type Database struct {
	db   *sql.DB
	gorm *gorm.DB
}

// wrapGorm создаёт *gorm.DB поверх уже открытого *sql.DB.
// Используем тот же физический пул соединений, никакого второго драйвера.
func wrapGorm(rawDB *sql.DB) (*gorm.DB, error) {
	return gorm.Open(sqlserver.New(sqlserver.Config{Conn: rawDB}), &gorm.Config{
		Logger:                                   gormlogger.Default.LogMode(gormlogger.Warn),
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: schema.NamingStrategy{
			// Имена таблиц/колонок в БД — PascalCase (BuyerUser.BuyerUserID),
			// а не snake_case. NoLowerCase отключает дефолтное приведение.
			NoLowerCase:   true,
			SingularTable: true,
		},
	})
}

// NewDatabase создаёт новое подключение к MS SQL Server
func NewDatabase(cfg *config.Config) (*Database, error) {
	// Формируем строку подключения напрямую для SQL Server
	// Это избегает проблем с URL encoding для именованных экземпляров
	var connStr string
	if cfg.DB.Port == 1433 {
		// Для именованного экземпляра используем только сервер
		connStr = fmt.Sprintf("server=%s;user id=%s;password=%s;database=%s;connection timeout=30;keepAlive=30",
			cfg.DB.Server, cfg.DB.User, cfg.DB.Password, cfg.DB.Database)
	} else {
		// Для обычного подключения с портом
		connStr = fmt.Sprintf("server=%s:%d;user id=%s;password=%s;database=%s;connection timeout=30;keepAlive=30",
			cfg.DB.Server, cfg.DB.Port, cfg.DB.User, cfg.DB.Password, cfg.DB.Database)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть соединение с БД: %w", err)
	}

	// Настройка пула соединений
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	gdb, err := wrapGorm(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("не удалось инициализировать GORM: %w", err)
	}

	return &Database{db: db, gorm: gdb}, nil
}

// NewSourceDatabase создаёт новое подключение к базе eplus_work для синхронизации
func NewSourceDatabase(cfg *config.Config) (*Database, error) {
	// Формируем строку подключения напрямую для SQL Server
	var connStr string
	if cfg.SourceDB.Port == 1433 {
		// Для именованного экземпляра используем только сервер
		connStr = fmt.Sprintf("server=%s;user id=%s;password=%s;database=%s;connection timeout=30;keepAlive=30",
			cfg.SourceDB.Server, cfg.SourceDB.User, cfg.SourceDB.Password, cfg.SourceDB.Database)
	} else {
		// Для обычного подключения с портом
		connStr = fmt.Sprintf("server=%s:%d;user id=%s;password=%s;database=%s;connection timeout=30;keepAlive=30",
			cfg.SourceDB.Server, cfg.SourceDB.Port, cfg.SourceDB.User, cfg.SourceDB.Password, cfg.SourceDB.Database)
	}

	db, err := sql.Open("sqlserver", connStr)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть соединение с source БД: %w", err)
	}

	// Настройка пула соединений
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	gdb, err := wrapGorm(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("не удалось инициализировать GORM (source): %w", err)
	}

	return &Database{db: db, gorm: gdb}, nil
}

// Ping проверяет соединение с базой данных
func (d *Database) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close закрывает соединение с базой данных
func (d *Database) Close() error {
	return d.db.Close()
}

// QueryRowsContext выполняет запрос и возвращает строки.
// Маршрутизирует через *gorm.DB, чтобы все запросы проходили GORM-stack
// (logger, callback'и). Драйвер и пул соединений — тот же *sql.DB.
func (d *Database) QueryRowsContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.gorm.WithContext(ctx).Raw(query, args...).Rows()
}

// gormExecResult адаптирует *gorm.DB.Exec под интерфейс sql.Result,
// чтобы legacy-код, ожидающий (sql.Result, error), мог работать через GORM.
type gormExecResult struct {
	rowsAffected int64
}

// LastInsertId — sql.Result.LastInsertId.
// MSSQL не поддерживает LastInsertId; legacy-код этим методом не пользовался.
func (r gormExecResult) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("LastInsertId не поддерживается через GORM-обёртку")
}

// RowsAffected — sql.Result.RowsAffected.
func (r gormExecResult) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// ColumnInfo содержит информацию о колонке
type ColumnInfo struct {
	Name     string
	DataType string
	IsGUID   bool
}

// GetTableColumns возвращает список колонок таблицы
func (d *Database) GetTableColumns(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT COLUMN_NAME 
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_NAME = @table_name 
		ORDER BY ORDINAL_POSITION
	`

	rows, err := d.db.QueryContext(ctx, query, sql.Named("table_name", tableName))
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

// GetTableColumnsWithTypes возвращает детальную информацию о колонках таблицы
func (d *Database) GetTableColumnsWithTypes(ctx context.Context, tableName string) ([]ColumnInfo, error) {
	query := `
		SELECT 
			COLUMN_NAME,
			DATA_TYPE,
			CASE WHEN DATA_TYPE = 'uniqueidentifier' THEN 1 ELSE 0 END as IS_GUID
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE TABLE_NAME = @table_name 
		ORDER BY ORDINAL_POSITION
	`

	rows, err := d.db.QueryContext(ctx, query, sql.Named("table_name", tableName))
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return columns, nil
}

// TableExists проверяет существование таблицы
func (d *Database) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_NAME = @table_name
	`

	var count int
	err := d.db.QueryRowContext(ctx, query, sql.Named("table_name", tableName)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("не удалось проверить существование таблицы %s: %w", tableName, err)
	}

	return count > 0, nil
}

// GetDB возвращает указатель на sql.DB для прямого доступа
func (d *Database) GetDB() *sql.DB {
	return d.db
}

// GORM возвращает *gorm.DB поверх того же *sql.DB.
// Использовать для нового кода; legacy raw-SQL продолжает работать через QueryContext/ExecContext.
func (d *Database) GORM() *gorm.DB {
	return d.gorm
}

// GORMWith возвращает *gorm.DB с привязкой к контексту запроса.
func (d *Database) GORMWith(ctx context.Context) *gorm.DB {
	return d.gorm.WithContext(ctx)
}

// QueryContext выполняет запрос с контекстом через GORM-stack.
func (d *Database) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return d.gorm.WithContext(ctx).Raw(query, args...).Rows()
}

// BeginTx начинает транзакцию.
// Транзакции делегируются *sql.DB напрямую — *sql.Tx работает внутри одного
// connection, и gorm.DB поверх того же пула не вмешивается. Для нового кода
// предпочтителен db.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {...}).
func (d *Database) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return d.db.BeginTx(ctx, opts)
}

// ExecContext выполняет команду через GORM-stack, возвращая адаптер sql.Result.
func (d *Database) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	res := d.gorm.WithContext(ctx).Exec(query, args...)
	if res.Error != nil {
		return nil, res.Error
	}
	return gormExecResult{rowsAffected: res.RowsAffected}, nil
}

// PrepareContext подготавливает запрос с контекстом.
// Prepared statements остаются на *sql.DB — GORM их не использует.
func (d *Database) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return d.db.PrepareContext(ctx, query)
}

// QueryRowContext выполняет запрос, возвращающий одну строку, через GORM-stack.
func (d *Database) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return d.gorm.WithContext(ctx).Raw(query, args...).Row()
}
