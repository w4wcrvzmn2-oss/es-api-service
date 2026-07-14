package sync

import (
	"context"
	"database/sql"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"fmt"
	"time"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/robfig/cron/v3"
)

// DrugSyncSimple представляет упрощенный сервис синхронизации справочника препаратов
type DrugSyncSimple struct {
	sourceDB *db.Database // База eplus_work
	targetDB *db.Database // База elfisa
	logger   *logger.Logger
	config   *config.Config
	cron     *cron.Cron
}

// NewDrugSyncSimple создает новый упрощенный сервис синхронизации
func NewDrugSyncSimple(sourceDB, targetDB *db.Database, logger *logger.Logger, config *config.Config) *DrugSyncSimple {
	return &DrugSyncSimple{
		sourceDB: sourceDB,
		targetDB: targetDB,
		logger:   logger,
		config:   config,
		cron:     cron.New(),
	}
}

// StartScheduler запускает планировщик синхронизации
func (ds *DrugSyncSimple) StartScheduler() {
	// Расписание: каждый день в 03:00
	_, err := ds.cron.AddFunc("0 3 * * *", func() {
		ds.logger.Info("Запуск автоматической синхронизации справочника препаратов по расписанию")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := ds.SyncDrugs(ctx); err != nil {
			ds.logger.Error("Ошибка автоматической синхронизации справочника препаратов: %v", err)
		}
	})
	if err != nil {
		ds.logger.Error("Ошибка добавления задачи в планировщик: %v", err)
		return
	}
	ds.cron.Start()
	ds.logger.Info("Планировщик синхронизации справочника препаратов запущен. Следующий запуск в 03:00.")
}

// SyncDrugs выполняет синхронизацию справочника препаратов
func (ds *DrugSyncSimple) SyncDrugs(ctx context.Context) error {
	ds.logger.Info("Начало синхронизации справочника препаратов из eplus_work.es_ef2 в elfisa.es_ef2")

	// 1. Получаем данные из sourceDB (eplus_work.es_ef2)
	sourceData, err := ds.fetchSourceData(ctx)
	if err != nil {
		return fmt.Errorf("ошибка получения данных из sourceDB: %w", err)
	}
	ds.logger.Info("Получено %d записей из sourceDB", len(sourceData))

	// 2. Обновляем/вставляем данные в targetDB (elfisa.es_ef2)
	if err := ds.updateTargetData(ctx, sourceData); err != nil {
		return fmt.Errorf("ошибка обновления данных в targetDB: %w", err)
	}

	ds.logger.Info("Синхронизация справочника препаратов завершена успешно")
	return nil
}

func (ds *DrugSyncSimple) fetchSourceData(ctx context.Context) ([]map[string]interface{}, error) {
	// Получаем только основные поля для упрощения
	query := `
		SELECT 
			CAST(GUID_ES AS NVARCHAR(50)) as GUID_ES, NAME, BARCODE, CUREFORM_COD, CUREFORM_NAME, INN_NAME_RUS, INN_NAME_LAT,
			PRODUCER_COD, TRN_NAME_RUS, TRN_NAME_LAT, UPAK_COD, DATA_AN, DATA_REG, DOSAGE,
			KOD_ES, NDS_RATE, ID_ES, DISCRIBE, RATING, UPDATED
		FROM es_ef2 
		WHERE DELETED IS NULL
		ORDER BY UPDATED
	`
	rows, err := ds.sourceDB.GORMWith(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса к sourceDB: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения колонок sourceDB: %w", err)
	}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("ошибка сканирования строки sourceDB: %w", err)
		}

		rowMap := make(map[string]interface{})
		for i, col := range columns {
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
		return nil, fmt.Errorf("ошибка итерации по строкам sourceDB: %w", err)
	}

	return results, nil
}

