package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/matching"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// handleMatchInvoiceData запускает процесс сопоставления данных импорта
func (s *Server) handleMatchInvoiceData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	var req struct {
		InvoiceImportID string `json:"invoice_import_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.InvoiceImportID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан InvoiceImportID")
		return
	}

	// Создаем независимый контекст для горутины
	// Используем контекст без таймаута для сопоставления больших файлов (может занять много времени)
	// Для очень больших файлов сопоставление может занимать несколько часов
	matchCtx, matchCancel := context.WithCancel(context.Background()) // Без таймаута для больших файлов

	// Запускаем сопоставление в отдельной горутине
	go func() {
		defer matchCancel()

		matcher := matching.NewPriceMatcher(s.database, s.logger)
		err := matcher.MatchInvoiceData(matchCtx, req.InvoiceImportID)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка сопоставления данных импорта %s: %v", req.InvoiceImportID, err)
			}
		} else {
			if s.logger != nil {
				s.logger.Info("Сопоставление данных импорта %s успешно завершено", req.InvoiceImportID)
			}
		}
	}()

	s.writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":            "matching_started",
		"message":           "Процесс сопоставления запущен в фоновом режиме",
		"invoice_import_id": req.InvoiceImportID,
	})
}

// handleSupplierPricesRouter роутит запросы к /api/supplier-prices
func (s *Server) handleSupplierPricesRouter(w http.ResponseWriter, r *http.Request) {
	pathParts := strings.Split(r.URL.Path, "/")

	// Проверяем наличие ID в пути
	// pathParts[0] = "", pathParts[1] = "api", pathParts[2] = "supplier-prices", pathParts[3] = ID, pathParts[4] = "match"
	if len(pathParts) >= 4 && pathParts[3] != "" && pathParts[3] != "summary" && pathParts[3] != "create" {
		// Проверяем дополнительный путь для match
		if len(pathParts) >= 5 && pathParts[4] == "match" {
			if r.Method == http.MethodPut || r.Method == http.MethodPatch {
				// Извлекаем ID из пути и передаем в обработчик через контекст
				priceID := pathParts[3]
				if priceID != "" {
					// Добавляем ID в контекст запроса для использования в handleUpdateSupplierPriceMatch
					ctx := context.WithValue(r.Context(), "priceID", priceID)
					r = r.WithContext(ctx)
				}
				s.handleUpdateSupplierPriceMatch(w, r)
				return
			}
		}

		// Проверяем дополнительный путь для toggle
		if len(pathParts) >= 5 && pathParts[4] == "toggle" {
			s.handleToggleSupplierPrice(w, r)
			return
		}

		// PUT/PATCH для обновления, DELETE для удаления
		if r.Method == http.MethodPut || r.Method == http.MethodPatch {
			s.handleUpdateSupplierPrice(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			s.handleDeleteSupplierPrice(w, r)
			return
		}

		// GET по ID — одна запись SupplierPrice
		if r.Method == http.MethodGet {
			priceID := pathParts[3]
			ctx := context.WithValue(r.Context(), "priceID", priceID)
			r = r.WithContext(ctx)
			s.handleGetSupplierPriceByID(w, r)
			return
		}
	}

	// GET запрос - получение списка прайсов
	if r.Method == http.MethodGet {
		s.handleGetSupplierPrices(w, r)
		return
	}

	s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
}

// handleGetSupplierPrices возвращает список прайсов поставщика
func (s *Server) handleGetSupplierPrices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	supplierID := r.URL.Query().Get("supplier_id")
	if supplierID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_id")
		return
	}
	if _, err := uuid.Parse(supplierID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_id")
		return
	}

	regionID := r.URL.Query().Get("region_id")
	if regionID != "" {
		if _, err := uuid.Parse(regionID); err != nil {
			s.writeError(w, http.StatusBadRequest, "Недопустимый формат region_id")
			return
		}
	}

	includeInactive := r.URL.Query().Get("include_inactive") == "true"
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
	latestOnly := r.URL.Query().Get("latest_only") != "false"

	var updatedSince *time.Time
	if sinceStr := strings.TrimSpace(r.URL.Query().Get("updated_since")); sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05", sinceStr)
		}
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "Недопустимый updated_since (нужен RFC3339)")
			return
		}
		updatedSince = &t
	}

	supplierWhere := `sp.SupplierID = CAST(@supplierID AS UUID)`
	if latestOnly {
		supplierWhere = `sp.InvoiceImportID IN (
			SELECT lii.InvoiceImportID FROM InvoiceImport lii
			WHERE lii.ImportPointID IN (
				SELECT pl.ImportPointID FROM PriceList pl
				WHERE pl.SupplierID = CAST(@supplierID AS UUID) AND pl.IsActive = 1
			)
			AND lii.ImportStatus = 'COMPLETED'
			AND lii.CompletedAt = (
				SELECT MAX(lii2.CompletedAt) FROM InvoiceImport lii2
				WHERE lii2.ImportPointID = lii.ImportPointID AND lii2.ImportStatus = 'COMPLETED'
			)
		)`
	}

	countWhere := supplierWhere
	if !includeInactive {
		countWhere += ` AND sp.IsActive = 1`
	}
	if regionID != "" {
		countWhere += ` AND (sp.RegionID = CAST(@regionID AS UUID) OR sp.RegionID IS NULL)`
	}
	if updatedSince != nil {
		countWhere += ` AND sp.UpdatedAt > @updatedSince`
	}

	argsMeta := []interface{}{sql.Named("supplierID", supplierID)}
	if regionID != "" {
		argsMeta = append(argsMeta, sql.Named("regionID", regionID))
	}
	if updatedSince != nil {
		argsMeta = append(argsMeta, sql.Named("updatedSince", *updatedSince))
	}

	var totalInDB, totalNullGuid, totalEmptyGuid int
	var maxUpdated sql.NullTime
	statsQuery := fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS TEXT) = '' THEN 1 ELSE 0 END), 0),
			MAX(sp.UpdatedAt)
		FROM SupplierPrice sp
		WHERE %s
	`, countWhere)
	if err := s.database.GORMWith(ctx).Raw(statsQuery, argsMeta...).Row().Scan(&totalInDB, &totalNullGuid, &totalEmptyGuid, &maxUpdated); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка статистики прайсов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения прайса")
		return
	}

	maxUp := time.Time{}
	if maxUpdated.Valid {
		maxUp = maxUpdated.Time
	}
	etag := priceListETag(supplierID, totalInDB, maxUp)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, must-revalidate")
	if maxUpdated.Valid {
		w.Header().Set("Last-Modified", maxUp.UTC().Format(http.TimeFormat))
	}
	if updatedSince != nil {
		w.Header().Set("X-Price-Delta", "1")
	}
	if match := strings.TrimSpace(r.Header.Get("If-None-Match")); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if s.tryServePriceCache(w, r, supplierID, etag, totalInDB) {
		return
	}

	query := fmt.Sprintf(`
		SELECT
			CAST(sp.SupplierPriceID AS TEXT) AS SupplierPriceID,
			CAST(sp.SupplierID AS TEXT) AS SupplierID,
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS TEXT) END AS GUID_ES,
			ef2.NAME AS DrugName,
			ef2.INN_NAME_RUS AS INN,
			ef2.CUREFORM_NAME AS CureForm,
			ef2.BARCODE AS Barcode,
			sp.ItemCode AS SupplierItemCode,
			sp.ItemName AS SupplierItemName,
			sp.Price,
			sp.Quantity,
			sp.InvoiceNumber,
			sp.InvoiceDate,
			sp.BatchNumber,
			sp.ExpiryDate,
			sp.MatchMethod,
			sp.MatchConfidence,
			sp.InvoiceDate AS LastPriceDate,
			sp.IsActive,
			CAST(sp.RegionID AS TEXT) AS RegionID,
			r.Name AS RegionName,
			COALESCE(sp.MarkupPct, 0) AS MarkupPct,
			sp.Price * (1 + COALESCE(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
			sp.CreatedAt,
			CAST(NULL AS TEXT) AS ProducerName
		FROM SupplierPrice sp
		LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r ON sp.RegionID = r.RegionID
		WHERE %s
`, supplierWhere)

	if !includeInactive {
		query += ` AND sp.IsActive = 1`
	}
	args := []interface{}{sql.Named("supplierID", supplierID)}
	if regionID != "" {
		query += ` AND (sp.RegionID = CAST(@regionID AS UUID) OR sp.RegionID IS NULL)`
		args = append(args, sql.Named("regionID", regionID))
	}
	if updatedSince != nil {
		query += ` AND sp.UpdatedAt > @updatedSince`
		args = append(args, sql.Named("updatedSince", *updatedSince))
	}
	query += `
		ORDER BY
			CASE WHEN ef2.NAME IS NULL THEN 1 ELSE 0 END,
			ef2.NAME,
			sp.ItemName,
			sp.InvoiceDate DESC,
			sp.Price DESC
	`
	if limitVal > 0 || offsetVal > 0 {
		if limitVal <= 0 {
			limitVal = 1000000
		}
		query += fmt.Sprintf(" OFFSET %d LIMIT %d", offsetVal, limitVal)
	}

	rows, err := s.database.GORMWith(ctx).Raw(query, args...).Rows()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка SQL прайсов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения прайса: %v", err))
		return
	}
	defer rows.Close()

	type SupplierPriceView struct {
		SupplierPriceID  string   `json:"supplier_price_id"`
		SupplierID       string   `json:"supplier_id"`
		GUID_ES          *string  `json:"guid_es,omitempty"`
		DrugName         *string  `json:"drug_name,omitempty"`
		INN              *string  `json:"inn,omitempty"`
		CureForm         *string  `json:"cure_form,omitempty"`
		Barcode          *string  `json:"barcode,omitempty"`
		ProducerName     *string  `json:"producer_name,omitempty"`
		SupplierItemCode *string  `json:"supplier_item_code,omitempty"`
		SupplierItemName *string  `json:"supplier_item_name,omitempty"`
		Quantity         *float64 `json:"quantity,omitempty"`
		InvoiceNumber    *string  `json:"invoice_number,omitempty"`
		InvoiceDate      *string  `json:"invoice_date,omitempty"`
		BatchNumber      *string  `json:"batch_number,omitempty"`
		ExpiryDate       *string  `json:"expiry_date,omitempty"`
		MatchMethod      *string  `json:"match_method,omitempty"`
		MatchConfidence  *float64 `json:"match_confidence,omitempty"`
		IsActive         bool     `json:"is_active"`
		RegionID         *string  `json:"region_id,omitempty"`
		RegionName       *string  `json:"region_name,omitempty"`
		FinalPrice       *float64 `json:"price"`
		LastPriceDate    *string  `json:"last_price_date,omitempty"`
		CreatedAt        string   `json:"created_at"`
	}

	dbUnmatched := totalNullGuid + totalEmptyGuid
	dbMatched := totalInDB - dbUnmatched

	expectedReturned := totalInDB
	if limitVal > 0 {
		expectedReturned = limitVal
		if offsetVal >= totalInDB {
			expectedReturned = 0
		} else if offsetVal+limitVal > totalInDB {
			expectedReturned = totalInDB - offsetVal
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Price-Cache", "MISS")
	w.WriteHeader(http.StatusOK)

	var out io.Writer = w
	var cacheWC *priceCacheWriteCloser
	cacheable := updatedSince == nil && regionID == "" && !includeInactive && latestOnly && limitVal == 0 && offsetVal == 0
	if cacheable {
		if wc, _, err := openPriceCacheWriter(supplierID); err == nil {
			if c, ok := wc.(*priceCacheWriteCloser); ok {
				cacheWC = c
				out = io.MultiWriter(w, cacheWC)
			}
		}
	}

	if _, err := fmt.Fprintf(out, `{"supplier_id":%s,"total_prices":%d,"stats":{"total_in_db":%d,"matched_final":%d,"unmatched_final":%d,"prices_returned":%d},"prices":`,
		strconv.Quote(supplierID), expectedReturned, totalInDB, dbMatched, dbUnmatched, expectedReturned,
	); err != nil {
		if cacheWC != nil {
			cacheWC.Abort()
		}
		return
	}

	streamer := NewJSONStreamer(out)
	if err := streamer.WriteArrayStart(); err != nil {
		return
	}

	returned := 0
	for rows.Next() {
		var sp SupplierPriceView
		var guidES, drugName, inn, cureForm, barcode, producerName, supplierItemCode, supplierItemName sql.NullString
		var invoiceNumber, batchNumber, matchMethod, regionIDNull, regionName sql.NullString
		var invoiceDate, expiryDate, lastPriceDate sql.NullTime
		var quantity, matchConfidence sql.NullFloat64
		var basePrice, finalPrice sql.NullFloat64

		err := rows.Scan(
			&sp.SupplierPriceID,
			&sp.SupplierID,
			&guidES,
			&drugName,
			&inn,
			&cureForm,
			&barcode,
			&supplierItemCode,
			&supplierItemName,
			&basePrice,
			&quantity,
			&invoiceNumber,
			&invoiceDate,
			&batchNumber,
			&expiryDate,
			&matchMethod,
			&matchConfidence,
			&lastPriceDate,
			&sp.IsActive,
			&regionIDNull,
			&regionName,
			&sql.NullFloat64{},
			&finalPrice,
			&sp.CreatedAt,
			&producerName,
		)
		if err != nil {
			continue
		}
		if guidES.Valid && strings.TrimSpace(guidES.String) != "" {
			sp.GUID_ES = &guidES.String
		}
		if drugName.Valid {
			sp.DrugName = &drugName.String
		}
		if inn.Valid {
			sp.INN = &inn.String
		}
		if cureForm.Valid {
			sp.CureForm = &cureForm.String
		}
		if barcode.Valid {
			sp.Barcode = &barcode.String
		}
		if producerName.Valid {
			sp.ProducerName = &producerName.String
		}
		if supplierItemCode.Valid {
			sp.SupplierItemCode = &supplierItemCode.String
		}
		if supplierItemName.Valid {
			sp.SupplierItemName = &supplierItemName.String
		}
		if quantity.Valid {
			q := quantity.Float64
			sp.Quantity = &q
		}
		if invoiceNumber.Valid {
			sp.InvoiceNumber = &invoiceNumber.String
		}
		if invoiceDate.Valid {
			sp.InvoiceDate = stringPtr(invoiceDate.Time.Format(time.RFC3339))
		}
		if batchNumber.Valid {
			sp.BatchNumber = &batchNumber.String
		}
		if expiryDate.Valid {
			sp.ExpiryDate = stringPtr(expiryDate.Time.Format(time.RFC3339))
		}
		if matchMethod.Valid {
			sp.MatchMethod = &matchMethod.String
		}
		if matchConfidence.Valid {
			sp.MatchConfidence = &matchConfidence.Float64
		}
		if lastPriceDate.Valid {
			sp.LastPriceDate = stringPtr(lastPriceDate.Time.Format(time.RFC3339))
		}
		if regionIDNull.Valid {
			sp.RegionID = &regionIDNull.String
		}
		if regionName.Valid {
			sp.RegionName = &regionName.String
		}
		if finalPrice.Valid {
			sp.FinalPrice = &finalPrice.Float64
		}

		if err := streamer.WriteItem(sp); err != nil {
			if cacheWC != nil {
				cacheWC.Abort()
			}
			return
		}
		returned++
		if returned%2000 == 0 {
			flushWriter(w)
		}
	}
	_ = streamer.WriteArrayEnd()
	_, _ = out.Write([]byte("}"))
	flushWriter(w)

	if cacheWC != nil {
		if err := cacheWC.Close(); err != nil {
			cacheWC.Abort()
		} else {
			_ = writePriceCacheMeta(supplierID, priceCacheMeta{
				ETag:      etag,
				Total:     totalInDB,
				UpdatedAt: maxUp,
				BuiltAt:   time.Now().UTC(),
			})
		}
	} else if cacheable {
		s.scheduleRebuildPriceCache(supplierID)
	}

	if s.logger != nil {
		s.logger.Info("handleGetSupplierPrices stream: supplier=%s total_in_db=%d returned=%d delta=%v",
			supplierID, totalInDB, returned, updatedSince != nil)
	}
}


func (s *Server) handleGetSupplierPriceByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	priceIDVal := r.Context().Value("priceID")
	priceID, ok := priceIDVal.(string)
	if !ok || priceID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_price_id")
		return
	}

	if _, err := uuid.Parse(priceID); err != nil {
		if s.logger != nil {
			s.logger.Warn("Недопустимый формат supplier_price_id: %s", priceID)
		}
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_price_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	query := `
		SELECT
			CAST(sp.SupplierPriceID AS TEXT) AS SupplierPriceID,
			CAST(sp.SupplierID AS TEXT) AS SupplierID,
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS TEXT) END AS GUID_ES,
			ef2.NAME AS DrugName,
			ef2.INN_NAME_RUS AS INN,
			ef2.CUREFORM_NAME AS CureForm,
			ef2.BARCODE AS Barcode,
			sp.ItemCode AS SupplierItemCode,
			sp.ItemName AS SupplierItemName,
			sp.Price,
			sp.Quantity,
			sp.InvoiceNumber,
			sp.InvoiceDate,
			sp.BatchNumber,
			sp.ExpiryDate,
			sp.MatchMethod,
			sp.MatchConfidence,
			sp.InvoiceDate AS LastPriceDate,
			sp.IsActive,
			CAST(sp.RegionID AS TEXT) AS RegionID,
			r.Name AS RegionName,
			COALESCE(sp.MarkupPct, 0) AS MarkupPct,
			sp.Price * (1 + COALESCE(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
			sp.CreatedAt,
			CAST(NULL AS TEXT) AS ProducerName
		FROM SupplierPrice sp 
		LEFT JOIN es_ef2 ef2  ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r  ON sp.RegionID = r.RegionID
		WHERE sp.SupplierPriceID = CAST(@priceID AS UUID)
LIMIT 1
`

	type SupplierPriceView struct {
		SupplierPriceID  string   `json:"supplier_price_id"`
		SupplierID       string   `json:"supplier_id"`
		GUID_ES          *string  `json:"guid_es,omitempty"`
		DrugName         *string  `json:"drug_name,omitempty"`
		INN              *string  `json:"inn,omitempty"`
		CureForm         *string  `json:"cure_form,omitempty"`
		Barcode          *string  `json:"barcode,omitempty"`
		ProducerName     *string  `json:"producer_name,omitempty"`
		SupplierItemCode *string  `json:"supplier_item_code,omitempty"`
		SupplierItemName *string  `json:"supplier_item_name,omitempty"`
		Quantity         *float64 `json:"quantity,omitempty"`
		InvoiceNumber    *string  `json:"invoice_number,omitempty"`
		InvoiceDate      *string  `json:"invoice_date,omitempty"`
		BatchNumber      *string  `json:"batch_number,omitempty"`
		ExpiryDate       *string  `json:"expiry_date,omitempty"`
		MatchMethod      *string  `json:"match_method,omitempty"`
		MatchConfidence  *float64 `json:"match_confidence,omitempty"`
		IsActive         bool     `json:"is_active"`
		RegionID         *string  `json:"region_id,omitempty"`
		RegionName       *string  `json:"region_name,omitempty"`
		FinalPrice       *float64 `json:"price"`
		LastPriceDate    *string  `json:"last_price_date,omitempty"`
		CreatedAt        string   `json:"created_at"`
	}

	var sp SupplierPriceView
	var guidES, drugName, inn, cureForm, barcode, producerName, supplierItemCode, supplierItemName sql.NullString
	var invoiceNumber, batchNumber, matchMethod, regionID, regionName sql.NullString
	var invoiceDate, expiryDate, lastPriceDate sql.NullTime
	var quantity, matchConfidence sql.NullFloat64
	var basePrice, finalPrice sql.NullFloat64

	err := s.database.GORMWith(ctx).Raw(query, sql.Named("priceID", priceID)).Row().Scan(
		&sp.SupplierPriceID,
		&sp.SupplierID,
		&guidES,
		&drugName,
		&inn,
		&cureForm,
		&barcode,
		&supplierItemCode,
		&supplierItemName,
		&basePrice,
		&quantity,
		&invoiceNumber,
		&invoiceDate,
		&batchNumber,
		&expiryDate,
		&matchMethod,
		&matchConfidence,
		&lastPriceDate,
		&sp.IsActive,
		&regionID,
		&regionName,
		&sql.NullFloat64{}, // MarkupPct — не передаём клиенту
		&finalPrice,
		&sp.CreatedAt,
		&producerName,
	)
	if err == sql.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "Запись не найдена")
		return
	}
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выборки SupplierPrice по ID %s: %v", priceID, err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения записи: %v", err))
		return
	}

	if guidES.Valid && strings.TrimSpace(guidES.String) != "" {
		sp.GUID_ES = &guidES.String
	}
	if drugName.Valid {
		sp.DrugName = &drugName.String
	}
	if inn.Valid {
		sp.INN = &inn.String
	}
	if cureForm.Valid {
		sp.CureForm = &cureForm.String
	}
	if barcode.Valid {
		sp.Barcode = &barcode.String
	}
	if producerName.Valid {
		sp.ProducerName = &producerName.String
	}
	if supplierItemCode.Valid {
		sp.SupplierItemCode = &supplierItemCode.String
	}
	if supplierItemName.Valid {
		sp.SupplierItemName = &supplierItemName.String
	}
	if quantity.Valid {
		q := quantity.Float64
		sp.Quantity = &q
	}
	if invoiceNumber.Valid {
		sp.InvoiceNumber = &invoiceNumber.String
	}
	if invoiceDate.Valid {
		sp.InvoiceDate = stringPtr(invoiceDate.Time.Format(time.RFC3339))
	}
	if batchNumber.Valid {
		sp.BatchNumber = &batchNumber.String
	}
	if expiryDate.Valid {
		sp.ExpiryDate = stringPtr(expiryDate.Time.Format(time.RFC3339))
	}
	if matchMethod.Valid {
		sp.MatchMethod = &matchMethod.String
	}
	if matchConfidence.Valid {
		sp.MatchConfidence = &matchConfidence.Float64
	}
	if lastPriceDate.Valid {
		sp.LastPriceDate = stringPtr(lastPriceDate.Time.Format(time.RFC3339))
	}
	if regionID.Valid {
		sp.RegionID = &regionID.String
	}
	if regionName.Valid {
		sp.RegionName = &regionName.String
	}
	if finalPrice.Valid {
		sp.FinalPrice = &finalPrice.Float64
	}

	s.writeJSON(w, http.StatusOK, sp)
}

