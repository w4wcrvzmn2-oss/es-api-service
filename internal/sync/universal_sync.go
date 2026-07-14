package sync

import (
	"context"
	"database/sql"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/robfig/cron/v3"
)

// UniversalSync представляет универсальный сервис синхронизации всех таблиц es_*
type UniversalSync struct {
	sourceDB *db.Database // База eplus_work
	targetDB *db.Database // База elfisa
	logger   *logger.Logger
	config   *config.Config
	cron     *cron.Cron
}

// TableInfo представляет информацию о таблице для синхронизации
type TableInfo struct {
	Name         string
	PrimaryKey   string
	HasDeleted   bool
	HasTimestamp bool
	Columns      []ColumnInfo
	ForeignKeys  []ForeignKeyInfo
	SyncOrder    int // Порядок синхронизации (0 - сначала, 1 - потом)
}

// ColumnInfo представляет информацию о колонке
type ColumnInfo struct {
	Name         string
	DataType     string
	IsNullable   bool
	MaxLength    int
	IsPrimaryKey bool
	IsForeignKey bool
}

// ForeignKeyInfo представляет информацию о внешнем ключе
type ForeignKeyInfo struct {
	Column    string
	RefTable  string
	RefColumn string
}

// NewUniversalSync создает новый универсальный сервис синхронизации
func NewUniversalSync(sourceDB, targetDB *db.Database, logger *logger.Logger, config *config.Config) *UniversalSync {
	return &UniversalSync{
		sourceDB: sourceDB,
		targetDB: targetDB,
		logger:   logger,
		config:   config,
		cron:     cron.New(),
	}
}

// StartScheduler запускает планировщик синхронизации
func (us *UniversalSync) StartScheduler() {
	// Расписание: каждый день в 03:00
	_, err := us.cron.AddFunc("0 3 * * *", func() {
		us.logger.Info("Запуск автоматической синхронизации всех таблиц es_* по расписанию")
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		if err := us.SyncAllTables(ctx); err != nil {
			us.logger.Error("Ошибка автоматической синхронизации таблиц es_*: %v", err)
		}
	})
	if err != nil {
		us.logger.Error("Ошибка добавления задачи в планировщик: %v", err)
		return
	}
	us.cron.Start()
	us.logger.Info("Планировщик синхронизации таблиц es_* запущен. Следующий запуск в 03:00.")
}

// Stop останавливает планировщик синхронизации
func (us *UniversalSync) Stop() {
	if us.cron != nil {
		us.cron.Stop()
		if us.logger != nil {
			us.logger.Info("Планировщик синхронизации таблиц es_* остановлен")
		}
	}
}

// SyncAllTables выполняет синхронизацию всех таблиц es_*
func (us *UniversalSync) SyncAllTables(ctx context.Context) error {
	us.logger.Info("Начало синхронизации всех таблиц es_* из eplus_work в elfisa")

	// Получаем список всех таблиц es_*
	tables, err := us.getESTables(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения списка таблиц es_*: %w", err)
	}

	us.logger.Info("Найдено %d таблиц es_* для синхронизации", len(tables))

	// Синхронизируем таблицы в правильном порядке
	for _, tableName := range tables {
		us.logger.Info("Синхронизация таблицы %s...", tableName)

		if err := us.syncTable(ctx, tableName); err != nil {
			us.logger.Error("Ошибка синхронизации таблицы %s: %v", tableName, err)
			continue
		}

		us.logger.Info("Таблица %s синхронизирована успешно", tableName)
	}

	us.logger.Info("Синхронизация всех таблиц es_* завершена")
	return nil
}

// getESTables получает список всех таблиц, начинающихся строго с ES_ (исключая ESHOP_ и ESKLP_)
func (us *UniversalSync) getESTables(ctx context.Context) ([]string, error) {
	// Получаем все таблицы, начинающиеся с ES
	query := `
		SELECT TABLE_NAME 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_TYPE = 'BASE TABLE' 
		  AND TABLE_NAME LIKE 'ES%'
		ORDER BY TABLE_NAME
	`

	rows, err := us.sourceDB.GORMWith(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса к sourceDB: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("ошибка сканирования имени таблицы: %w", err)
		}

		// Строгая фильтрация: только ES_* (исключаем ESHOP_* и ESKLP_*)
		// ES_ означает: ES + знак подчеркивания + любые символы
		if strings.HasPrefix(tableName, "ES_") &&
			!strings.HasPrefix(tableName, "ESHOP_") &&
			!strings.HasPrefix(tableName, "ESKLP_") {
			tables = append(tables, tableName)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по строкам: %w", err)
	}

	if us.logger != nil {
		us.logger.Info("Найдено %d таблиц ES_* для синхронизации (исключены ESHOP_* и ESKLP_*)", len(tables))
	}

	return tables, nil
}

