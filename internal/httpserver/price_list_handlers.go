package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/dbfimport"
	"es_api_service/internal/matching"
	intsync "es_api_service/internal/sync"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// isValidCron проверяет корректность CRON выражения (включая @every 1h30m).
func isValidCron(cronExpr string) bool {
	parserWithSeconds := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parserWithSeconds.Parse(cronExpr); err == nil {
		return true
	}
	parserStandard := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parserStandard.Parse(cronExpr)
	return err == nil
}

// calculateNextUpdateFromCron вычисляет следующее время обновления на основе CRON выражения
func calculateNextUpdateFromCron(cronExpr string, from time.Time) *time.Time {
	parserWithSeconds := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	schedule, err := parserWithSeconds.Parse(cronExpr)
	if err != nil {
		parserStandard := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err = parserStandard.Parse(cronExpr)
		if err != nil {
			return nil
		}
	}
	nextTime := schedule.Next(from)
	return &nextTime
}

// handlePriceListsRouter роутит запросы к /api/price-lists
func (s *Server) handlePriceListsRouter(w http.ResponseWriter, r *http.Request) {
	if s.logger != nil {
		s.logger.Info("handlePriceListsRouter: Метод=%s, Путь=%s, Query=%s", r.Method, r.URL.Path, r.URL.RawQuery)
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")

	if s.logger != nil {
		s.logger.Info("handlePriceListsRouter: pathParts=%v, len=%d", pathParts, len(pathParts))
	}

	if r.Method == http.MethodPost && len(pathParts) >= 4 && pathParts[3] == "create" {
		s.handleCreatePriceList(w, r)
		return
	}

	if len(pathParts) >= 4 && pathParts[3] != "" && pathParts[3] != "create" {
		if len(pathParts) >= 5 && pathParts[4] == "regions" {
			s.handleGetPriceListRegions(w, r)
			return
		}
		if len(pathParts) >= 5 && pathParts[4] == "items" && r.Method == http.MethodGet {
			s.handleGetPriceListItems(w, r)
			return
		}
		if len(pathParts) >= 5 && pathParts[4] == "buyers" && r.Method == http.MethodGet {
			s.handleGetPriceListBuyers(w, r, pathParts[3])
			return
		}
		if len(pathParts) >= 5 && pathParts[4] == "fetch" && r.Method == http.MethodPost {
			s.handleForceFetchPriceList(w, r, pathParts[3])
			return
		}

		if r.Method == http.MethodPut || r.Method == http.MethodPatch {
			s.handleUpdatePriceList(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			s.handleDeletePriceList(w, r)
			return
		}
	}

	if r.Method == http.MethodGet {
		s.handleGetPriceLists(w, r)
		return
	}

	s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
}

// priceListView — DTO для GET-ответов.
type priceListView struct {
	PriceListID      string     `json:"price_list_id"`
	SupplierID       string     `json:"supplier_id"`
	SupplierName     string     `json:"supplier_name"`
	Name             string     `json:"name"`
	Description      *string    `json:"description,omitempty"`
	ImportPointID    *string    `json:"import_point_id,omitempty"`
	ImportPointName  *string    `json:"import_point_name,omitempty"`
	DefaultMarkupPct float64    `json:"-"`
	DefaultMarkupOut *float64   `json:"default_markup_pct" gorm:"-"`
	ScheduleCron     *string    `json:"schedule_cron,omitempty"`
	LastUpdateAt     *time.Time `json:"-"`
	NextUpdateAt     *time.Time `json:"-"`
	LastUpdateAtStr  *string    `json:"last_update_at,omitempty" gorm:"-"`
	NextUpdateAtStr  *string    `json:"next_update_at,omitempty" gorm:"-"`
	IsActive         bool       `json:"is_active"`
	CreatedAt        string     `json:"created_at"`
	UpdatedAt        string     `json:"updated_at"`
	RegionsCount     int        `json:"regions_count"`
	PricesCount      int        `json:"prices_count"`
	UnmatchedCount   int        `json:"unmatched_count"`
}

// handleGetPriceLists возвращает список прайсов поставщика
func (s *Server) handleGetPriceLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	supplierID := r.URL.Query().Get("supplier_id")

	// Один LATERAL на последний импорт + один COUNT по SupplierPrice вместо
	// двух коррелированных подзапросов на каждую строку прайса (раньше UI «висел»).
	query := `
		SELECT CAST(pl.PriceListID AS TEXT) AS PriceListID,
			CAST(pl.SupplierID AS TEXT) AS SupplierID,
			s.Name AS SupplierName,
			pl.Name AS Name,
			pl.Description AS Description,
			CAST(pl.ImportPointID AS TEXT) AS ImportPointID,
			ip.Name AS ImportPointName,
			CAST(COALESCE(pl.DefaultMarkupPct, 0) AS FLOAT) AS DefaultMarkupPct,
			pl.ScheduleCron AS ScheduleCron,
			pl.LastUpdateAt AS LastUpdateAt,
			pl.NextUpdateAt AS NextUpdateAt,
			pl.IsActive AS IsActive,
			pl.CreatedAt AS CreatedAt,
			pl.UpdatedAt AS UpdatedAt,
			COALESCE(rc.RegionsCount, 0) AS RegionsCount,
			COALESCE(pc.PricesCount, 0) AS PricesCount,
			COALESCE(pc.UnmatchedCount, 0) AS UnmatchedCount
		FROM PriceList pl
		INNER JOIN Supplier s ON pl.SupplierID = s.SupplierID
		LEFT JOIN ImportPoint ip ON pl.ImportPointID = ip.ImportPointID
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS RegionsCount
			FROM PriceListRegion plr
			WHERE plr.PriceListID = pl.PriceListID AND plr.IsActive = 1
		) rc ON TRUE
		LEFT JOIN LATERAL (
			SELECT ii.InvoiceImportID
			FROM InvoiceImport ii
			WHERE ii.ImportPointID = pl.ImportPointID AND ii.ImportStatus = 'COMPLETED'
			ORDER BY ii.CompletedAt DESC
			LIMIT 1
		) latest ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS PricesCount,
				COUNT(*) FILTER (WHERE sp.GUID_ES IS NULL)::int AS UnmatchedCount
			FROM SupplierPrice sp
			WHERE sp.InvoiceImportID = latest.InvoiceImportID
		) pc ON TRUE
	`

	var args []interface{}
	if supplierID != "" {
		query += " WHERE pl.SupplierID = CAST(@supplierID AS UUID)"
		args = append(args, sql.Named("supplierID", supplierID))
	}
	query += " ORDER BY s.Name, pl.Name"

	var priceLists []priceListView
	err := s.database.GORMWith(ctx).Raw(query, args...).Scan(&priceLists).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения прайсов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения прайсов: %v", err))
		return
	}
	for i := range priceLists {
		v := priceLists[i].DefaultMarkupPct
		priceLists[i].DefaultMarkupOut = &v
		if priceLists[i].LastUpdateAt != nil {
			t := priceLists[i].LastUpdateAt.Format(time.RFC3339)
			priceLists[i].LastUpdateAtStr = &t
		}
		if priceLists[i].NextUpdateAt != nil {
			t := priceLists[i].NextUpdateAt.Format(time.RFC3339)
			priceLists[i].NextUpdateAtStr = &t
		}
	}
	if priceLists == nil {
		priceLists = []priceListView{}
	}

	if s.logger != nil {
		s.logger.Info("Успешно получено прайсов: %d", len(priceLists))
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"price_lists": priceLists,
		"total":       len(priceLists),
	})
}