func (s *Server) handleGlobalStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var matched, unmatched int
	err := s.database.GORMWith(ctx).Raw(`
		SELECT
			COUNT(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS TEXT) <> '' THEN 1 END),
			COUNT(CASE WHEN sp.GUID_ES IS NULL OR CAST(sp.GUID_ES AS TEXT) = '' THEN 1 END)
		FROM SupplierPrice sp
		INNER JOIN PriceList pl ON pl.PriceListID = sp.PriceListID AND pl.IsActive = TRUE
		WHERE sp.IsActive = TRUE
	`).Row().Scan(&matched, &unmatched)

	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения глобальной статистики: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения статистики")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]int{
		"matched":   matched,
		"unmatched": unmatched,
		"total":     matched + unmatched,
	})
}

// handleGetSupplierPriceSummary возвращает сводную информацию о прайсах (последние цены по каждому препарату)
// buyerAssignedSupplierIDs возвращает SupplierID активных прайс-листов, назначенных покупателю.
func (s *Server) buyerAssignedSupplierIDs(ctx context.Context, buyerID string) ([]string, error) {
	if s.database == nil || buyerID == "" {
		return nil, nil
	}
	var ids []string
	err := s.database.GORMWith(ctx).Raw(`
		SELECT DISTINCT CAST(pl.SupplierID AS TEXT)
		FROM BuyerPriceList bpl
		JOIN PriceList pl ON pl.PriceListID = bpl.PriceListID
		WHERE bpl.BuyerID = CAST(? AS UUID)
		  AND bpl.IsActive = 1
		  AND pl.IsActive = 1
		  AND pl.SupplierID IS NOT NULL
	`, buyerID).Scan(&ids).Error
	return ids, err
}