// syncTable синхронизирует конкретную таблицу
func (us *UniversalSync) syncTable(ctx context.Context, tableName string) error {
	// Получаем информацию о структуре таблицы
	tableInfo, err := us.getTableInfo(ctx, tableName)
	if err != nil {
		return fmt.Errorf("ошибка получения информации о таблице %s: %w", tableName, err)
	}

	// Получаем данные из sourceDB
	sourceData, err := us.fetchTableData(ctx, tableName, tableInfo)
	if err != nil {
		return fmt.Errorf("ошибка получения данных из sourceDB для таблицы %s: %w", tableName, err)
	}

	us.logger.Info("Получено %d записей из таблицы %s", len(sourceData), tableName)

	// Синхронизируем с targetDB
	if err := us.updateTableData(ctx, tableName, sourceData, tableInfo); err != nil {
		return fmt.Errorf("ошибка обновления данных в targetDB для таблицы %s: %w", tableName, err)
	}

	return nil
}

// getTableInfo получает информацию о структуре таблицы
func (us *UniversalSync) getTableInfo(ctx context.Context, tableName string) (*TableInfo, error) {
	// Получаем колонки таблицы
	columnsQuery := `
		SELECT c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE, c.CHARACTER_MAXIMUM_LENGTH, 
		       CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END as IS_PRIMARY_KEY
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		) pk ON c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
		WHERE c.TABLE_NAME = @tableName
		ORDER BY c.ORDINAL_POSITION
	`

	rows, err := us.sourceDB.GORMWith(ctx).Raw(columnsQuery, sql.Named("tableName", tableName)).Rows()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения колонок таблицы %s: %w", tableName, err)
	}
	defer rows.Close()

	var columns []ColumnInfo
	var primaryKey string
	hasDeleted := false
	hasTimestamp := false

	for rows.Next() {
		var col ColumnInfo
		var isNullable string
		var isPrimaryKey int
		var maxLength sql.NullInt64

		err := rows.Scan(&col.Name, &col.DataType, &isNullable, &maxLength, &isPrimaryKey)
		if err != nil {
			return nil, fmt.Errorf("ошибка сканирования колонки: %w", err)
		}

		col.IsNullable = isNullable == "YES"
		col.IsPrimaryKey = isPrimaryKey == 1
		if maxLength.Valid {
			col.MaxLength = int(maxLength.Int64)
		} else {
			col.MaxLength = 0
		}

		if col.IsPrimaryKey {
			primaryKey = col.Name
		}

		if col.Name == "DELETED" {
			hasDeleted = true
		}

		if col.Name == "TS" {
			hasTimestamp = true
		}

		columns = append(columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по колонкам: %w", err)
	}

	// Получаем внешние ключи
	foreignKeys, err := us.getForeignKeys(ctx, tableName)
	if err != nil {
		us.logger.Warn("Не удалось получить внешние ключи для таблицы %s: %v", tableName, err)
	}

	// Определяем порядок синхронизации
	syncOrder := us.getSyncOrder(tableName)

	return &TableInfo{
		Name:         tableName,
		PrimaryKey:   primaryKey,
		HasDeleted:   hasDeleted,
		HasTimestamp: hasTimestamp,
		Columns:      columns,
		ForeignKeys:  foreignKeys,
		SyncOrder:    syncOrder,
	}, nil
}