func (ds *DrugSyncSimple) updateTargetData(ctx context.Context, data []map[string]interface{}) error {
	// Используем MERGE для обновления существующих и вставки новых записей
	// Это безопасно, так как не нарушает внешние ключи
	mergeQuery := `
		MERGE es_ef2 AS target
		USING (SELECT @GUID_ES AS GUID_ES) AS source
		ON target.GUID_ES = source.GUID_ES
		WHEN MATCHED THEN
			UPDATE SET
				NAME = @NAME,
				BARCODE = @BARCODE,
				CUREFORM_COD = @CUREFORM_COD,
				CUREFORM_NAME = @CUREFORM_NAME,
				INN_NAME_RUS = @INN_NAME_RUS,
				INN_NAME_LAT = @INN_NAME_LAT,
				PRODUCER_COD = @PRODUCER_COD,
				TRN_NAME_RUS = @TRN_NAME_RUS,
				TRN_NAME_LAT = @TRN_NAME_LAT,
				UPAK_COD = @UPAK_COD,
				DATA_AN = @DATA_AN,
				DATA_REG = @DATA_REG,
				DOSAGE = @DOSAGE,
				KOD_ES = @KOD_ES,
				NDS_RATE = @NDS_RATE,
				ID_ES = @ID_ES,
				DISCRIBE = @DISCRIBE,
				RATING = @RATING,
				UPDATED = @UPDATED,
				updated_at = GETUTCDATE(),
				is_active = 1
		WHEN NOT MATCHED THEN
			INSERT (
				GUID_ES, NAME, BARCODE, CUREFORM_COD, CUREFORM_NAME, INN_NAME_RUS, INN_NAME_LAT,
				PRODUCER_COD, TRN_NAME_RUS, TRN_NAME_LAT, UPAK_COD, DATA_AN, DATA_REG, DOSAGE,
				KOD_ES, NDS_RATE, ID_ES, DISCRIBE, RATING, UPDATED,
				created_at, updated_at, is_active
			) VALUES (
				CAST(@GUID_ES AS UNIQUEIDENTIFIER), @NAME, @BARCODE, @CUREFORM_COD, @CUREFORM_NAME, @INN_NAME_RUS, @INN_NAME_LAT,
				@PRODUCER_COD, @TRN_NAME_RUS, @TRN_NAME_LAT, @UPAK_COD, @DATA_AN, @DATA_REG, @DOSAGE,
				@KOD_ES, @NDS_RATE, @ID_ES, @DISCRIBE, @RATING, @UPDATED,
				GETUTCDATE(), GETUTCDATE(), 1
			);
	`

	// Обрабатываем данные батчами по 1000 записей
	batchSize := 1000
	for i := 0; i < len(data); i += batchSize {
		end := i + batchSize
		if end > len(data) {
			end = len(data)
		}

		batch := data[i:end]

		// Начинаем транзакцию для батча
		tx, err := ds.targetDB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("ошибка начала транзакции для батча: %w", err)
		}

		batchStmt, err := tx.PrepareContext(ctx, mergeQuery)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка подготовки запроса для батча: %w", err)
		}

		for _, row := range batch {
			_, err := batchStmt.ExecContext(ctx,
				sql.Named("GUID_ES", row["GUID_ES"]),
				sql.Named("NAME", row["NAME"]),
				sql.Named("BARCODE", row["BARCODE"]),
				sql.Named("CUREFORM_COD", row["CUREFORM_COD"]),
				sql.Named("CUREFORM_NAME", row["CUREFORM_NAME"]),
				sql.Named("INN_NAME_RUS", row["INN_NAME_RUS"]),
				sql.Named("INN_NAME_LAT", row["INN_NAME_LAT"]),
				sql.Named("PRODUCER_COD", row["PRODUCER_COD"]),
				sql.Named("TRN_NAME_RUS", row["TRN_NAME_RUS"]),
				sql.Named("TRN_NAME_LAT", row["TRN_NAME_LAT"]),
				sql.Named("UPAK_COD", row["UPAK_COD"]),
				sql.Named("DATA_AN", row["DATA_AN"]),
				sql.Named("DATA_REG", row["DATA_REG"]),
				sql.Named("DOSAGE", row["DOSAGE"]),
				sql.Named("KOD_ES", row["KOD_ES"]),
				sql.Named("NDS_RATE", row["NDS_RATE"]),
				sql.Named("ID_ES", row["ID_ES"]),
				sql.Named("DISCRIBE", row["DISCRIBE"]),
				sql.Named("RATING", row["RATING"]),
				sql.Named("UPDATED", row["UPDATED"]),
			)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("ошибка вставки записи %v: %w", row["GUID_ES"], err)
			}
		}

		batchStmt.Close()

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("ошибка коммита батча: %w", err)
		}

		ds.logger.Info("Обработан батч %d-%d из %d записей", i+1, end, len(data))
	}

	return nil
}