func (s *Server) handleGetSupplierPriceSummary(w http.ResponseWriter, r *http.Request) {
	// Обработка паник
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleGetSupplierPriceSummary: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	// Проверяем наличие подключения к БД
	if s.database == nil {
		if s.logger != nil {
			s.logger.Error("База данных не инициализирована")
		}
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	// Увеличиваем таймаут для сводного прайса до 120 секунд, так как запрос может быть долгим
	// особенно во время синхронизации ES_EF2 или сопоставления
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	supplierID := r.URL.Query().Get("supplier_id")

	if s.logger != nil {
		if supplierID != "" {
			s.logger.Info("Запрос сводного прайса для поставщика: %s", supplierID)
		} else {
			s.logger.Info("Запрос сводного прайса для всех поставщиков")
		}
	}

	var query string
	var args []interface{}

	// Проверяем фильтр по региону
	regionID := r.URL.Query().Get("region_id")
	if regionID != "" {
		// Валидация UUID для региона
		if _, err := uuid.Parse(regionID); err != nil {
			if s.logger != nil {
				s.logger.Warn("Недопустимый формат region_id: %s", regionID)
			}
			s.writeError(w, http.StatusBadRequest, "Недопустимый формат region_id")
			return
		}
	}

	// Итоговая цена: база × (1 + (прайс + регион + клиент) / 100), ±%.
	pc := s.resolvePricingContext(ctx, r)
	finalPriceExpr := sqlAdditiveFinalPriceExpr("sp", pc)
	if s.logger != nil {
		s.logger.Info("Сводный прайс: pricing buyer_id=%s region_id=%s", pc.BuyerID, pc.RegionID)
	}

	// Поиск по названию (дозагрузка из десктопа) и постраничность.
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	qLike := ""
	if search != "" {
		qLike = "%" + search + "%"
	}
	limitVal, offsetVal := 5000, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limitVal)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		fmt.Sscanf(v, "%d", &offsetVal)
	}
	if limitVal <= 0 {
		limitVal = 5000 // по умолчанию (совместимо со старым клиентом)
	}
	if limitVal > 500000 {
		limitVal = 500000 // весь прайс (~230k+) одним запросом
	}
	if offsetVal < 0 {
		offsetVal = 0
	}

	// Скоуп по назначенным покупателю прайс-листам: покупатель видит цены ТОЛЬКО
	// назначенных ему поставщиков (BuyerPriceList → PriceList → Supplier).
	// Если активных назначений нет — показываем всё (обратная совместимость).
	// UUID берутся из БД и валидируются, поэтому инлайним их безопасно.
	supplierScope := ""
	if pc.BuyerID != "" {
		ids, errScope := s.buyerAssignedSupplierIDs(ctx, pc.BuyerID)
		if errScope != nil && s.logger != nil {
			s.logger.Warn("Сводный прайс: не удалось получить назначения покупателя %s: %v", pc.BuyerID, errScope)
		}
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, e := uuid.Parse(id); e == nil {
				parts = append(parts, "CAST('"+id+"' AS UUID)")
			}
		}
		if len(parts) > 0 {
			supplierScope = " AND sp.SupplierID IN (" + strings.Join(parts, ", ") + ")"
			if s.logger != nil {
				s.logger.Info("Сводный прайс: скоуп покупателя %s → %d поставщик(ов)", pc.BuyerID, len(parts))
			}
		}
	}

	// Фильтр по конкретному прайс-листу (выбор «Мои прайсы» в десктопе).
	priceListScope := ""
	if plID := strings.TrimSpace(r.URL.Query().Get("price_list_id")); plID != "" {
		if _, err := uuid.Parse(plID); err != nil {
			s.writeError(w, http.StatusBadRequest, "Недопустимый формат price_list_id")
			return
		}
		priceListScope = " AND sp.PriceListID = CAST('" + plID + "' AS UUID)"
	}

	if supplierID != "" {
		// Валидация UUID для защиты от SQL injection
		if _, err := uuid.Parse(supplierID); err != nil {
			if s.logger != nil {
				s.logger.Warn("Недопустимый формат supplier_id: %s", supplierID)
			}
			s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_id")
			return
		}

		// Сводный прайс для конкретного поставщика
		query = `
			WITH LatestPrices AS (
				SELECT
					sp.GUID_ES,
					sp.SupplierPriceID,
					sp.BatchNumber,
					sp.ExpiryDate,
					sp.Manufacturer,
					sp.Country,
					sp.Price,
					sp.Quantity,
					sp.InvoiceDate,
					sp.MatchMethod,
					sp.MatchConfidence,
					sp.RegionID,
					COALESCE(sp.MarkupPct, 0) AS MarkupPct,
					` + finalPriceExpr + ` AS FinalPrice,
					ROW_NUMBER() OVER (
						PARTITION BY 
							sp.GUID_ES, 
							COALESCE(sp.BatchNumber, ''), 
							COALESCE(CAST(sp.ExpiryDate AS TEXT), ''),
							COALESCE(sp.Manufacturer, ''),
							COALESCE(sp.Country, '')
						ORDER BY sp.InvoiceDate DESC, sp.Price DESC
					) AS rn
				FROM SupplierPrice sp 
				WHERE sp.SupplierID = CAST(@supplierID AS UUID)
				  AND sp.IsActive = 1
				  AND sp.GUID_ES IS NOT NULL
		`
		args = []interface{}{sql.Named("supplierID", supplierID)}
		query += supplierScope
		query += priceListScope

		query += `
			)
			SELECT
				CAST(lp.GUID_ES AS TEXT) AS GUID_ES,
				CAST(lp.SupplierPriceID AS TEXT) AS SupplierPriceID,
				NULL AS SupplierID,
				NULL AS SupplierName,
				ef2.NAME AS DrugName,
				ef2.INN_NAME_RUS AS INN,
				ef2.CUREFORM_NAME AS CureForm,
				ef2.BARCODE AS Barcode,
				lp.Price AS BasePrice,
				lp.MarkupPct,
				lp.FinalPrice AS Price,
				lp.Quantity AS Quantity,
				lp.BatchNumber AS BatchNumber,
				lp.ExpiryDate AS ExpiryDate,
				lp.Manufacturer AS Manufacturer,
				lp.InvoiceDate AS LastPriceDate,
				lp.MatchMethod,
				lp.MatchConfidence,
				ef2.TRN_NAME_RUS AS TradeName,
				ef2.DOSAGE AS Dosage,
				ef2.REESTR_PRICE AS RegistryPrice,
				CAST(ef2.C_INSTRUCTION AS TEXT) AS InstructionGUID,
				ef2.DISCRIBE AS Description,
				ef2.STORING_CONDITION AS StoringCondition,
				ef2.SROK_SAVED AS ExpiryPeriod,
				ep.PRODUCER_NAME AS ProducerName,
				ef2.DATA_REG AS RegistryDate,
				ef2.KOD_ES AS ES_Code,
				ef2.REGISTR_STATUS AS RegistryStatus
			FROM LatestPrices lp
			LEFT JOIN es_ef2 ef2  ON lp.GUID_ES = ef2.GUID_ES
			LEFT JOIN (
				SELECT KOD_PRODUCER, MIN(PRODUCER_NAME) AS PRODUCER_NAME
				FROM es_producer 
				GROUP BY KOD_PRODUCER
			) ep ON ep.KOD_PRODUCER = ef2.PRODUCER_COD
			WHERE lp.rn = 1
			  AND (@q = '' OR ef2.NAME LIKE @q)
			ORDER BY COALESCE(ef2.NAME, ''), ef2.NAME, COALESCE(lp.BatchNumber, ''), lp.ExpiryDate
			OFFSET @offset LIMIT @limit
		`
	} else {
		// Сводный прайс для всех поставщиков (группировка по препарату и поставщику)
		query = `
			WITH LatestPrices AS (
				SELECT
					sp.GUID_ES,
					sp.SupplierPriceID,
					sp.SupplierID,
					sp.BatchNumber,
					sp.ExpiryDate,
					sp.Manufacturer,
					sp.Country,
					sp.Price,
					sp.Quantity,
					sp.InvoiceDate,
					sp.MatchMethod,
					sp.MatchConfidence,
					sp.RegionID,
					COALESCE(sp.MarkupPct, 0) AS MarkupPct,
					` + finalPriceExpr + ` AS FinalPrice,
					ROW_NUMBER() OVER (
						PARTITION BY 
							sp.GUID_ES, 
							sp.SupplierID,
							COALESCE(sp.BatchNumber, ''), 
							COALESCE(CAST(sp.ExpiryDate AS TEXT), ''),
							COALESCE(sp.Manufacturer, ''),
							COALESCE(sp.Country, '')
						ORDER BY sp.InvoiceDate DESC, sp.Price DESC
					) AS rn
				FROM SupplierPrice sp 
				WHERE sp.IsActive = 1
				  AND sp.GUID_ES IS NOT NULL
		`
		args = []interface{}{}
		query += supplierScope
		query += priceListScope

		query += `
			)
			SELECT
				CAST(lp.GUID_ES AS TEXT) AS GUID_ES,
				CAST(lp.SupplierPriceID AS TEXT) AS SupplierPriceID,
				CAST(lp.SupplierID AS TEXT) AS SupplierID,
				s.Name AS SupplierName,
				ef2.NAME AS DrugName,
				ef2.INN_NAME_RUS AS INN,
				ef2.CUREFORM_NAME AS CureForm,
				ef2.BARCODE AS Barcode,
				lp.Price AS BasePrice,
				lp.MarkupPct,
				lp.FinalPrice AS Price,
				lp.Quantity AS Quantity,
				lp.BatchNumber AS BatchNumber,
				lp.ExpiryDate AS ExpiryDate,
				lp.Manufacturer AS Manufacturer,
				lp.InvoiceDate AS LastPriceDate,
				lp.MatchMethod,
				lp.MatchConfidence,
				ef2.TRN_NAME_RUS AS TradeName,
				ef2.DOSAGE AS Dosage,
				ef2.REESTR_PRICE AS RegistryPrice,
				CAST(ef2.C_INSTRUCTION AS TEXT) AS InstructionGUID,
				ef2.DISCRIBE AS Description,
				ef2.STORING_CONDITION AS StoringCondition,
				ef2.SROK_SAVED AS ExpiryPeriod,
				ep.PRODUCER_NAME AS ProducerName,
				ef2.DATA_REG AS RegistryDate,
				ef2.KOD_ES AS ES_Code,
				ef2.REGISTR_STATUS AS RegistryStatus
			FROM LatestPrices lp
			LEFT JOIN es_ef2 ef2  ON lp.GUID_ES = ef2.GUID_ES
			LEFT JOIN Supplier s  ON lp.SupplierID = s.SupplierID AND s.IsActive = 1
			LEFT JOIN (
				SELECT KOD_PRODUCER, MIN(PRODUCER_NAME) AS PRODUCER_NAME
				FROM es_producer 
				GROUP BY KOD_PRODUCER
			) ep ON ep.KOD_PRODUCER = ef2.PRODUCER_COD
			WHERE lp.rn = 1
			  AND (@q = '' OR ef2.NAME LIKE @q)
			ORDER BY COALESCE(ef2.NAME, ''), ef2.NAME, s.Name, COALESCE(lp.BatchNumber, ''), lp.ExpiryDate, lp.FinalPrice
			OFFSET @offset LIMIT @limit
		`
	}

	// Общие параметры обеих веток: фильтр по имени и постраничность.
	args = append(args, sql.Named("q", qLike), sql.Named("offset", offsetVal), sql.Named("limit", limitVal))

	if s.logger != nil {
		s.logger.Debug("Выполнение SQL запроса для сводного прайса")
	}

	// Диагностические COUNT/COUNT(DISTINCT) убраны: они выполняли два лишних
	// прохода по SupplierPrice на каждый запрос сводного прайса только ради лог-строк.

	// Retry логика для обработки deadlock
	maxRetries := 3
	var rows *sql.Rows
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		rows, err = s.database.QueryContext(ctx, query, args...)
		if err == nil {
			break
		}
		
		// Проверяем, является ли ошибка deadlock
		errStr := err.Error()
		if strings.Contains(errStr, "взаимоблокировка") || 
		   strings.Contains(errStr, "deadlock") ||
		   strings.Contains(errStr, "victim") {
			if s.logger != nil {
				s.logger.Warn("Обнаружена взаимоблокировка при запросе сводного прайса (попытка %d/%d): %v", attempt+1, maxRetries, err)
			}
			if attempt < maxRetries-1 {
				// Ждем перед повтором (экспоненциальная задержка)
				waitTime := time.Duration(attempt+1) * 100 * time.Millisecond
				time.Sleep(waitTime)
				continue
			}
		}
		
		// Если это не deadlock или закончились попытки
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL запроса для сводного прайса: %v", err)
			s.logger.Error("SQL: %s", query)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения сводки: %v", err))
		return
	}
	defer rows.Close()

	type PriceSummary struct {
		GUID_ES         string  `json:"guid_es"`
		SupplierPriceID string  `json:"supplier_price_id"` // PK конкретной партии — для POST /api/buyer/orders
		SupplierID      *string `json:"supplier_id,omitempty"`
		SupplierName *string `json:"supplier_name,omitempty"`
		DrugName     *string `json:"drug_name,omitempty"`
		INN          *string `json:"inn,omitempty"`
		CureForm     *string `json:"cure_form,omitempty"`
		Barcode      *string `json:"barcode,omitempty"`
		// BasePrice и MarkupPct не передаются клиенту для безопасности
		Price           *float64 `json:"price"`              // Финальная цена (рассчитана на сервере с учетом наценки)
		Quantity        *float64 `json:"quantity,omitempty"` // Количество партии на остатке
		BatchNumber     *string  `json:"batch_number,omitempty"` // Номер партии/серии
		ExpiryDate      *string  `json:"expiry_date,omitempty"`   // Срок годности партии
		Manufacturer    *string  `json:"manufacturer,omitempty"`   // Производитель
		Country         *string  `json:"country,omitempty"`        // Страна
		RegionID        *string  `json:"region_id,omitempty"`
		RegionName      *string  `json:"region_name,omitempty"`
		LastPriceDate   *string  `json:"last_price_date,omitempty"`
		MatchMethod     *string  `json:"match_method,omitempty"`
		MatchConfidence *float64 `json:"match_confidence,omitempty"`
		// Дополнительные поля из es_ef2
		TradeName        *string  `json:"trade_name,omitempty"`
		Dosage           *string  `json:"dosage,omitempty"`
		RegistryPrice    *float64 `json:"registry_price,omitempty"`
		InstructionGUID  *string  `json:"instruction_guid,omitempty"`
		Description      *string  `json:"description,omitempty"`
		StoringCondition *string  `json:"storing_condition,omitempty"`
		ExpiryPeriod     *string  `json:"expiry_period,omitempty"`
		ProducerName     *string  `json:"producer_name,omitempty"`
		RegistryDate     *string  `json:"registry_date,omitempty"`
		ES_Code          *int64   `json:"es_code,omitempty"`
		RegistryStatus   *string  `json:"registry_status,omitempty"`
	}

	var summary []PriceSummary
	for rows.Next() {
		var ps PriceSummary
		var supplierID sql.NullString
		var supplierName sql.NullString
		var drugName, inn, cureForm, barcode, matchMethod sql.NullString
		var batchNumber, manufacturer sql.NullString
		var expiryDate sql.NullTime
		var lastPriceDate sql.NullTime
		var basePrice, finalPrice, matchConfidence, quantity sql.NullFloat64
		// Дополнительные поля из es_ef2
		var tradeName, dosage, instructionGUID, description, storingCondition, expiryPeriod sql.NullString
		var producerName, registryStatus sql.NullString
		var registryDate sql.NullTime
		var registryPrice sql.NullFloat64
		var esCode sql.NullInt64

		err := rows.Scan(
			&ps.GUID_ES,
			&ps.SupplierPriceID,
			&supplierID,
			&supplierName,
			&drugName,
			&inn,
			&cureForm,
			&barcode,
			&basePrice,
			&sql.NullFloat64{}, // MarkupPct - пропускаем, не передаем клиенту
			&finalPrice,
			&quantity,
			&batchNumber,
			&expiryDate,
			&manufacturer,
			// Country, RegionID, RegionName убраны из SQL запроса
			&lastPriceDate,
			&matchMethod,
			&matchConfidence,
			// Дополнительные поля
			&tradeName,
			&dosage,
			&registryPrice,
			&instructionGUID,
			&description,
			&storingCondition,
			&expiryPeriod,
			&producerName,
			&registryDate,
			&esCode,
			&registryStatus,
		)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования сводного прайса: %v", err)
			}
			continue
		}

		if supplierID.Valid {
			ps.SupplierID = &supplierID.String
		}
		if supplierName.Valid {
			ps.SupplierName = &supplierName.String
		}
		if drugName.Valid {
			ps.DrugName = &drugName.String
		}
		if inn.Valid {
			ps.INN = &inn.String
		}
		if cureForm.Valid {
			ps.CureForm = &cureForm.String
		}
		if barcode.Valid {
			ps.Barcode = &barcode.String
		}
		// BasePrice и MarkupPct не передаются клиенту - расчеты только на сервере
		if finalPrice.Valid {
			ps.Price = &finalPrice.Float64 // Только финальная цена передается клиенту
		}
		if quantity.Valid {
			ps.Quantity = &quantity.Float64
		}
		if batchNumber.Valid {
			ps.BatchNumber = &batchNumber.String
		}
		if expiryDate.Valid {
			ps.ExpiryDate = stringPtr(expiryDate.Time.Format(time.RFC3339))
		}
		if manufacturer.Valid {
			ps.Manufacturer = &manufacturer.String
		}
		// Country, RegionID, RegionName не используются в сводном прайсе - все данные о препарате из ЕС
		if lastPriceDate.Valid {
			ps.LastPriceDate = stringPtr(lastPriceDate.Time.Format(time.RFC3339))
		}
		if matchMethod.Valid {
			ps.MatchMethod = &matchMethod.String
		}
		if matchConfidence.Valid {
			ps.MatchConfidence = &matchConfidence.Float64
		}

		// Обработка дополнительных полей из es_ef2
		if tradeName.Valid {
			ps.TradeName = &tradeName.String
		}
		if dosage.Valid {
			ps.Dosage = &dosage.String
		}
		if registryPrice.Valid {
			ps.RegistryPrice = &registryPrice.Float64
		}
		if instructionGUID.Valid {
			ps.InstructionGUID = &instructionGUID.String
		}
		if description.Valid {
			ps.Description = &description.String
		}
		if storingCondition.Valid {
			ps.StoringCondition = &storingCondition.String
		}
		if expiryPeriod.Valid {
			ps.ExpiryPeriod = &expiryPeriod.String
		}
		if producerName.Valid {
			ps.ProducerName = &producerName.String
		}
		if registryDate.Valid {
			ps.RegistryDate = stringPtr(registryDate.Time.Format(time.RFC3339))
		}
		if esCode.Valid {
			ps.ES_Code = &esCode.Int64
		}
		if registryStatus.Valid {
			ps.RegistryStatus = &registryStatus.String
		}

		summary = append(summary, ps)
	}

	// Проверяем ошибки после итерации
	if err := rows.Err(); err != nil {
		errStr := err.Error()
		// Если это deadlock при итерации, просто логируем и продолжаем с уже полученными данными
		if strings.Contains(errStr, "взаимоблокировка") || 
		   strings.Contains(errStr, "deadlock") ||
		   strings.Contains(errStr, "victim") {
			if s.logger != nil {
				s.logger.Warn("Обнаружена взаимоблокировка при итерации строк сводного прайса (частичные данные могут быть потеряны): %v", err)
				s.logger.Info("Возвращаем %d записей из сводного прайса (некоторые данные могли быть потеряны из-за deadlock)", len(summary))
			}
			// Продолжаем с уже полученными данными, не прерываем запрос
		} else {
			// Другие ошибки
			if s.logger != nil {
				s.logger.Error("Ошибка при итерации строк сводного прайса: %v", err)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обработки данных: %v", err))
			return
		}
	}

	if summary == nil {
		summary = []PriceSummary{}
	}

	if s.logger != nil {
		s.logger.Info("Успешно получено позиций в сводном прайсе: %d", len(summary))
	}

	// Реальный итог позиций во всём (отфильтрованном) прайсе — чтобы десктоп
	// корректно догружал постранично, а не останавливался на первой странице.
	// total = offset + отдано. Тяжёлый GROUP BY-COUNT гоняем ТОЛЬКО если вернули полную
	// страницу (возможно есть ещё). Неполная страница → total точно известен, COUNT не нужен
	// (раньше COUNT дублировал дедупликацию на КАЖДЫЙ запрос — сводный прайс «висел»).
	totalDrugs := offsetVal + len(summary)
	if len(summary) == limitVal {
		supplierFilter, join, nameFilter := "", "", ""
		countArgs := []interface{}{}
		if supplierID != "" {
			supplierFilter = " AND sp.SupplierID = CAST(@supplierID AS UUID)"
			countArgs = append(countArgs, sql.Named("supplierID", supplierID))
		}
		if qLike != "" {
			join = " LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES"
			nameFilter = " AND ef2.NAME LIKE @q"
			countArgs = append(countArgs, sql.Named("q", qLike))
		}
		countQuery := `SELECT COUNT(*) FROM (
			SELECT 1 FROM SupplierPrice sp` + join + `
			WHERE sp.IsActive = 1 AND sp.GUID_ES IS NOT NULL` + supplierFilter + supplierScope + priceListScope + nameFilter + `
			GROUP BY sp.GUID_ES, sp.SupplierID,
			         COALESCE(sp.BatchNumber, ''),
			         COALESCE(CAST(sp.ExpiryDate AS TEXT), ''),
			         COALESCE(sp.Manufacturer, ''),
			         COALESCE(sp.Country, '')
		) t`
		var cnt int64
		if e := s.database.GORMWith(ctx).Raw(countQuery, countArgs...).Scan(&cnt).Error; e != nil {
			if s.logger != nil {
				s.logger.Warn("Сводный прайс: не удалось посчитать total_drugs: %v", e)
			}
		} else if int(cnt) > totalDrugs {
			totalDrugs = int(cnt)
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"supplier_id": supplierID,
		"total_drugs": totalDrugs,
		"summary":     summary,
	})
}

// stringPtr создает указатель на строку
func stringPtr(s string) *string {
	return &s
}