// getForeignKeys получает информацию о внешних ключах таблицы
func (us *UniversalSync) getForeignKeys(ctx context.Context, tableName string) ([]ForeignKeyInfo, error) {
	query := `
		SELECT fk.COLUMN_NAME, pk.TABLE_NAME, pk.COLUMN_NAME
		FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE fk ON rc.CONSTRAINT_NAME = fk.CONSTRAINT_NAME
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE pk ON rc.UNIQUE_CONSTRAINT_NAME = pk.CONSTRAINT_NAME
		WHERE fk.TABLE_NAME = @tableName
	`

	rows, err := us.sourceDB.GORMWith(ctx).Raw(query, sql.Named("tableName", tableName)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var foreignKeys []ForeignKeyInfo
	for rows.Next() {
		var fk ForeignKeyInfo
		err := rows.Scan(&fk.Column, &fk.RefTable, &fk.RefColumn)
		if err != nil {
			return nil, err
		}
		foreignKeys = append(foreignKeys, fk)
	}

	return foreignKeys, nil
}

// getSyncOrder определяет порядок синхронизации таблицы
func (us *UniversalSync) getSyncOrder(tableName string) int {
	// Сначала синхронизируем основные справочники
	priorityTables := map[string]int{
		"ES_COUNTRY":          0,
		"ES_PRODUCER":         0,
		"ES_ATC":              0,
		"ES_FARMGROUP":        0,
		"ES_GROUP":            0,
		"ES_GOODS_CLASSIFIER": 0,
		"ES_INSTRUCTION":      0,
		"ES_PKU_LIST":         0,
		"ES_STORE_CONDITION":  0,
		"ES_SUPPLIER":         0,
		"ES_EF2":              1, // Препараты после справочников
	}

	if order, exists := priorityTables[tableName]; exists {
		return order
	}

	// Связующие таблицы синхронизируем в последнюю очередь
	if strings.HasPrefix(tableName, "ES_ES_2_") {
		return 2
	}

	// Остальные таблицы
	return 1
}

// fetchTableData получает данные из таблицы sourceDB
func (us *UniversalSync) fetchTableData(ctx context.Context, tableName string, tableInfo *TableInfo) ([]map[string]interface{}, error) {
	// Строим SELECT запрос
	columns := make([]string, 0, len(tableInfo.Columns))
	for _, col := range tableInfo.Columns {
		if col.DataType == "uniqueidentifier" {
			columns = append(columns, fmt.Sprintf("CAST(%s AS NVARCHAR(50)) as %s", col.Name, col.Name))
		} else {
			columns = append(columns, col.Name)
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), tableName)

	// Добавляем условие WHERE если есть поле DELETED
	if tableInfo.HasDeleted {
		query += " WHERE DELETED IS NULL"
	}

	rows, err := us.sourceDB.GORMWith(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса к sourceDB: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	columnNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения колонок: %w", err)
	}

	for rows.Next() {
		values := make([]interface{}, len(columnNames))
		valuePtrs := make([]interface{}, len(columnNames))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки: %w", err)
		}

		rowMap := make(map[string]interface{})
		for i, col := range columnNames {
			val := values[i]
			// Обработка NULL значений
			if b, ok := val.([]byte); ok {
				rowMap[col] = string(b)
			} else {
				rowMap[col] = val
			}
		}
		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка итерации по строкам: %w", err)
	}

	return results, nil
}

// tableExists проверяет существование таблицы в target базе
func (us *UniversalSync) tableExists(ctx context.Context, tableName string) bool {
	query := "SELECT COUNT(*) FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_NAME = @tableName"
	var count int
	err := us.targetDB.GORMWith(ctx).Raw(query, sql.Named("tableName", tableName)).Row().Scan(&count)
	return err == nil && count > 0
}

