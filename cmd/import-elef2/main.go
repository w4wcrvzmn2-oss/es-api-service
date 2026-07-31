// Одноразовый импорт es_ef2 из текстового экспорта SSMS (elef2.txt).
// Запуск на сервере PostgreSQL:
//   import_elf2.exe -file D:\es_api_service\data\elef2.txt -cfg D:\es_api_service\es_api_service.cfg
package main

import (
	"bufio"
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"es_api_service/internal/config"
	"es_api_service/internal/db"

	"gopkg.in/yaml.v3"
)

type colRange struct {
	start, end int
}

var uuidLine = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-`)

func parseRanges(sep string) []colRange {
	var ranges []colRange
	i := 0
	for i < len(sep) {
		if sep[i] == '-' {
			start := i
			for i < len(sep) && sep[i] == '-' {
				i++
			}
			ranges = append(ranges, colRange{start, i})
		} else {
			i++
		}
	}
	return ranges
}

func field(line string, r colRange) string {
	// SSMS fixed-width export считает позиции в символах (рунах), не в байтах UTF-8.
	runes := []rune(line)
	if r.start >= len(runes) {
		return ""
	}
	end := r.end
	if end > len(runes) {
		end = len(runes)
	}
	return strings.TrimSpace(string(runes[r.start:end]))
}

func loadCfg(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.DB.Port == 0 {
		cfg.DB.Port = 5432
	}
	if cfg.DB.SSLMode == "" {
		cfg.DB.SSLMode = "disable"
	}
	if cfg.DB.MaxOpenConns <= 0 {
		cfg.DB.MaxOpenConns = 20
	}
	if cfg.DB.MaxIdleConns <= 0 {
		cfg.DB.MaxIdleConns = 5
	}
	return &cfg, nil
}

func main() {
	filePath := flag.String("file", "elef2.txt", "путь к экспорту SSMS")
	cfgPath := flag.String("cfg", "es_api_service.cfg", "конфиг es_api_service")
	batchSize := flag.Int("batch", 400, "размер пакета INSERT")
	flag.Parse()

	cfg, err := loadCfg(*cfgPath)
	if err != nil {
		log.Fatalf("конфиг: %v", err)
	}

	f, err := os.Open(*filePath)
	if err != nil {
		log.Fatalf("файл: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)

	if !scanner.Scan() {
		log.Fatalf("пустой файл")
	}
	if !scanner.Scan() {
		log.Fatalf("нет строки разделителя")
	}
	sep := scanner.Text()
	ranges := parseRanges(sep)
	if len(ranges) < 20 {
		log.Fatalf("ожидали 20 колонок, получили %d — проверьте формат SSMS", len(ranges))
	}
	log.Printf("Колонок: %d, пакет: %d", len(ranges), *batchSize)

	database, err := db.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("БД: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	tableName, err := ensureEsEf2Table(ctx, database)
	if err != nil {
		log.Fatalf("таблица es_ef2: %v", err)
	}
	log.Printf("Таблица для импорта: %s", tableName)

	if _, err := database.ExecContext(ctx, fmt.Sprintf(`TRUNCATE TABLE %s`, tableName)); err != nil {
		// VIEW нельзя TRUNCATE — пробуем DELETE
		if _, err2 := database.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s`, tableName)); err2 != nil {
			log.Fatalf("очистка %s: truncate=%v delete=%v", tableName, err, err2)
		}
	}

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (
			"GUID_ES", "NAME", "BARCODE", "CUREFORM_COD", "CUREFORM_NAME",
			"INN_NAME_RUS", "INN_NAME_LAT", "PRODUCER_COD", "TRN_NAME_RUS", "TRN_NAME_LAT",
			"UPAK_COD", "DATA_AN", "DATA_REG", "DOSAGE",
			"KOD_ES", "NDS_RATE", "ID_ES", "DISCRIBE", "RATING", "UPDATED",
			"TS", "created_at", "updated_at", "is_active"
		) VALUES (
			CAST(@guid AS UUID), @name, @barcode, @cureformCod, @cureformName,
			@innRus, @innLat, @producerCod, @trnRus, @trnLat,
			@upakCod, @dataAn, @dataReg, @dosage,
			@kodEs, @ndsRate, @idEs, @discribe, @rating, @updated,
			@ts, (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'), TRUE
		)
		ON CONFLICT ("GUID_ES") DO NOTHING
	`, tableName)

	var inserted, skipped, errors int
	start := time.Now()
	lineNo := 2

	for {
		var batchLines []string
		for len(batchLines) < *batchSize && scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if line == "" || !uuidLine.MatchString(line) {
				skipped++
				continue
			}
			batchLines = append(batchLines, line)
		}
		if len(batchLines) == 0 {
			break
		}

		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			log.Fatalf("tx: %v", err)
		}
		stmt, err := db.PrepareRaw(ctx, tx, insertSQL)
		if err != nil {
			tx.Rollback()
			log.Fatalf("prepare: %v", err)
		}

		batchOK := 0
		for _, line := range batchLines {
			guid := field(line, ranges[0])
			name := field(line, ranges[1])
			if name == "" {
				name = "(без названия)"
			}
			// SAVEPOINT: одна битая строка не валит весь пакет
			if _, err := tx.ExecContext(ctx, `SAVEPOINT sp_row`); err != nil {
				stmt.Close()
				tx.Rollback()
				log.Fatalf("savepoint: %v", err)
			}
			_, err := stmt.ExecContext(ctx,
				sql.Named("guid", guid),
				sql.Named("name", name),
				sql.Named("barcode", nullStr(field(line, ranges[2]))),
				sql.Named("cureformCod", nullStr(field(line, ranges[3]))),
				sql.Named("cureformName", nullStr(field(line, ranges[4]))),
				sql.Named("innRus", nullStr(field(line, ranges[5]))),
				sql.Named("innLat", nullStr(field(line, ranges[6]))),
				sql.Named("producerCod", nullBigint(field(line, ranges[7]))),
				sql.Named("trnRus", nullStr(field(line, ranges[8]))),
				sql.Named("trnLat", nullStr(field(line, ranges[9]))),
				sql.Named("upakCod", nullBigint(field(line, ranges[10]))),
				sql.Named("dataAn", nullTime(field(line, ranges[11]))),
				sql.Named("dataReg", nullTime(field(line, ranges[12]))),
				sql.Named("dosage", nullStr(field(line, ranges[13]))),
				sql.Named("kodEs", zeroBigint(field(line, ranges[14]))),
				sql.Named("ndsRate", zeroNumeric(field(line, ranges[15]))),
				sql.Named("idEs", zeroBigint(field(line, ranges[16]))),
				sql.Named("discribe", nullStr(field(line, ranges[17]))),
				sql.Named("rating", zeroInt(field(line, ranges[18]))),
				sql.Named("updated", timeOrNow(field(line, ranges[19]))),
				sql.Named("ts", []byte{0}),
			)
			if err != nil {
				_, _ = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT sp_row`)
				errors++
				if errors <= 10 {
					log.Printf("строка ~%d: %v | guid=%s | data_an=%q updated=%q",
						lineNo, err, guid, field(line, ranges[11]), field(line, ranges[19]))
				}
				continue
			}
			_, _ = tx.ExecContext(ctx, `RELEASE SAVEPOINT sp_row`)
			batchOK++
			inserted++
		}
		stmt.Close()
		if err := tx.Commit(); err != nil {
			log.Fatalf("commit: %v", err)
		}
		_ = batchOK

		if inserted%20000 == 0 && inserted > 0 {
			log.Printf("... %d строк, %.0f сек", inserted, time.Since(start).Seconds())
		}

		if len(batchLines) < *batchSize {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("чтение: %v", err)
	}

	_, _ = database.ExecContext(ctx, `
		DO $$ BEGIN
		  IF to_regclass('public."ES_EF2"') IS NOT NULL
		     AND to_regclass('public."es_ef2"') IS NULL THEN
		    EXECUTE 'CREATE OR REPLACE VIEW "es_ef2" AS SELECT * FROM "ES_EF2"';
		  END IF;
		END $$;
	`)

	var cnt int64
	_ = database.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tableName)).Scan(&cnt)
	log.Printf("Готово: вставлено %d, пропущено %d, ошибок %d, в БД %d, время %.1f мин",
		inserted, skipped, errors, cnt, time.Since(start).Minutes())
}

