package sync

import (
	"context"
	"database/sql"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// DrugSync представляет сервис синхронизации справочника препаратов
type DrugSync struct {
	sourceDB    *db.Database // База eplus_work
	targetDB    *db.Database // База elfisa
	logger      *logger.Logger
	config      *config.Config
	cron        *cron.Cron
}

// DrugRecord представляет запись о препарате
type DrugRecord struct {
	GUID_ES                string
	NAME                   string
	BARCODE                *string
	CUREFORM_COD           *string
	CUREFORM_NAME          *string
	INN_NAME_RUS           *string
	INN_NAME_LAT           *string
	PRODUCER_COD           *int64
	TRN_NAME_RUS           *string
	TRN_NAME_LAT           *string
	UPAK_COD               *int64
	DATA_AN                *time.Time
	DATA_REG               *time.Time
	DOSAGE                 *string
	PR_JNVLS               *bool
	PR_LGOTA               *bool
	PR_NOT_RECEPT          *bool
	PR_OA                  *bool
	PR_BAD                 *bool
	SPISOK_AB              *string
	PR_PKKN                *bool
	N_PKKN                 *int32
	N_FV                   *int64
	UPAK_1                 *string
	UPAK_2                 *string
	UPAK_3                 *string
	KOL_1_UPAK             *int64
	KOL_CUREFORM           *int64
	KOL_VES                *float64
	KOL_VES_UNIT           *string
	SROK_SAVED             *string
	STORING_CONDITION      *string
	KOMPLEKTN              *string
	N_LICENSE              *string
	N_REG_UDOST            *string
	REGISTRATOR_COD        *int64
	REGISTR_STATUS         *string
	REESTR_PRICE           *float64
	KOD_ES                 int64
	NDS_RATE               float64
	GUID_ES_CHAR           *string
	ID_ES                  int64
	C_INSTRUCTION          *string
	DISCRIBE               *string
	RATING                 int32
	UPDATED                time.Time
	DELETED                *time.Time
	PACK_VOLUME            *float64
	PACK_WEIGHT            *float64
	BOX_PACK_QUANTITY      *int32
	IMMUNOBIOLOGICAL       *bool
	TS                     []byte
	GUID_PKU_LIST          *string
	IS_ALCOHOL_CONTENT     *bool
	IS_KIZ                 *bool
	GUID_GOODS_CLASSIFIER  *string
}

// NewDrugSync создает новый сервис синхронизации
func NewDrugSync(sourceDB, targetDB *db.Database, logger *logger.Logger, config *config.Config) *DrugSync {
	return &DrugSync{
		sourceDB: sourceDB,
		targetDB: targetDB,
		logger:   logger,
		config:   config,
		cron:     cron.New(),
	}
}

// StartScheduler запускает планировщик синхронизации
func (ds *DrugSync) StartScheduler() {
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
func (ds *DrugSync) SyncDrugs(ctx context.Context) error {
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

func (ds *DrugSync) fetchSourceData(ctx context.Context) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			GUID_ES, NAME, BARCODE, CUREFORM_COD, CUREFORM_NAME, INN_NAME_RUS, INN_NAME_LAT,
			PRODUCER_COD, TRN_NAME_RUS, TRN_NAME_LAT, UPAK_COD, DATA_AN, DATA_REG, DOSAGE,
			PR_JNVLS, PR_LGOTA, PR_NOT_RECEPT, PR_OA, PR_BAD, SPISOK_AB, PR_PKKN, N_PKKN, N_FV,
			UPAK_1, UPAK_2, UPAK_3, KOL_1_UPAK, KOL_CUREFORM, KOL_VES, KOL_VES_UNIT,
			SROK_SAVED, STORING_CONDITION, KOMPLEKTN, N_LICENSE, N_REG_UDOST, REGISTRATOR_COD,
			REGISTR_STATUS, REESTR_PRICE, KOD_ES, NDS_RATE, GUID_ES_CHAR, ID_ES, C_INSTRUCTION,
			DISCRIBE, RATING, UPDATED, DELETED, PACK_VOLUME, PACK_WEIGHT, BOX_PACK_QUANTITY,
			IMMUNOBIOLOGICAL, TS, GUID_PKU_LIST, IS_ALCOHOL_CONTENT, IS_KIZ, GUID_GOODS_CLASSIFIER
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

func (ds *DrugSync) updateTargetData(ctx context.Context, data []map[string]interface{}) error {
	// Используем INSERT ... ON CONFLICT для обновления существующих и вставки новых записей
	// Это безопасно, так как не нарушает внешние ключи
	upsertQuery := `
		INSERT INTO "es_ef2" (
			"GUID_ES", "NAME", "BARCODE", "CUREFORM_COD", "CUREFORM_NAME", "INN_NAME_RUS", "INN_NAME_LAT",
			"PRODUCER_COD", "TRN_NAME_RUS", "TRN_NAME_LAT", "UPAK_COD", "DATA_AN", "DATA_REG", "DOSAGE",
			"PR_JNVLS", "PR_LGOTA", "PR_NOT_RECEPT", "PR_OA", "PR_BAD", "SPISOK_AB", "PR_PKKN", "N_PKKN", "N_FV",
			"UPAK_1", "UPAK_2", "UPAK_3", "KOL_1_UPAK", "KOL_CUREFORM", "KOL_VES", "KOL_VES_UNIT",
			"SROK_SAVED", "STORING_CONDITION", "KOMPLEKTN", "N_LICENSE", "N_REG_UDOST", "REGISTRATOR_COD",
			"REGISTR_STATUS", "REESTR_PRICE", "KOD_ES", "NDS_RATE", "GUID_ES_CHAR", "ID_ES", "C_INSTRUCTION",
			"DISCRIBE", "RATING", "UPDATED", "DELETED", "PACK_VOLUME", "PACK_WEIGHT", "BOX_PACK_QUANTITY",
			"IMMUNOBIOLOGICAL", "TS", "GUID_PKU_LIST", "IS_ALCOHOL_CONTENT", "IS_KIZ", "GUID_GOODS_CLASSIFIER",
			"created_at", "updated_at", "is_active"
		) VALUES (
			CAST(@GUID_ES AS UUID), @NAME, @BARCODE, @CUREFORM_COD, @CUREFORM_NAME, @INN_NAME_RUS, @INN_NAME_LAT,
			@PRODUCER_COD, @TRN_NAME_RUS, @TRN_NAME_LAT, @UPAK_COD, @DATA_AN, @DATA_REG, @DOSAGE,
			@PR_JNVLS, @PR_LGOTA, @PR_NOT_RECEPT, @PR_OA, @PR_BAD, @SPISOK_AB, @PR_PKKN, @N_PKKN, @N_FV,
			@UPAK_1, @UPAK_2, @UPAK_3, @KOL_1_UPAK, @KOL_CUREFORM, @KOL_VES, @KOL_VES_UNIT,
			@SROK_SAVED, @STORING_CONDITION, @KOMPLEKTN, @N_LICENSE, @N_REG_UDOST, @REGISTRATOR_COD,
			@REGISTR_STATUS, @REESTR_PRICE, @KOD_ES, @NDS_RATE, @GUID_ES_CHAR, @ID_ES, @C_INSTRUCTION,
			@DISCRIBE, @RATING, @UPDATED, @DELETED, @PACK_VOLUME, @PACK_WEIGHT, @BOX_PACK_QUANTITY,
			@IMMUNOBIOLOGICAL, @TS, @GUID_PKU_LIST, @IS_ALCOHOL_CONTENT, @IS_KIZ, @GUID_GOODS_CLASSIFIER,
			(NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'), TRUE
		)
		ON CONFLICT ("GUID_ES") DO UPDATE SET
			"NAME" = EXCLUDED."NAME",
			"BARCODE" = EXCLUDED."BARCODE",
			"CUREFORM_COD" = EXCLUDED."CUREFORM_COD",
			"CUREFORM_NAME" = EXCLUDED."CUREFORM_NAME",
			"INN_NAME_RUS" = EXCLUDED."INN_NAME_RUS",
			"INN_NAME_LAT" = EXCLUDED."INN_NAME_LAT",
			"PRODUCER_COD" = EXCLUDED."PRODUCER_COD",
			"TRN_NAME_RUS" = EXCLUDED."TRN_NAME_RUS",
			"TRN_NAME_LAT" = EXCLUDED."TRN_NAME_LAT",
			"UPAK_COD" = EXCLUDED."UPAK_COD",
			"DATA_AN" = EXCLUDED."DATA_AN",
			"DATA_REG" = EXCLUDED."DATA_REG",
			"DOSAGE" = EXCLUDED."DOSAGE",
			"PR_JNVLS" = EXCLUDED."PR_JNVLS",
			"PR_LGOTA" = EXCLUDED."PR_LGOTA",
			"PR_NOT_RECEPT" = EXCLUDED."PR_NOT_RECEPT",
			"PR_OA" = EXCLUDED."PR_OA",
			"PR_BAD" = EXCLUDED."PR_BAD",
			"SPISOK_AB" = EXCLUDED."SPISOK_AB",
			"PR_PKKN" = EXCLUDED."PR_PKKN",
			"N_PKKN" = EXCLUDED."N_PKKN",
			"N_FV" = EXCLUDED."N_FV",
			"UPAK_1" = EXCLUDED."UPAK_1",
			"UPAK_2" = EXCLUDED."UPAK_2",
			"UPAK_3" = EXCLUDED."UPAK_3",
			"KOL_1_UPAK" = EXCLUDED."KOL_1_UPAK",
			"KOL_CUREFORM" = EXCLUDED."KOL_CUREFORM",
			"KOL_VES" = EXCLUDED."KOL_VES",
			"KOL_VES_UNIT" = EXCLUDED."KOL_VES_UNIT",
			"SROK_SAVED" = EXCLUDED."SROK_SAVED",
			"STORING_CONDITION" = EXCLUDED."STORING_CONDITION",
			"KOMPLEKTN" = EXCLUDED."KOMPLEKTN",
			"N_LICENSE" = EXCLUDED."N_LICENSE",
			"N_REG_UDOST" = EXCLUDED."N_REG_UDOST",
			"REGISTRATOR_COD" = EXCLUDED."REGISTRATOR_COD",
			"REGISTR_STATUS" = EXCLUDED."REGISTR_STATUS",
			"REESTR_PRICE" = EXCLUDED."REESTR_PRICE",
			"KOD_ES" = EXCLUDED."KOD_ES",
			"NDS_RATE" = EXCLUDED."NDS_RATE",
			"GUID_ES_CHAR" = EXCLUDED."GUID_ES_CHAR",
			"ID_ES" = EXCLUDED."ID_ES",
			"C_INSTRUCTION" = EXCLUDED."C_INSTRUCTION",
			"DISCRIBE" = EXCLUDED."DISCRIBE",
			"RATING" = EXCLUDED."RATING",
			"UPDATED" = EXCLUDED."UPDATED",
			"DELETED" = EXCLUDED."DELETED",
			"PACK_VOLUME" = EXCLUDED."PACK_VOLUME",
			"PACK_WEIGHT" = EXCLUDED."PACK_WEIGHT",
			"BOX_PACK_QUANTITY" = EXCLUDED."BOX_PACK_QUANTITY",
			"IMMUNOBIOLOGICAL" = EXCLUDED."IMMUNOBIOLOGICAL",
			"TS" = EXCLUDED."TS",
			"GUID_PKU_LIST" = EXCLUDED."GUID_PKU_LIST",
			"IS_ALCOHOL_CONTENT" = EXCLUDED."IS_ALCOHOL_CONTENT",
			"IS_KIZ" = EXCLUDED."IS_KIZ",
			"GUID_GOODS_CLASSIFIER" = EXCLUDED."GUID_GOODS_CLASSIFIER",
			"updated_at" = (NOW() AT TIME ZONE 'utc'),
			"is_active" = TRUE;
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
		
		batchStmt, err := db.PrepareRaw(ctx, tx, upsertQuery)
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
				sql.Named("PR_JNVLS", row["PR_JNVLS"]),
				sql.Named("PR_LGOTA", row["PR_LGOTA"]),
				sql.Named("PR_NOT_RECEPT", row["PR_NOT_RECEPT"]),
				sql.Named("PR_OA", row["PR_OA"]),
				sql.Named("PR_BAD", row["PR_BAD"]),
				sql.Named("SPISOK_AB", row["SPISOK_AB"]),
				sql.Named("PR_PKKN", row["PR_PKKN"]),
				sql.Named("N_PKKN", row["N_PKKN"]),
				sql.Named("N_FV", row["N_FV"]),
				sql.Named("UPAK_1", row["UPAK_1"]),
				sql.Named("UPAK_2", row["UPAK_2"]),
				sql.Named("UPAK_3", row["UPAK_3"]),
				sql.Named("KOL_1_UPAK", row["KOL_1_UPAK"]),
				sql.Named("KOL_CUREFORM", row["KOL_CUREFORM"]),
				sql.Named("KOL_VES", row["KOL_VES"]),
				sql.Named("KOL_VES_UNIT", row["KOL_VES_UNIT"]),
				sql.Named("SROK_SAVED", row["SROK_SAVED"]),
				sql.Named("STORING_CONDITION", row["STORING_CONDITION"]),
				sql.Named("KOMPLEKTN", row["KOMPLEKTN"]),
				sql.Named("N_LICENSE", row["N_LICENSE"]),
				sql.Named("N_REG_UDOST", row["N_REG_UDOST"]),
				sql.Named("REGISTRATOR_COD", row["REGISTRATOR_COD"]),
				sql.Named("REGISTR_STATUS", row["REGISTR_STATUS"]),
				sql.Named("REESTR_PRICE", row["REESTR_PRICE"]),
				sql.Named("KOD_ES", row["KOD_ES"]),
				sql.Named("NDS_RATE", row["NDS_RATE"]),
				sql.Named("GUID_ES_CHAR", row["GUID_ES_CHAR"]),
				sql.Named("ID_ES", row["ID_ES"]),
				sql.Named("C_INSTRUCTION", row["C_INSTRUCTION"]),
				sql.Named("DISCRIBE", row["DISCRIBE"]),
				sql.Named("RATING", row["RATING"]),
				sql.Named("UPDATED", row["UPDATED"]),
				sql.Named("DELETED", row["DELETED"]),
				sql.Named("PACK_VOLUME", row["PACK_VOLUME"]),
				sql.Named("PACK_WEIGHT", row["PACK_WEIGHT"]),
				sql.Named("BOX_PACK_QUANTITY", row["BOX_PACK_QUANTITY"]),
				sql.Named("IMMUNOBIOLOGICAL", row["IMMUNOBIOLOGICAL"]),
				sql.Named("TS", row["TS"]),
				sql.Named("GUID_PKU_LIST", row["GUID_PKU_LIST"]),
				sql.Named("IS_ALCOHOL_CONTENT", row["IS_ALCOHOL_CONTENT"]),
				sql.Named("IS_KIZ", row["IS_KIZ"]),
				sql.Named("GUID_GOODS_CLASSIFIER", row["GUID_GOODS_CLASSIFIER"]),
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