// handleCreatePriceList создает новый прайс
func (s *Server) handleCreatePriceList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	var req struct {
		SupplierID       string   `json:"supplier_id"`
		Name             string   `json:"name"`
		Description      *string  `json:"description,omitempty"`
		ImportPointID    *string  `json:"import_point_id,omitempty"`
		DefaultMarkupPct *float64 `json:"default_markup_pct,omitempty"`
		ScheduleCron     *string  `json:"schedule_cron,omitempty"`
		RegionIDs        []string `json:"region_ids,omitempty"`
		IsActive         bool     `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}
	if req.SupplierID == "" || req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Не указаны обязательные поля: supplier_id, name")
		return
	}
	if _, err := uuid.Parse(req.SupplierID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_id")
		return
	}
	if req.ImportPointID != nil && *req.ImportPointID != "" {
		if _, err := uuid.Parse(*req.ImportPointID); err != nil {
			s.writeError(w, http.StatusBadRequest, "Недопустимый формат import_point_id")
			return
		}
	}
	for _, regionID := range req.RegionIDs {
		if regionID != "" {
			if _, err := uuid.Parse(regionID); err != nil {
				s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый формат region_id: %s", regionID))
				return
			}
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var nextUpdateAt *time.Time
	if req.ScheduleCron != nil && *req.ScheduleCron != "" {
		if !isValidCron(*req.ScheduleCron) {
			s.writeError(w, http.StatusBadRequest, "Некорректный формат CRON. Примеры: '0 10 * * *' (ежедневно в 10:00), '0 8 * * 1-5' (Пн-Пт в 08:00)")
			return
		}
		nextUpdate := calculateNextUpdateFromCron(*req.ScheduleCron, time.Now().UTC())
		if nextUpdate != nil {
			nextUpdateAt = nextUpdate
		}
	}

	priceListID := uuid.New().String()
	defaultMarkupPct := 0.0
	if req.DefaultMarkupPct != nil {
		defaultMarkupPct = *req.DefaultMarkupPct
	}

	var scheduleCronArg interface{}
	var nextUpdateAtArg interface{}
	if req.ScheduleCron != nil && *req.ScheduleCron != "" {
		scheduleCronArg = *req.ScheduleCron
	}
	if nextUpdateAt != nil {
		nextUpdateAtArg = *nextUpdateAt
	}

	var importPointArg interface{}
	if req.ImportPointID != nil && *req.ImportPointID != "" {
		importPointArg = *req.ImportPointID
	}

	// Позиционные ? — как у ImportPoint; optional UUID решается в Go (без CASE/@named).
	insertQuery := `
		INSERT INTO PriceList (
			PriceListID, SupplierID, Name, Description, ImportPointID,
			DefaultMarkupPct, ScheduleCron, NextUpdateAt, IsActive,
			CreatedAt, UpdatedAt
		)
		VALUES (
			CAST(? AS UUID),
			CAST(? AS UUID),
			?,
			?,
			CAST(? AS UUID),
			?,
			?,
			?,
			?,
			(NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc')
		)
	`

	err := s.database.GORMWith(ctx).Exec(insertQuery,
		priceListID,
		req.SupplierID,
		req.Name,
		getStringPtr(req.Description),
		importPointArg,
		defaultMarkupPct,
		scheduleCronArg,
		nextUpdateAtArg,
		req.IsActive,
	).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания прайса: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания прайса: %v", err))
		return
	}

	if len(req.RegionIDs) > 0 {
		if badRegion, ok := s.validateRegionsForSupplier(ctx, req.SupplierID, req.RegionIDs); !ok {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Регион %s не доступен для данного поставщика", badRegion))
			return
		}
		regionQuery := `
			INSERT INTO PriceListRegion (PriceListRegionID, PriceListID, RegionID, IsActive, CreatedAt)
			VALUES (gen_random_uuid(), CAST(? AS UUID), CAST(? AS UUID), TRUE, (NOW() AT TIME ZONE 'utc'))
		`
		for _, regionID := range req.RegionIDs {
			if regionID == "" {
				continue
			}
			if err := s.database.GORMWith(ctx).Exec(regionQuery, priceListID, regionID).Error; err != nil {
				if s.logger != nil {
					s.logger.Warn("Ошибка добавления региона %s к прайсу: %v", regionID, err)
				}
			}
		}
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"price_list_id": priceListID,
		"message":       "Прайс успешно создан",
	})
}

// handleUpdatePriceList обновляет прайс
func (s *Server) handleUpdatePriceList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var priceListID string
	for i, part := range pathParts {
		if part == "price-lists" && i+1 < len(pathParts) {
			candidateID := pathParts[i+1]
			if candidateID != "create" && candidateID != "regions" {
				priceListID = candidateID
				break
			}
		}
	}
	if priceListID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}
	if _, err := uuid.Parse(priceListID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат ID прайса")
		return
	}

	var req struct {
		Name             *string  `json:"name,omitempty"`
		Description      *string  `json:"description,omitempty"`
		ImportPointID    *string  `json:"import_point_id,omitempty"`
		DefaultMarkupPct *float64 `json:"default_markup_pct"`
		ScheduleCron     *string  `json:"schedule_cron,omitempty"`
		RegionIDs        []string `json:"region_ids,omitempty"`
		IsActive         *bool    `json:"is_active,omitempty"`
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка чтения запроса: %v", err))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	var rawReq map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &rawReq)

	hasDefaultMarkupPct := false
	if _, exists := rawReq["default_markup_pct"]; exists {
		hasDefaultMarkupPct = true
	}

	if err := json.NewDecoder(bytes.NewReader(bodyBytes)).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	g := s.database.GORMWith(ctx)

	var updates []string
	var args []interface{}
	args = append(args, sql.Named("priceListID", priceListID))

	if req.Name != nil {
		updates = append(updates, "Name = @name")
		args = append(args, sql.Named("name", *req.Name))
	}
	if req.Description != nil {
		updates = append(updates, "Description = @description")
		args = append(args, sql.Named("description", getStringPtr(req.Description)))
	}
	if req.ImportPointID != nil {
		if *req.ImportPointID == "" {
			updates = append(updates, "ImportPointID = NULL")
		} else {
			updates = append(updates, "ImportPointID = CAST(@importPointID AS UUID)")
			args = append(args, sql.Named("importPointID", *req.ImportPointID))
		}
	}
	if hasDefaultMarkupPct {
		defaultMarkupValue := 0.0
		if rawValue, ok := rawReq["default_markup_pct"]; ok && rawValue != nil {
			switch v := rawValue.(type) {
			case float64:
				defaultMarkupValue = v
			case float32:
				defaultMarkupValue = float64(v)
			case int:
				defaultMarkupValue = float64(v)
			case int64:
				defaultMarkupValue = float64(v)
			case string:
				if parsed, perr := strconv.ParseFloat(v, 64); perr == nil {
					defaultMarkupValue = parsed
				}
			}
		} else if req.DefaultMarkupPct != nil {
			defaultMarkupValue = *req.DefaultMarkupPct
		}
		if defaultMarkupValue < -999.99 || defaultMarkupValue > 999.99 {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Наценка должна быть в диапазоне от -999.99 до 999.99, получено: %.2f", defaultMarkupValue))
			return
		}

		updates = append(updates, "DefaultMarkupPct = CAST(@defaultMarkupPct AS DECIMAL(5,2))")
		args = append(args, sql.Named("defaultMarkupPct", defaultMarkupValue))

		// Каскадно обновляем MarkupPct только когда наценка реально изменилась.
		var currentMarkup float64
		if err := g.Raw(
			`SELECT CAST(COALESCE(DefaultMarkupPct, 0) AS FLOAT) FROM PriceList WHERE PriceListID = CAST(@priceListID AS UUID)`,
			sql.Named("priceListID", priceListID),
		).Row().Scan(&currentMarkup); err == nil && math.Abs(currentMarkup-defaultMarkupValue) > 0.0001 {
			updatePricesQuery := `
				UPDATE SupplierPrice AS sp
				SET MarkupPct = CAST(@defaultMarkupPct AS DECIMAL(5,2)),
					UpdatedAt = (NOW() AT TIME ZONE 'utc')
				WHERE sp.IsActive = TRUE
				  AND (
					sp.PriceListID = CAST(@priceListID AS UUID)
					OR EXISTS (
						SELECT 1
						FROM InvoiceImport ii
						JOIN PriceList pl ON pl.PriceListID = CAST(@priceListID AS UUID)
						WHERE ii.InvoiceImportID = sp.InvoiceImportID
						  AND pl.ImportPointID IS NOT NULL
						  AND ii.ImportPointID = pl.ImportPointID
						  AND pl.SupplierID = sp.SupplierID
					)
				  )
			`
			res := g.Exec(updatePricesQuery,
				sql.Named("priceListID", priceListID),
				sql.Named("defaultMarkupPct", defaultMarkupValue),
			)
			if res.Error != nil {
				if s.logger != nil {
					s.logger.Warn("Не удалось обновить наценку в связанных прайсах: %v", res.Error)
				}
			} else if s.logger != nil {
				s.logger.Info("Обновлена наценка для %d позиций прайса: PriceListID=%s, MarkupPct=%.2f%%", res.RowsAffected, priceListID, defaultMarkupValue)
			}
		}
	}

	if req.ScheduleCron != nil {
		updates = append(updates, "ScheduleCron = @scheduleCron")
		args = append(args, sql.Named("scheduleCron", getStringPtr(req.ScheduleCron)))
		if *req.ScheduleCron != "" {
			nextUpdate := calculateNextUpdateFromCron(*req.ScheduleCron, time.Now().UTC())
			if nextUpdate != nil {
				updates = append(updates, "NextUpdateAt = @nextUpdateAt")
				args = append(args, sql.Named("nextUpdateAt", *nextUpdate))
			}
		} else {
			updates = append(updates, "NextUpdateAt = NULL")
		}
	}
	if req.IsActive != nil {
		updates = append(updates, "IsActive = @isActive")
		args = append(args, sql.Named("isActive", *req.IsActive))
	}

	if len(updates) == 0 && req.RegionIDs == nil {
		s.writeError(w, http.StatusBadRequest, "Не указаны поля для обновления")
		return
	}

	updates = append(updates, "UpdatedAt = (NOW() AT TIME ZONE 'utc')")

	if len(updates) > 0 {
		query := fmt.Sprintf(`
			UPDATE PriceList
			SET %s
			WHERE PriceListID = CAST(@priceListID AS UUID);
		`, strings.Join(updates, ", "))

		res := g.Exec(query, args...)
		if res.Error != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка обновления прайса: %v, SQL: %s", res.Error, query)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления прайса: %v", res.Error))
			return
		}
	}

	// Обновляем регионы атомарно (GORM Transaction).
	if req.RegionIDs != nil {
		var ownerSupplierID string
		err := g.Raw(
			`SELECT CAST(SupplierID AS TEXT) FROM PriceList WHERE PriceListID = CAST(@priceListID AS UUID)`,
			sql.Named("priceListID", priceListID),
		).Row().Scan(&ownerSupplierID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Ошибка определения поставщика прайса")
			return
		}

		if len(req.RegionIDs) > 0 {
			if badRegion, ok := s.validateRegionsForSupplier(ctx, ownerSupplierID, req.RegionIDs); !ok {
				s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Регион %s не доступен для данного поставщика", badRegion))
				return
			}
		}

		txErr := g.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(
				`DELETE FROM PriceListRegion WHERE PriceListID = CAST(? AS UUID)`,
				priceListID,
			).Error; err != nil {
				return err
			}
			insertQuery := `
				INSERT INTO PriceListRegion (PriceListRegionID, PriceListID, RegionID, IsActive, CreatedAt)
				VALUES (gen_random_uuid(), CAST(? AS UUID), CAST(? AS UUID), TRUE, (NOW() AT TIME ZONE 'utc'))
			`
			for _, regionID := range req.RegionIDs {
				if regionID == "" {
					continue
				}
				if err := tx.Exec(insertQuery, priceListID, regionID).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if txErr != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка транзакции регионов прайса: %v", txErr)
			}
			s.writeError(w, http.StatusInternalServerError, "Ошибка обновления регионов прайса")
			return
		}
	}

	response := map[string]interface{}{
		"price_list_id": priceListID,
		"message":       "Прайс успешно обновлен",
	}
	if hasDefaultMarkupPct {
		var savedValue float64
		_ = g.Raw(
			`SELECT CAST(COALESCE(DefaultMarkupPct, 0) AS FLOAT) FROM PriceList WHERE PriceListID = CAST(@priceListID AS UUID)`,
			sql.Named("priceListID", priceListID),
		).Row().Scan(&savedValue)
		response["default_markup_pct"] = savedValue
	}
	s.writeJSON(w, http.StatusOK, response)
}

// Вспомогательная функция для получения ключей map
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// validateRegionsForSupplier проверяет, что все regionIDs принадлежат поставщику
func (s *Server) validateRegionsForSupplier(ctx context.Context, supplierID string, regionIDs []string) (string, bool) {
	query := `SELECT COUNT(*) FROM SupplierRegion WHERE SupplierID = CAST(@supplierID AS UUID) AND RegionID = CAST(@regionID AS UUID) AND IsActive = 1`
	for _, regionID := range regionIDs {
		if regionID == "" {
			continue
		}
		var cnt int
		err := s.database.GORMWith(ctx).Raw(query,
			sql.Named("supplierID", supplierID),
			sql.Named("regionID", regionID),
		).Row().Scan(&cnt)
		if err != nil || cnt == 0 {
			return regionID, false
		}
	}
	return "", true
}

// handleDeletePriceList удаляет прайс (soft) + деактивирует связанные SupplierPrice.
func (s *Server) handleDeletePriceList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	var priceListID string
	for i, part := range pathParts {
		if part == "price-lists" && i+1 < len(pathParts) {
			priceListID = pathParts[i+1]
			break
		}
	}
	if priceListID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}
	if _, err := uuid.Parse(priceListID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат ID прайса")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var notFound bool
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			`UPDATE PriceList SET IsActive = 0, UpdatedAt = (NOW() AT TIME ZONE 'utc')
			 WHERE PriceListID = CAST(@priceListID AS UUID)`,
			sql.Named("priceListID", priceListID),
		)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			notFound = true
			return errors.New("not found")
		}
		return tx.Exec(
			`UPDATE SupplierPrice SET IsActive = 0, UpdatedAt = (NOW() AT TIME ZONE 'utc')
			 WHERE PriceListID = CAST(@priceListID AS UUID)`,
			sql.Named("priceListID", priceListID),
		).Error
	})
	if notFound {
		s.writeError(w, http.StatusNotFound, "Прайс не найден")
		return
	}
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка удаления прайса: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления прайса")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":       "Прайс успешно удален (деактивирован)",
		"price_list_id": priceListID,
	})
}

// handleGetPriceListRegions возвращает регионы прайса
func (s *Server) handleGetPriceListRegions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	var priceListID string
	for i, part := range pathParts {
		if part == "price-lists" && i+1 < len(pathParts) && i+2 < len(pathParts) && pathParts[i+2] == "regions" {
			priceListID = pathParts[i+1]
			break
		}
	}
	if priceListID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}
	if _, err := uuid.Parse(priceListID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат ID прайса")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type regionView struct {
		RegionID   string  `json:"region_id"`
		RegionName string  `json:"region_name"`
		RegionCode *string `json:"-"`
		RegionCodeOut string `json:"region_code" gorm:"-"`
		IsActive   bool    `json:"is_active"`
	}
	var regions []regionView
	err := s.database.GORMWith(ctx).Raw(
		`SELECT
			CAST(plr.RegionID AS TEXT) AS RegionID,
			r.Name AS RegionName,
			r.Code AS RegionCode,
			plr.IsActive AS IsActive
		FROM PriceListRegion plr
		INNER JOIN Region r ON plr.RegionID = r.RegionID
		WHERE plr.PriceListID = CAST(@priceListID AS UUID)
		  AND plr.IsActive = 1
		ORDER BY r.Name`,
		sql.Named("priceListID", priceListID),
	).Scan(&regions).Error
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения регионов: %v", err))
		return
	}
	for i := range regions {
		if regions[i].RegionCode != nil {
			regions[i].RegionCodeOut = *regions[i].RegionCode
		}
	}
	if regions == nil {
		regions = []regionView{}
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"regions": regions,
		"total":   len(regions),
	})
}

// handleGetPriceListItems — позиции загруженного прайса
func (s *Server) handleGetPriceListItems(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}
	priceListID := pathParts[3]
	if _, err := uuid.Parse(priceListID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат price_list_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	g := s.database.GORMWith(ctx)

	type plInfo struct {
		Name         string
		SupplierName string
		SupplierID   string
		Description  *string
		LastUpdateAt *time.Time
	}
	var info plInfo
	err := g.Raw(
		`SELECT pl.Name AS Name,
			s.Name AS SupplierName,
			CAST(pl.SupplierID AS TEXT) AS SupplierID,
			pl.Description AS Description,
			pl.LastUpdateAt AS LastUpdateAt
		FROM PriceList pl
		INNER JOIN Supplier s ON pl.SupplierID = s.SupplierID
		WHERE pl.PriceListID = CAST(@priceListID AS UUID)`,
		sql.Named("priceListID", priceListID),
	).Take(&info).Error
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Прайс не найден")
		return
	}

	type statsRow struct {
		Total     int
		Matched   int
		Unmatched int
	}
	var stats statsRow
	_ = g.Raw(
		`SELECT
			COUNT(*) AS Total,
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS TEXT) != '' THEN 1 ELSE 0 END), 0) AS Matched,
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NULL OR CAST(sp.GUID_ES AS TEXT) = '' THEN 1 ELSE 0 END), 0) AS Unmatched
		FROM SupplierPrice sp
		WHERE sp.InvoiceImportID = (
			SELECT ii.InvoiceImportID
			FROM InvoiceImport ii
			WHERE ii.ImportPointID = (
				SELECT pl.ImportPointID FROM PriceList pl
				WHERE pl.PriceListID = CAST(@priceListID AS UUID)
			)
			AND ii.ImportStatus = 'COMPLETED'
			ORDER BY ii.CompletedAt DESC
		)
