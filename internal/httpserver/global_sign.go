package httpserver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var globalSignRe = regexp.MustCompile(`(?i)^EX-(\d{1,7})$`)

// ensureGlobalSignSchema — колонка + счётчик (идемпотентно).
func (s *Server) ensureGlobalSignSchema(tx *gorm.DB) error {
	stmts := []string{
		`ALTER TABLE "Order" ADD COLUMN IF NOT EXISTS "GlobalSign" varchar(16)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "ux_order_global_sign" ON "Order" ("GlobalSign") WHERE "GlobalSign" IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS "GlobalSignCounter" (
			"ID" integer PRIMARY KEY CHECK ("ID" = 1),
			"NextValue" bigint NOT NULL DEFAULT 1
		)`,
		`INSERT INTO "GlobalSignCounter" ("ID", "NextValue") VALUES (1, 1) ON CONFLICT ("ID") DO NOTHING`,
	}
	for _, q := range stmts {
		if err := tx.Exec(q).Error; err != nil {
			return err
		}
	}
	return nil
}

func formatGlobalSign(n int64) string {
	if n < 1 {
		n = 1
	}
	if n > 9999999 {
		return fmt.Sprintf("EX-%d", n) // overflow beyond 7 digits still unique
	}
	return fmt.Sprintf("EX-%07d", n)
}

func parseGlobalSign(sign string) (int64, bool) {
	m := globalSignRe.FindStringSubmatch(strings.TrimSpace(sign))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// allocateGlobalSign — атомарно выдаёт следующий EX-####### (глобально уникальный).
func (s *Server) allocateGlobalSign(tx *gorm.DB) (string, error) {
	if err := s.ensureGlobalSignSchema(tx); err != nil {
		return "", err
	}

	var next int64
	// Блокируем строку счётчика.
	if err := tx.Raw(`
		SELECT "NextValue" FROM "GlobalSignCounter" WHERE "ID" = 1 FOR UPDATE
	`).Scan(&next).Error; err != nil {
		return "", err
	}
	if next < 1 {
		next = 1
	}

	// Пропускаем уже занятые номера (на случай ручных вставок / рассинхрона).
	for attempts := 0; attempts < 10000; attempts++ {
		sign := formatGlobalSign(next)
		var cnt int64
		if err := tx.Raw(`
			SELECT COUNT(*) FROM "Order" WHERE "GlobalSign" = ?
		`, sign).Scan(&cnt).Error; err != nil {
			return "", err
		}
		if cnt == 0 {
			if err := tx.Exec(`
				UPDATE "GlobalSignCounter" SET "NextValue" = ? WHERE "ID" = 1
			`, next+1).Error; err != nil {
				return "", err
			}
			return sign, nil
		}
		next++
	}
	return "", fmt.Errorf("не удалось выделить GlobalSign")
}

// resolveGlobalSignForCreate — если клиент прислал свободный EX-N, берём его
// и поднимаем счётчик; иначе выделяем новый.
func (s *Server) resolveGlobalSignForCreate(tx *gorm.DB, requested *string) (string, error) {
	if err := s.ensureGlobalSignSchema(tx); err != nil {
		return "", err
	}

	if requested != nil {
		sign := strings.ToUpper(strings.TrimSpace(*requested))
		if n, ok := parseGlobalSign(sign); ok {
			sign = formatGlobalSign(n)
			var cnt int64
			if err := tx.Raw(`SELECT COUNT(*) FROM "Order" WHERE "GlobalSign" = ?`, sign).Scan(&cnt).Error; err != nil {
				return "", err
			}
			if cnt == 0 {
				// Поднимаем счётчик не ниже n+1
				if err := tx.Exec(`
					UPDATE "GlobalSignCounter"
					SET "NextValue" = GREATEST("NextValue", ?)
					WHERE "ID" = 1
				`, n+1).Error; err != nil {
					return "", err
				}
				return sign, nil
			}
			// Занят — выдаём новый ниже
		}
	}
	return s.allocateGlobalSign(tx)
}

// reserveGlobalSign — отдельный endpoint: заранее резервирует номер для десктопа.
func (s *Server) reserveGlobalSign(tx *gorm.DB) (string, error) {
	return s.allocateGlobalSign(tx)
}