func nullStr(s string) interface{} {
	if isNullish(s) {
		return nil
	}
	return strings.TrimSpace(s)
}

func nullBigint(s string) interface{} {
	if isNullish(s) {
		return nil
	}
	return strings.TrimSpace(s)
}

func zeroBigint(s string) interface{} {
	if isNullish(s) {
		return "0"
	}
	return strings.TrimSpace(s)
}

func zeroNumeric(s string) interface{} {
	if isNullish(s) {
		return "0"
	}
	return strings.TrimSpace(s)
}

func zeroInt(s string) interface{} {
	if isNullish(s) {
		return "0"
	}
	return strings.TrimSpace(s)
}

func isNullish(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	u := strings.ToUpper(s)
	return u == "NULL" || u == "N" || u == "NU" || u == "NUL"
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if isNullish(s) {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func nullTime(s string) interface{} {
	if t, ok := parseTime(s); ok {
		return t
	}
	return nil
}

func timeOrNow(s string) interface{} {
	if t, ok := parseTime(s); ok {
		return t
	}
	return time.Now().UTC()
}

// ensureEsEf2Table находит или создаёт таблицу es_ef2 (на сервере обычно lowercase "es_ef2", не "ES_EF2").
func ensureEsEf2Table(ctx context.Context, database *db.Database) (string, error) {
	var relname, relkind sql.NullString
	err := database.QueryRowContext(ctx, `
		SELECT c.relname, c.relkind
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND lower(c.relname) = 'es_ef2'
		ORDER BY CASE c.relkind WHEN 'r' THEN 0 WHEN 'v' THEN 1 ELSE 2 END
		LIMIT 1
	`).Scan(&relname, &relkind)
	if err == nil && relname.Valid && relname.String != "" {
		return quoteIdent(relname.String), nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	log.Printf("Таблица es_ef2 не найдена — создаём...")
	createSQL := `
		CREATE TABLE IF NOT EXISTS "es_ef2" (
			"GUID_ES" UUID NOT NULL PRIMARY KEY,
			"NAME" TEXT NOT NULL,
			"BARCODE" TEXT,
			"CUREFORM_COD" TEXT,
			"CUREFORM_NAME" TEXT,
			"INN_NAME_RUS" TEXT,
			"INN_NAME_LAT" TEXT,
			"PRODUCER_COD" BIGINT,
			"TRN_NAME_RUS" TEXT,
			"TRN_NAME_LAT" TEXT,
			"UPAK_COD" BIGINT,
			"DATA_AN" TIMESTAMPTZ,
			"DATA_REG" TIMESTAMPTZ,
			"DOSAGE" TEXT,
			"KOD_ES" BIGINT NOT NULL DEFAULT 0,
			"NDS_RATE" NUMERIC(19,4) NOT NULL DEFAULT 0,
			"ID_ES" BIGINT NOT NULL DEFAULT 0,
			"DISCRIBE" TEXT,
			"RATING" INTEGER NOT NULL DEFAULT 0,
			"UPDATED" TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
			"DELETED" TIMESTAMPTZ,
			"TS" BYTEA NOT NULL DEFAULT '\x00',
			"created_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
			"updated_at" TIMESTAMPTZ NOT NULL DEFAULT (NOW() AT TIME ZONE 'utc'),
			"is_active" BOOLEAN NOT NULL DEFAULT TRUE
		)
	`
	if _, err := database.ExecContext(ctx, createSQL); err != nil {
		return "", fmt.Errorf("create es_ef2: %w", err)
	}
	return `"es_ef2"`, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