LIMIT 1
`,
		sql.Named("priceListID", priceListID),
	).Take(&stats).Error

	if r.URL.Query().Get("stats_only") == "true" {
		lastUpdate := ""
		if info.LastUpdateAt != nil {
			lastUpdate = info.LastUpdateAt.Format("2006-01-02 15:04:05")
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"price_list": map[string]interface{}{
				"name":           info.Name,
				"supplier_name":  info.SupplierName,
				"supplier_id":    info.SupplierID,
				"last_update_at": lastUpdate,
			},
			"stats": map[string]interface{}{
				"total":     stats.Total,
				"matched":   stats.Matched,
				"unmatched": stats.Unmatched,
			},
		})
		return
	}

	matchStatus := r.URL.Query().Get("match_status")
	matchFilter := ""
	switch matchStatus {
	case "matched":
		matchFilter = " AND sp.GUID_ES IS NOT NULL"
	case "unmatched":
		matchFilter = " AND sp.GUID_ES IS NULL"
	}

	limitVal := 0
	offsetVal := 0
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if v, err := strconv.Atoi(lStr); err == nil && v > 0 {
			limitVal = v
		}
	}
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		if v, err := strconv.Atoi(oStr); err == nil && v >= 0 {
			offsetVal = v
		}
	}
	pagination := ""
	if limitVal > 0 || offsetVal > 0 {
		if limitVal <= 0 {
			limitVal = 1000000
		}
		pagination = fmt.Sprintf(" OFFSET %d LIMIT %d", offsetVal, limitVal)
	}

	itemsWhere := `sp.InvoiceImportID = (
		SELECT ii.InvoiceImportID
		FROM InvoiceImport ii
		WHERE ii.ImportPointID = (
			SELECT pl2.ImportPointID FROM PriceList pl2
			WHERE pl2.PriceListID = CAST(@priceListID AS UUID)
		)
		AND ii.ImportStatus = 'COMPLETED'
		ORDER BY ii.CompletedAt DESC
		LIMIT 1
	)