// updateTableData обновляет данные в таблице targetDB
func (us *UniversalSync) updateTableData(ctx context.Context, tableName string, data []map[string]interface{}, tableInfo *TableInfo) error {
	// Проверяем существование таблицы в target базе
	if !us.tableExists(ctx, tableName) {
		us.logger.Info("Таблица %s не существует в target базе, пропускаем синхронизацию", tableName)
		return nil
	}

	// Для ES_EF2 используем MERGE вместо DELETE, чтобы не нарушать внешние ключи
	if strings.ToUpper(tableName) == "ES_EF2" {
		return us.updateES_EF2WithMerge(ctx, tableName, data, tableInfo)
	}

	tx, err := us.targetDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ошибка начала транзакции для таблицы %s: %w", tableName, err)
	}
	defer tx.Rollback()

	// Для остальных таблиц используем стандартный подход: DELETE + INSERT
	// Очистка и вставка выполняются в одной транзакции, чтобы не оставить таблицу частично заполненной.
	_, err = tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", tableName))
	if err != nil {
		return fmt.Errorf("ошибка очистки таблицы %s: %w", tableName, err)
	}

	if len(data) == 0 {
		us.logger.Info("Таблица %s пуста, пропускаем вставку", tableName)
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("ошибка коммита очистки таблицы %s: %w", tableName, err)
		}
		return nil
	}

	// Строим INSERT запрос
	columns := make([]string, 0, len(tableInfo.Columns))
	placeholders := make([]string, 0, len(tableInfo.Columns))

	for _, col := range tableInfo.Columns {
		// Исключаем колонки, которые не должны синхронизироваться
		if col.Name == "TS" || col.Name == "timestamp" {
			continue // timestamp колонки не вставляются
		}
		if strings.HasSuffix(col.Name, "_at") || col.Name == "is_active" {
			continue // created_at, updated_at, is_active добавляются отдельно
		}

		columns = append(columns, col.Name)
		if col.DataType == "uniqueidentifier" {
			placeholders = append(placeholders, fmt.Sprintf("CAST(@%s AS UNIQUEIDENTIFIER)", col.Name))
		} else {
			placeholders = append(placeholders, fmt.Sprintf("@%s", col.Name))
		}
	}

	// Добавляем служебные поля
	columns = append(columns, "created_at", "updated_at", "is_active")
	placeholders = append(placeholders, "GETUTCDATE()", "GETUTCDATE()", "1")

	insertQuery := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	// Обрабатываем данные батчами по 1000 записей
	batchSize := 1000
	for i := 0; i < len(data); i += batchSize {
		end := i + batchSize
		if end > len(data) {
			end = len(data)
		}

		batch := data[i:end]

		batchStmt, err := tx.PrepareContext(ctx, insertQuery)
		if err != nil {
			return fmt.Errorf("ошибка подготовки запроса для батча: %w", err)
		}

		for _, row := range batch {
			// Подготавливаем параметры для запроса
			args := make([]sql.NamedArg, 0, len(tableInfo.Columns))
			for _, col := range tableInfo.Columns {
				// Исключаем те же колонки, что и в INSERT
				if col.Name == "TS" || col.Name == "timestamp" {
					continue
				}
				if strings.HasSuffix(col.Name, "_at") || col.Name == "is_active" {
					continue
				}
				args = append(args, sql.Named(col.Name, row[col.Name]))
			}

			// Преобразуем []sql.NamedArg в []interface{}
			interfaceArgs := make([]interface{}, len(args))
			for i, arg := range args {
				interfaceArgs[i] = arg
			}
			_, err := batchStmt.ExecContext(ctx, interfaceArgs...)
			if err != nil {
				batchStmt.Close()
				return fmt.Errorf("ошибка вставки записи в таблицу %s: %w", tableName, err)
			}
		}

		batchStmt.Close()

		us.logger.Info("Обработан батч %d-%d из %d записей для таблицы %s", i+1, end, len(data), tableName)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ошибка коммита таблицы %s: %w", tableName, err)
	}

	return nil
}

