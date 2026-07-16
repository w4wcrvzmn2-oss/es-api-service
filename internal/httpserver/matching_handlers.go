package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/matching"
	"fmt"
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	supplierID := r.URL.Query().Get("supplier_id")
	if supplierID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_id")
		return
	}

	// Валидация UUID для защиты от SQL injection
	if _, err := uuid.Parse(supplierID); err != nil {
		if s.logger != nil {
			s.logger.Warn("Недопустимый формат supplier_id: %s", supplierID)
		}
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_id")
		return
	}

	// Валидация UUID для защиты от SQL injection
	if _, err := uuid.Parse(supplierID); err != nil {
		if s.logger != nil {
			s.logger.Warn("Недопустимый формат supplier_id: %s", supplierID)
		}
		s.writeError(w, http.StatusBadRequest, "Недопустимый формат supplier_id")
		return
	}

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

	// Параметр для включения неактивных записей (для страницы сопоставления)
	includeInactive := r.URL.Query().Get("include_inactive") == "true"

	// Пагинация
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

	// Фильтр только по последнему импорту (по умолчанию true)
	latestOnly := r.URL.Query().Get("latest_only") != "false"

	// Определяем WHERE-условие: только последний импорт или все записи
	supplierWhere := `sp.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)`
	if latestOnly {
		supplierWhere = `sp.InvoiceImportID IN (
			SELECT lii.InvoiceImportID FROM InvoiceImport lii
			WHERE lii.ImportPointID IN (
				SELECT pl.ImportPointID FROM PriceList pl
				WHERE pl.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER) AND pl.IsActive = 1
			)
			AND lii.ImportStatus = 'COMPLETED'
			AND lii.CompletedAt = (
				SELECT MAX(lii2.CompletedAt) FROM InvoiceImport lii2
				WHERE lii2.ImportPointID = lii.ImportPointID AND lii2.ImportStatus = 'COMPLETED'
			)
		)`
	}

	query := fmt.Sprintf(`
		SELECT
			CAST(sp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
			CAST(sp.SupplierID AS NVARCHAR(50)) AS SupplierID,
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS NVARCHAR(50)) END AS GUID_ES,
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
			CAST(sp.RegionID AS NVARCHAR(50)) AS RegionID,
			r.Name AS RegionName,
			ISNULL(sp.MarkupPct, 0) AS MarkupPct,
			sp.Price * (1 + ISNULL(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
			sp.CreatedAt,
			ep.PRODUCER_NAME AS ProducerName
		FROM SupplierPrice sp
		LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r ON sp.RegionID = r.RegionID
		OUTER APPLY (
			SELECT TOP 1 PRODUCER_NAME
			FROM es_producer ep
			WHERE ep.KOD_PRODUCER = ef2.PRODUCER_COD
		) ep
		WHERE %s
	`, supplierWhere)

	if !includeInactive {
		query += ` AND sp.IsActive = 1`
	}

	var args []interface{}
	args = append(args, sql.Named("supplierID", supplierID))

	if regionID != "" {
		query += ` AND (sp.RegionID = CAST(@regionID AS UNIQUEIDENTIFIER) OR sp.RegionID IS NULL)`
		args = append(args, sql.Named("regionID", regionID))
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
		query += fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", offsetVal, limitVal)
	}

	if s.logger != nil {
		s.logger.Info("Выполнение SQL запроса для получения прайсов поставщика: %s", supplierID)
		s.logger.Debug("SQL запрос: %s", query)
		s.logger.Debug("Параметры запроса: supplierID=%s, regionID=%s, includeInactive=%v", supplierID, regionID, includeInactive)
	}

	rows, err := s.database.GORMWith(ctx).Raw(query, args...).Rows()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL запроса для прайсов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения прайса: %v", err))
		return
	}
	defer rows.Close()

	// Статистика — используем тот же фильтр что и основной запрос
	countWhere := supplierWhere
	if !includeInactive {
		countWhere += ` AND sp.IsActive = 1`
	}

	var totalInDB, totalNullGuid, totalEmptyGuid int
	statsQuery := fmt.Sprintf(`
		SELECT
			COUNT(*),
			ISNULL(SUM(CASE WHEN sp.GUID_ES IS NULL THEN 1 ELSE 0 END), 0),
			ISNULL(SUM(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS NVARCHAR(50)) = '' THEN 1 ELSE 0 END), 0)
		FROM SupplierPrice sp
		WHERE %s
	`, countWhere)
	countErr := s.database.GORMWith(ctx).Raw(statsQuery, sql.Named("supplierID", supplierID)).Row().Scan(&totalInDB, &totalNullGuid, &totalEmptyGuid)
	if countErr != nil && s.logger != nil {
		s.logger.Error("Ошибка подсчета записей: %v", countErr)
	} else if s.logger != nil {
		s.logger.Info("Статистика для поставщика %s: всего=%d, NULL GUID=%d, пустых GUID=%d", supplierID, totalInDB, totalNullGuid, totalEmptyGuid)
	}

	// Счетчики для статистики
	rowCount := 0
	matchedCount := 0
	unmatchedCount := 0
	nullGuidCount := 0
	emptyGuidCount := 0

	type SupplierPriceView struct {
		SupplierPriceID  string  `json:"supplier_price_id"`
		SupplierID       string  `json:"supplier_id"`
		GUID_ES          *string `json:"guid_es,omitempty"`
		DrugName         *string `json:"drug_name,omitempty"`
		INN              *string `json:"inn,omitempty"`
		CureForm         *string `json:"cure_form,omitempty"`
		Barcode          *string `json:"barcode,omitempty"`
		ProducerName     *string `json:"producer_name,omitempty"`
		SupplierItemCode *string `json:"supplier_item_code,omitempty"`
		SupplierItemName *string `json:"supplier_item_name,omitempty"`
		// Price убран - используется FinalPrice с учетом наценки
		Quantity        *float64 `json:"quantity,omitempty"`
		InvoiceNumber   *string  `json:"invoice_number,omitempty"`
		InvoiceDate     *string  `json:"invoice_date,omitempty"`
		BatchNumber     *string  `json:"batch_number,omitempty"`
		ExpiryDate      *string  `json:"expiry_date,omitempty"`
		MatchMethod     *string  `json:"match_method,omitempty"`
		MatchConfidence *float64 `json:"match_confidence,omitempty"`
		IsActive        bool     `json:"is_active"`
		RegionID        *string  `json:"region_id,omitempty"`
		RegionName      *string  `json:"region_name,omitempty"`
		// MarkupPct не передается клиенту для безопасности - расчеты только на сервере
		// FinalPrice также не передается - используется только FinalPrice
		FinalPrice    *float64 `json:"price"` // Финальная цена (рассчитана на сервере с учетом наценки)
		LastPriceDate *string  `json:"last_price_date,omitempty"`
		CreatedAt     string   `json:"created_at"`
	}

	var prices []SupplierPriceView
	processedCount := 0
	skippedCount := 0

	for rows.Next() {
		processedCount++
		var sp SupplierPriceView
		var guidES, drugName, inn, cureForm, barcode, producerName, supplierItemCode, supplierItemName sql.NullString
		var invoiceNumber, batchNumber, matchMethod, regionID, regionName sql.NullString
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
			&regionID,
			&regionName,
			&sql.NullFloat64{}, // MarkupPct - пропускаем, не передаем клиенту
			&finalPrice,
			&sp.CreatedAt,
			&producerName, // Производитель из es_producer
		)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования SupplierPrice: %v", err)
			}
			continue
		}

		rowCount++
		// Проверяем, есть ли GUID_ES (не NULL и не пустая строка)
		if !guidES.Valid {
			// GUID_ES NULL в базе данных - это точно несопоставленная запись
			nullGuidCount++
			unmatchedCount++
		} else if strings.TrimSpace(guidES.String) == "" {
			// GUID_ES пустая строка - тоже несопоставленная
			emptyGuidCount++
			unmatchedCount++
		} else {
			// GUID_ES есть и не пустой - сопоставленная запись
			sp.GUID_ES = &guidES.String
			matchedCount++
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
		// BasePrice - базовая цена (для внутреннего использования, не передается клиенту)
		// Price - финальная цена с учетом наценки (передается клиенту)
		if basePrice.Valid {
			// Не передаем basePrice клиенту для безопасности
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
		// MarkupPct не передается клиенту - расчеты только на сервере
		if finalPrice.Valid {
			sp.FinalPrice = &finalPrice.Float64 // Только финальная цена передается клиенту
		}

		prices = append(prices, sp)
	}

	if prices == nil {
		prices = []SupplierPriceView{}
	}

	if s.logger != nil {
		s.logger.Info("Обработка завершена: обработано строк=%d, пропущено=%d, добавлено в результат=%d",
			processedCount, skippedCount, len(prices))
		if processedCount != rowCount {
			s.logger.Warn("Расхождение: rowCount=%d, но processedCount=%d", rowCount, processedCount)
		}
	}

	// Логируем статистику по сопоставленным/несопоставленным
	matchedCountFinal := 0
	unmatchedCountFinal := 0
	for _, p := range prices {
		if p.GUID_ES != nil && strings.TrimSpace(*p.GUID_ES) != "" {
			matchedCountFinal++
		} else {
			unmatchedCountFinal++
		}
	}
	if s.logger != nil {
		s.logger.Info("Строк обработано из БД: %d (сопоставлено: %d, не сопоставлено: %d, из них NULL: %d, пустых: %d)",
			rowCount, matchedCount, unmatchedCount, nullGuidCount, emptyGuidCount)
		s.logger.Info("Записей в результате: всего %d, сопоставлено %d, не сопоставлено %d", len(prices), matchedCountFinal, unmatchedCountFinal)
		if rowCount != len(prices) {
			s.logger.Warn("Расхождение: обработано строк %d, но в результате %d записей", rowCount, len(prices))
		}
		if matchedCount != matchedCountFinal || unmatchedCount != unmatchedCountFinal {
			s.logger.Warn("Расхождение в подсчете: при сканировании (сопоставлено: %d, не сопоставлено: %d), в результате (сопоставлено: %d, не сопоставлено: %d)",
				matchedCount, unmatchedCount, matchedCountFinal, unmatchedCountFinal)
		}
	}

	dbUnmatched := totalNullGuid + totalEmptyGuid
	dbMatched := totalInDB - dbUnmatched

	responseData := map[string]interface{}{
		"supplier_id":  supplierID,
		"total_prices": len(prices),
		"prices":       prices,
		"stats": map[string]interface{}{
			"total_in_db":      totalInDB,
			"matched_final":    dbMatched,
			"unmatched_final":  dbUnmatched,
			"prices_returned":  len(prices),
		},
	}

	if s.logger != nil {
		s.logger.Info("Ответ handleGetSupplierPrices: всего в БД=%d, обработано=%d, возвращено=%d, сопоставлено=%d, не сопоставлено=%d",
			totalInDB, rowCount, len(prices), matchedCountFinal, unmatchedCountFinal)
		if totalInDB != len(prices) {
			s.logger.Warn("ВНИМАНИЕ: В БД записей=%d, но возвращено только=%d. Возможна проблема с запросом или фильтрацией!",
				totalInDB, len(prices))
		}
	}

	s.writeJSON(w, http.StatusOK, responseData)
}