`

	type priceItemRow struct {
		ID              string
		ItemCode        *string
		ItemName        *string
		GuidES          *string
		DrugName        *string
		INN             *string
		Producer        *string
		Price           *float64
		FinalPrice      *float64
		Quantity        *float64
		BatchNumber     *string
		ExpiryDate      *time.Time
		MatchMethod     *string
		MatchConfidence *float64
		RegionID        *string
		RegionName      *string
		CreatedAt       time.Time
	}

	itemsQuery := fmt.Sprintf(`
		SELECT
			CAST(sp.SupplierPriceID AS TEXT) AS ID,
			sp.ItemCode AS ItemCode,
			sp.ItemName AS ItemName,
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS TEXT) END AS GuidES,
			ef2.NAME AS DrugName,
			ef2.INN_NAME_RUS AS INN,
			ep.PRODUCER_NAME AS Producer,
			sp.Price AS Price,
			sp.Price * (1 + COALESCE(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
			sp.Quantity AS Quantity,
			sp.BatchNumber AS BatchNumber,
			sp.ExpiryDate AS ExpiryDate,
			sp.MatchMethod AS MatchMethod,
			sp.MatchConfidence AS MatchConfidence,
			CAST(sp.RegionID AS TEXT) AS RegionID,
			r.Name AS RegionName,
			sp.CreatedAt AS CreatedAt
		FROM SupplierPrice sp
		LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r ON sp.RegionID = r.RegionID
		LEFT JOIN LATERAL (
			SELECT PRODUCER_NAME FROM es_producer ep WHERE ep.KOD_PRODUCER = ef2.PRODUCER_COD
		) ep ON true
		WHERE %s AND sp.IsActive = 1%s
		ORDER BY ef2.NAME, sp.ItemName%s
`, itemsWhere, matchFilter, pagination)

	var rows []priceItemRow
	err = g.Raw(itemsQuery, sql.Named("priceListID", priceListID)).Scan(&rows).Error
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка загрузки позиций")
		return
	}

	type PriceItem struct {
		ID              string   `json:"id"`
		ItemCode        *string  `json:"item_code,omitempty"`
		ItemName        *string  `json:"item_name,omitempty"`
		GuidES          *string  `json:"guid_es,omitempty"`
		DrugName        *string  `json:"drug_name,omitempty"`
		INN             *string  `json:"inn,omitempty"`
		Producer        *string  `json:"producer,omitempty"`
		Price           *float64 `json:"price,omitempty"`
		FinalPrice      *float64 `json:"final_price,omitempty"`
		Quantity        *float64 `json:"quantity,omitempty"`
		BatchNumber     *string  `json:"batch_number,omitempty"`
		ExpiryDate      *string  `json:"expiry_date,omitempty"`
		MatchMethod     *string  `json:"match_method,omitempty"`
		MatchConfidence *float64 `json:"match_confidence,omitempty"`
		RegionID        *string  `json:"region_id,omitempty"`
		RegionName      *string  `json:"region_name,omitempty"`
		CreatedAt       string   `json:"created_at"`
	}
	items := make([]PriceItem, 0, len(rows))
	for _, ir := range rows {
		var expiryStr *string
		if ir.ExpiryDate != nil {
			d := ir.ExpiryDate.Format("2006-01-02")
			expiryStr = &d
		}
		guid := ir.GuidES
		if guid != nil && strings.TrimSpace(*guid) == "" {
			guid = nil
		}
		items = append(items, PriceItem{
			ID:              ir.ID,
			ItemCode:        ir.ItemCode,
			ItemName:        ir.ItemName,
			GuidES:          guid,
			DrugName:        ir.DrugName,
			INN:             ir.INN,
			Producer:        ir.Producer,
			Price:           ir.Price,
			FinalPrice:      ir.FinalPrice,
			Quantity:        ir.Quantity,
			BatchNumber:     ir.BatchNumber,
			ExpiryDate:      expiryStr,
			MatchMethod:     ir.MatchMethod,
			MatchConfidence: ir.MatchConfidence,
			RegionID:        ir.RegionID,
			RegionName:      ir.RegionName,
			CreatedAt:       ir.CreatedAt.Format(time.RFC3339),
		})
	}

	resp := map[string]interface{}{
		"name":          info.Name,
		"supplier_name": info.SupplierName,
		"supplier_id":   info.SupplierID,
	}
	if info.Description != nil {
		resp["description"] = *info.Description
	}
	if info.LastUpdateAt != nil {
		resp["last_update_at"] = info.LastUpdateAt.Format("2006-01-02 15:04:05")
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"price_list": resp,
		"stats": map[string]int{
			"total":     stats.Total,
			"matched":   stats.Matched,
			"unmatched": stats.Unmatched,
		},
		"items":          items,
		"items_returned": len(items),
	})
}

// handleForceFetchPriceList запускает немедленный ручной забор прайса.
func (s *Server) handleForceFetchPriceList(w http.ResponseWriter, r *http.Request, priceListID string) {
	if _, err := uuid.Parse(priceListID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var exists int
	err := s.database.GORMWith(ctx).Raw(`
		SELECT 1
		FROM PriceList pl
		WHERE pl.PriceListID = CAST(@id AS UUID)
		  AND pl.IsActive = 1
		  AND pl.ImportPointID IS NOT NULL
		LIMIT 1`,
		sql.Named("id", priceListID),
	).Row().Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusNotFound, "Прайс не найден, неактивен или не имеет точки импорта")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка проверки прайса: %v", err))
		return
	}

	go func() {
		importer := dbfimport.NewDBFImporter(s.database, s.logger)
		matcher := matching.NewPriceMatcher(s.database, s.logger)
		scheduler := intsync.NewPriceListScheduler(s.database, s.logger, importer, matcher)
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := scheduler.ForceFetchPriceListNow(runCtx, priceListID); err != nil && s.logger != nil {
			s.logger.Error("Ошибка ручного запуска обновления прайса %s: %v", priceListID, err)
		}
	}()

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "started",
		"message": "Ручное обновление прайса запущено",
	})
}