// updateES_EF2WithMerge обновляет ES_EF2 используя MERGE вместо DELETE
// Это необходимо, чтобы не нарушать внешние ключи из таблицы SupplierPrice
func (us *UniversalSync) updateES_EF2WithMerge(ctx context.Context, tableName string, data []map[string]interface{}, tableInfo *TableInfo) error {
	if len(data) == 0 {
		us.logger.Info("Таблица %s пуста, пропускаем синхронизацию", tableName)
		return nil
	}

	// Строим списки колонок для MERGE
	updateColumns := make([]string, 0)
	insertColumns := make([]string, 0)
	insertValues := make([]string, 0)

	for _, col := range tableInfo.Columns {
		// Исключаем служебные колонки
		if col.Name == "TS" || col.Name == "timestamp" {
			continue
		}
		if col.Name == "GUID_ES" {
			// GUID_ES используется только для сравнения в MERGE, не обновляется
			insertColumns = append(insertColumns, col.Name)
			if col.DataType == "uniqueidentifier" {
				insertValues = append(insertValues, "CAST(@GUID_ES AS UNIQUEIDENTIFIER)")
			} else {
				insertValues = append(insertValues, "@GUID_ES")
			}
			continue
		}
		if strings.HasSuffix(col.Name, "_at") || col.Name == "is_active" {
			// Эти поля обрабатываются отдельно
			continue
		}

		// Добавляем в UPDATE
		updateColumns = append(updateColumns, fmt.Sprintf("%s = @%s", col.Name, col.Name))

		// Добавляем в INSERT
		insertColumns = append(insertColumns, col.Name)
		if col.DataType == "uniqueidentifier" {
			insertValues = append(insertValues, fmt.Sprintf("CAST(@%s AS UNIQUEIDENTIFIER)", col.Name))
		} else {
			insertValues = append(insertValues, fmt.Sprintf("@%s", col.Name))
		}
	}

	// Добавляем служебные поля
	updateColumns = append(updateColumns, "updated_at = GETUTCDATE()", "is_active = 1")
	insertColumns = append(insertColumns, "created_at", "updated_at", "is_active")
	insertValues = append(insertValues, "GETUTCDATE()", "GETUTCDATE()", "1")

	// Строим MERGE запрос
	mergeQuery := fmt.Sprintf(`
		MERGE %s AS target
		USING (SELECT @GUID_ES AS GUID_ES) AS source
		ON target.GUID_ES = source.GUID_ES
		WHEN MATCHED THEN
			UPDATE SET %s
		WHEN NOT MATCHED THEN
			INSERT (%s) VALUES (%s);
	`, tableName, strings.Join(updateColumns, ", "), strings.Join(insertColumns, ", "), strings.Join(insertValues, ", "))

	// Обрабатываем данные батчами по 1000 записей
	// Для ES_EF2 используем минимальный батч и меньше потоков, чтобы не блокировать запросы сводного прайса
	batchSize := 1000
	if tableName == "ES_EF2" {
		batchSize = 500 // Минимальный размер батча для ES_EF2, чтобы минимизировать блокировки
	}

	// Количество параллельных потоков для обработки батчей
	// Для ES_EF2 используем 2 потока, чтобы не мешать работе сводного прайса
	maxWorkers := 4
	if tableName == "ES_EF2" {
		maxWorkers = 2 // Уменьшаем количество потоков для ES_EF2, чтобы не блокировать запросы
	}

	// Структура для передачи информации о батче
	type batchInfo struct {
		start int
		end   int
		data  []map[string]interface{}
	}

	// Создаем канал для батчей и канал для ограничения количества одновременных горутин
	batchChan := make(chan batchInfo, maxWorkers*2)

	// Функция обработки одного батча
	processBatch := func(batch batchInfo) error {
		// Начинаем транзакцию для батча с Read Committed изоляцией
		// Это позволяет другим запросам (сводный прайс) работать параллельно
		opts := &sql.TxOptions{
			Isolation: sql.LevelReadCommitted, // Read Committed - минимальные блокировки
		}
		tx, err := us.targetDB.BeginTx(ctx, opts)
		if err != nil {
			return fmt.Errorf("ошибка начала транзакции для батча %d-%d: %w", batch.start+1, batch.end, err)
		}

		batchStmt, err := tx.PrepareContext(ctx, mergeQuery)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка подготовки MERGE запроса для батча %d-%d: %w", batch.start+1, batch.end, err)
		}

		for _, row := range batch.data {
			// Подготавливаем параметры для запроса
			args := make([]interface{}, 0)
			for _, col := range tableInfo.Columns {
				// Исключаем те же колонки, что и в MERGE
				if col.Name == "TS" || col.Name == "timestamp" {
					continue
				}
				if strings.HasSuffix(col.Name, "_at") || col.Name == "is_active" {
					continue
				}
				args = append(args, sql.Named(col.Name, row[col.Name]))
			}

			_, err := batchStmt.ExecContext(ctx, args...)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка выполнения MERGE для записи %v в батче %d-%d: %w", row["GUID_ES"], batch.start+1, batch.end, err)
			}
		}

		batchStmt.Close()

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("ошибка коммита батча %d-%d для таблицы %s: %w", batch.start+1, batch.end, tableName, err)
		}

		// Для ES_EF2 добавляем паузу после коммита, чтобы дать другим запросам доступ к БД
		if tableName == "ES_EF2" {
			time.Sleep(150 * time.Millisecond) // 150мс пауза между батчами для минимизации блокировок
		}

		us.logger.Info("Обработан батч %d-%d из %d записей для таблицы %s (MERGE)", batch.start+1, batch.end, len(data), tableName)
		return nil
	}

	// Заполняем канал батчами
	go func() {
		defer close(batchChan)
		for i := 0; i < len(data); i += batchSize {
			end := i + batchSize
			if end > len(data) {
				end = len(data)
			}
			batchChan <- batchInfo{
				start: i,
				end:   end,
				data:  data[i:end],
			}
		}
	}()

	// Запускаем воркеры для параллельной обработки
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error

	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for batch := range batchChan {
				// Проверяем контекст перед обработкой
				select {
				case <-ctx.Done():
					mu.Lock()
					if firstError == nil {
						firstError = ctx.Err()
					}
					mu.Unlock()
					return
				default:
				}

				err := processBatch(batch)
				if err != nil {
					mu.Lock()
					if firstError == nil {
						firstError = err
					}
					mu.Unlock()
				}
			}
		}(w)
	}

	// Ждем завершения всех воркеров
	wg.Wait()

	if firstError != nil {
		return firstError
	}

	return nil
}