// handleGetSupplierPriceByID возвращает одну запись SupplierPrice по её ID.
// ID извлекается роутером handleSupplierPricesRouter из пути /api/supplier-prices/{id}
// и кладётся в контекст под ключом "priceID".
// В отличие от handleGetSupplierPrices возвращает запись независимо от IsActive —
// клиент явно указал ID и должен увидеть конкретную запись (в т.ч. отключённую).
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
			CAST(sp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
			CAST(sp.SupplierID AS NVARCHAR(50)) AS SupplierID,
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS NVARCHAR(50)) END AS GUID_ES,
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
			CAST(sp.RegionID AS NVARCHAR(50)) AS RegionID,
			r.Name AS RegionName,
			ISNULL(sp.MarkupPct, 0) AS MarkupPct,
			sp.Price * (1 + ISNULL(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
			sp.CreatedAt,
			ep.PRODUCER_NAME AS ProducerName
		FROM SupplierPrice sp WITH (NOLOCK)
		LEFT JOIN es_ef2 ef2 WITH (NOLOCK) ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r WITH (NOLOCK) ON sp.RegionID = r.RegionID
		OUTER APPLY (
			SELECT TOP 1 PRODUCER_NAME
			FROM es_producer ep WITH (NOLOCK)
			WHERE ep.KOD_PRODUCER = ef2.PRODUCER_COD
		) ep
		WHERE sp.SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)
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
			COUNT(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS NVARCHAR(50)) <> '' THEN 1 END),
			COUNT(CASE WHEN sp.GUID_ES IS NULL OR CAST(sp.GUID_ES AS NVARCHAR(50)) = '' THEN 1 END)
		FROM SupplierPrice sp
		WHERE sp.IsActive = 1
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
					ISNULL(sp.MarkupPct, 0) AS MarkupPct,
					sp.Price * (1 + ISNULL(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
					-- Группируем по GUID_ES, BatchNumber, ExpiryDate, Manufacturer, Country для разделения партий
					-- Если поля NULL, считаем их как отдельную группу
					ROW_NUMBER() OVER (
						PARTITION BY 
							sp.GUID_ES, 
							ISNULL(sp.BatchNumber, ''), 
							ISNULL(CAST(sp.ExpiryDate AS NVARCHAR(50)), ''),
							ISNULL(sp.Manufacturer, ''),
							ISNULL(sp.Country, '')
						ORDER BY sp.InvoiceDate DESC, sp.Price DESC
					) AS rn
				FROM SupplierPrice sp WITH (NOLOCK)
				WHERE sp.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
				  AND sp.IsActive = 1
				  AND sp.GUID_ES IS NOT NULL
				  -- Если запись сопоставлена (GUID_ES IS NOT NULL), включаем её в сводный прайс независимо от состояния PriceList
				  -- PriceList проверяется только для несопоставленных записей, но здесь мы уже фильтруем только сопоставленные
		`
		args = []interface{}{sql.Named("supplierID", supplierID)}

		// regionID не используется в сводном прайсе - регионы не фильтруются

		query += `
			)
			SELECT TOP 5000
				CAST(lp.GUID_ES AS NVARCHAR(50)) AS GUID_ES,
				CAST(lp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
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
				-- Country и Region не отображаются в сводном прайсе - все данные о препарате из ЕС
				lp.InvoiceDate AS LastPriceDate,
				lp.MatchMethod,
				lp.MatchConfidence,
				-- Дополнительные поля из es_ef2
				ef2.TRN_NAME_RUS AS TradeName,
				ef2.DOSAGE AS Dosage,
				ef2.REESTR_PRICE AS RegistryPrice,
				CAST(ef2.C_INSTRUCTION AS NVARCHAR(50)) AS InstructionGUID,
				ef2.DISCRIBE AS Description,
				ef2.STORING_CONDITION AS StoringCondition,
				ef2.SROK_SAVED AS ExpiryPeriod,
				ep.PRODUCER_NAME AS ProducerName,
				ef2.DATA_REG AS RegistryDate,
				ef2.KOD_ES AS ES_Code,
				ef2.REGISTR_STATUS AS RegistryStatus
			FROM LatestPrices lp
			LEFT JOIN es_ef2 ef2 WITH (NOLOCK) ON lp.GUID_ES = ef2.GUID_ES
			-- Region не используется в сводном прайсе
			OUTER APPLY (
				SELECT TOP 1 PRODUCER_NAME
				FROM es_producer ep WITH (NOLOCK)
				WHERE ep.KOD_PRODUCER = ef2.PRODUCER_COD
			) ep
			WHERE lp.rn = 1
			ORDER BY ISNULL(ef2.NAME, ''), ef2.NAME, ISNULL(lp.BatchNumber, ''), lp.ExpiryDate
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
					ISNULL(sp.MarkupPct, 0) AS MarkupPct,
					sp.Price * (1 + ISNULL(sp.MarkupPct, 0) / 100.0) AS FinalPrice,
					-- Группируем по GUID_ES, SupplierID, BatchNumber, ExpiryDate, Series, Manufacturer, Country для разделения партий
					ROW_NUMBER() OVER (
						PARTITION BY 
							sp.GUID_ES, 
							sp.SupplierID,
							ISNULL(sp.BatchNumber, ''), 
							ISNULL(CAST(sp.ExpiryDate AS NVARCHAR(50)), ''),
							ISNULL(sp.Manufacturer, ''),
							ISNULL(sp.Country, '')
						ORDER BY sp.InvoiceDate DESC, sp.Price DESC
					) AS rn
				FROM SupplierPrice sp WITH (NOLOCK)
				WHERE sp.IsActive = 1
				  AND sp.GUID_ES IS NOT NULL
				  -- Если запись сопоставлена (GUID_ES IS NOT NULL), включаем её в сводный прайс независимо от состояния PriceList
				  -- PriceList проверяется только для несопоставленных записей, но здесь мы уже фильтруем только сопоставленные
		`
		args = []interface{}{}

		// regionID не используется в сводном прайсе - регионы не фильтруются

		query += `
			)
			SELECT TOP 5000
				CAST(lp.GUID_ES AS NVARCHAR(50)) AS GUID_ES,
				CAST(lp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
				CAST(lp.SupplierID AS NVARCHAR(50)) AS SupplierID,
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
				-- Country и Region не отображаются в сводном прайсе - все данные о препарате из ЕС
				lp.InvoiceDate AS LastPriceDate,
				lp.MatchMethod,
				lp.MatchConfidence,
				-- Дополнительные поля из es_ef2
				ef2.TRN_NAME_RUS AS TradeName,
				ef2.DOSAGE AS Dosage,
				ef2.REESTR_PRICE AS RegistryPrice,
				CAST(ef2.C_INSTRUCTION AS NVARCHAR(50)) AS InstructionGUID,
				ef2.DISCRIBE AS Description,
				ef2.STORING_CONDITION AS StoringCondition,
				ef2.SROK_SAVED AS ExpiryPeriod,
				ep.PRODUCER_NAME AS ProducerName,
				ef2.DATA_REG AS RegistryDate,
				ef2.KOD_ES AS ES_Code,
				ef2.REGISTR_STATUS AS RegistryStatus
			FROM LatestPrices lp
			LEFT JOIN es_ef2 ef2 WITH (NOLOCK) ON lp.GUID_ES = ef2.GUID_ES
			LEFT JOIN Supplier s WITH (NOLOCK) ON lp.SupplierID = s.SupplierID AND s.IsActive = 1
			-- Region не используется в сводном прайсе - все данные о препарате из ЕС
			OUTER APPLY (
				SELECT TOP 1 PRODUCER_NAME
				FROM es_producer ep WITH (NOLOCK)
				WHERE ep.KOD_PRODUCER = ef2.PRODUCER_COD
			) ep
			WHERE lp.rn = 1
			ORDER BY ISNULL(ef2.NAME, ''), ef2.NAME, s.Name, ISNULL(lp.BatchNumber, ''), lp.ExpiryDate, lp.FinalPrice
		`
	}

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

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"supplier_id": supplierID,
		"total_drugs": len(summary),
		"summary":     summary,
	})
}

// stringPtr создает указатель на строку
func stringPtr(s string) *string {
	return &s
}
