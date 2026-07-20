package httpserver

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type SCDashboard struct {
	PriceListCount  int     `json:"price_list_count"`
	TotalPositions  int     `json:"total_positions"`
	TodayOrderCount int     `json:"today_order_count"`
	TodayOrderSum   float64 `json:"today_order_sum"`
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) supplierIDFromClaims(r *http.Request) string {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		return ""
	}
	return claims.SupplierID
}

func (s *Server) handleSCDashboard(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	var dash SCDashboard
	ctx := r.Context()
	g := s.database.GORMWith(ctx)

	_ = g.Raw(
		`SELECT COUNT(*) FROM PriceList WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER) AND IsActive=1`,
		sql.Named("sid", sid),
	).Row().Scan(&dash.PriceListCount)

	_ = g.Raw(
		`SELECT COUNT(*) FROM SupplierPrice sp
		 WHERE sp.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		   AND sp.InvoiceImportID = (
			SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
			JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
			WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
			  AND ii.ImportStatus = 'COMPLETED'
			ORDER BY ii.CompletedAt DESC
		   )`,
		sql.Named("sid", sid),
	).Row().Scan(&dash.TotalPositions)

	_ = g.Raw(
		`SELECT COUNT(DISTINCT o.OrderID)
		 FROM [Order] o
		 JOIN OrderItem oi ON oi.OrderID = o.OrderID
		 WHERE oi.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		   AND CAST(o.CreatedAt AS DATE)=CAST(GETDATE() AS DATE)`,
		sql.Named("sid", sid),
	).Row().Scan(&dash.TodayOrderCount)

	_ = g.Raw(
		`SELECT ISNULL(SUM(oi.Quantity * oi.UnitPrice), 0)
		 FROM [Order] o
		 JOIN OrderItem oi ON oi.OrderID = o.OrderID
		 WHERE oi.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		   AND CAST(o.CreatedAt AS DATE)=CAST(GETDATE() AS DATE)`,
		sql.Named("sid", sid),
	).Row().Scan(&dash.TodayOrderSum)

	writeJSON(w, http.StatusOK, dash)
}

// --- Profile ---

func (s *Server) handleSCProfile(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	if r.Method == http.MethodGet {
		var name, inn, contacts, address, contractNum, deliveryAddress string
		err := s.database.GORMWith(r.Context()).Raw(
			`SELECT ISNULL(Name,''), ISNULL(INN,''), ISNULL(Contacts,''), ISNULL(Address,''), ISNULL(ContractNumber,''), ISNULL(DeliveryAddress,'')
			 FROM Supplier WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
			sql.Named("sid", sid),
		).Row().Scan(&name, &inn, &contacts, &address, &contractNum, &deliveryAddress)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"name": name, "inn": inn, "contacts": contacts,
			"address": address, "contract_number": contractNum,
			"delivery_address": deliveryAddress,
		})
		return
	}

	if r.Method == http.MethodPut {
		var body struct {
			Address         *string `json:"address"`
			DeliveryAddress *string `json:"delivery_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный JSON"})
			return
		}
		if body.DeliveryAddress != nil {
			err := s.database.GORMWith(r.Context()).Exec(
				`UPDATE Supplier SET DeliveryAddress=@addr WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
				sql.Named("addr", *body.DeliveryAddress), sql.Named("sid", sid),
			).Error
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "GET/PUT only"})
}

// --- Price Lists ---

func (s *Server) handleSCPriceLists(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	type PL struct {
		PriceListID       string     `json:"price_list_id"`
		Name              string     `json:"name"`
		IsActive          bool       `json:"is_active"`
		UpdatedAt         *time.Time `json:"-"`
		UpdatedAtStr      *string    `json:"updated_at" gorm:"-"`
		DefaultMarkupPct  float64    `json:"default_markup_pct"`
		ValidDays         int        `json:"valid_days"`
		RegionsCount      int        `json:"regions_count"`
		TotalItems        int        `json:"total_items"`
		FormalizedItems   int        `json:"formalized_items"`
		UnformalizedItems int        `json:"unformalized_items"`
	}
	var rows []PL
	err := s.database.GORMWith(r.Context()).Raw(
		`SELECT CAST(pl.PriceListID AS NVARCHAR(50)) AS PriceListID,
			pl.Name AS Name,
			pl.IsActive AS IsActive,
			pl.UpdatedAt AS UpdatedAt,
			CAST(ISNULL(pl.DefaultMarkupPct, 0) AS FLOAT) AS DefaultMarkupPct,
			ISNULL(pl.ValidDays, 0) AS ValidDays,
			(SELECT COUNT(*) FROM PriceListRegion plr WHERE plr.PriceListID=pl.PriceListID AND plr.IsActive=1) AS RegionsCount,
			(SELECT COUNT(*) FROM SupplierPrice sp
			 WHERE sp.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
			   AND sp.InvoiceImportID = (
				SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
				JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
				WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
				  AND ii.ImportStatus = 'COMPLETED'
				ORDER BY ii.CompletedAt DESC
			   )) AS TotalItems,
			(SELECT COUNT(*) FROM SupplierPrice sp
			 WHERE sp.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
			   AND sp.GUID_ES IS NOT NULL
			   AND sp.InvoiceImportID = (
				SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
				JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
				WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
				  AND ii.ImportStatus = 'COMPLETED'
				ORDER BY ii.CompletedAt DESC
			   )) AS FormalizedItems,
			(SELECT COUNT(*) FROM SupplierPrice sp
			 WHERE sp.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
			   AND sp.GUID_ES IS NULL
			   AND sp.InvoiceImportID = (
				SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
				JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
				WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
				  AND ii.ImportStatus = 'COMPLETED'
				ORDER BY ii.CompletedAt DESC
			   )) AS UnformalizedItems
		FROM PriceList pl
		WHERE pl.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		ORDER BY pl.Name`,
		sql.Named("sid", sid),
	).Scan(&rows).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	for i := range rows {
		if rows[i].UpdatedAt != nil {
			t := rows[i].UpdatedAt.Format("02.01.2006, 15:04:05")
			rows[i].UpdatedAtStr = &t
		}
	}
	if rows == nil {
		rows = []PL{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// --- Price List Update (inline editing) ---

func (s *Server) handleSCPriceListUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "PATCH only"})
		return
	}
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	plID := r.URL.Query().Get("id")
	if plID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id обязателен"})
		return
	}

	var body struct {
		DefaultMarkupPct *float64 `json:"default_markup_pct"`
		ValidDays        *int     `json:"valid_days"`
		IsActive         *bool    `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный JSON"})
		return
	}

	sets := []string{}
	if body.DefaultMarkupPct != nil {
		sets = append(sets, fmt.Sprintf("DefaultMarkupPct=%v", *body.DefaultMarkupPct))
	}
	if body.ValidDays != nil {
		sets = append(sets, fmt.Sprintf("ValidDays=%d", *body.ValidDays))
	}
	if body.IsActive != nil {
		v := 0
		if *body.IsActive {
			v = 1
		}
		sets = append(sets, fmt.Sprintf("IsActive=%d", v))
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Нечего обновлять"})
		return
	}

	q := fmt.Sprintf(
		"UPDATE PriceList SET %s, UpdatedAt=GETDATE() WHERE PriceListID=CAST(@plid AS UNIQUEIDENTIFIER) AND SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)",
		joinStrings(sets, ", "),
	)
	res := s.database.GORMWith(r.Context()).Exec(q,
		sql.Named("plid", plID), sql.Named("sid", sid),
	)
	if res.Error != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Прайс-лист не найден"})
		return
	}

	if body.DefaultMarkupPct != nil {
		updatePricesQ := `
			UPDATE sp
			SET sp.MarkupPct = CAST(@markup AS DECIMAL(5,2)),
				sp.UpdatedAt = GETUTCDATE()
			FROM SupplierPrice sp
			WHERE sp.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
			  AND sp.InvoiceImportID = (
				SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
				JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
				WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
				  AND ii.ImportStatus = 'COMPLETED'
				ORDER BY ii.CompletedAt DESC
			  )
		`
		result := s.database.GORMWith(r.Context()).Exec(updatePricesQ,
			sql.Named("sid", sid), sql.Named("markup", *body.DefaultMarkupPct))
		if result.Error != nil {
			s.logger.Warn("Не удалось обновить MarkupPct в SupplierPrice: %v", result.Error)
		} else {
			s.logger.Info("Обновлено %d позиций SupplierPrice.MarkupPct=%.2f%% для PriceListID=%s", result.RowsAffected, *body.DefaultMarkupPct, plID)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func joinStrings(s []string, sep string) string {
	result := ""
	for i, v := range s {
		if i > 0 {
			result += sep
		}
		result += v
	}
	return result
}

// --- Price List Region Settings ---

func (s *Server) handleSCPriceListRegions(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}
	plID := r.URL.Query().Get("price_list_id")
	if plID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "price_list_id обязателен"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleSCPLRegionsGet(w, r, sid, plID)
	case http.MethodPut:
		s.handleSCPLRegionsPut(w, r, sid, plID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "GET/PUT only"})
	}
}

func (s *Server) handleSCPLRegionsGet(w http.ResponseWriter, r *http.Request, sid, plID string) {
	type RegSetting struct {
		RegionName    string  `json:"region_name"`
		RegionID      string  `json:"region_id"`
		InWork        bool    `json:"in_work"`
		MarkupPct     float64 `json:"markup_pct"`
		BasePriceType string  `json:"base_price_type"`
		PLRID         string  `json:"-" gorm:"column:PLRID"`
	}
	var result []RegSetting
	err := s.database.GORMWith(r.Context()).Raw(
		`SELECT r.Name AS RegionName,
			CAST(r.RegionID AS NVARCHAR(50)) AS RegionID,
			ISNULL(plr.IsActive, 0) AS InWork,
			CAST(ISNULL(plr.MarkupPct, 0) AS FLOAT) AS MarkupPct,
			ISNULL(plr.BasePriceType, N'Базовая') AS BasePriceType,
			CAST(ISNULL(plr.PriceListRegionID, '00000000-0000-0000-0000-000000000000') AS NVARCHAR(50)) AS PLRID
		FROM SupplierRegion sr
		INNER JOIN Region r ON r.RegionID = sr.RegionID AND r.IsActive = 1
		LEFT JOIN PriceListRegion plr ON plr.RegionID = sr.RegionID
			AND plr.PriceListID = CAST(@plid AS UNIQUEIDENTIFIER)
		WHERE sr.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER) AND sr.IsActive = 1
		ORDER BY r.Name`,
		sql.Named("sid", sid), sql.Named("plid", plID),
	).Scan(&result).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if result == nil {
		result = []RegSetting{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSCPLRegionsPut(w http.ResponseWriter, r *http.Request, sid, plID string) {
	var ownership int
	err := s.database.GORMWith(r.Context()).Raw(
		`SELECT COUNT(*) FROM PriceList WHERE PriceListID=CAST(@plid AS UNIQUEIDENTIFIER) AND SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
		sql.Named("plid", plID), sql.Named("sid", sid),
	).Row().Scan(&ownership)
	if err != nil || ownership == 0 {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "Прайс-лист не принадлежит поставщику"})
		return
	}

	type RegInput struct {
		RegionID      string  `json:"region_id"`
		InWork        bool    `json:"in_work"`
		MarkupPct     float64 `json:"markup_pct"`
		BasePriceType string  `json:"base_price_type"`
	}
	var items []RegInput
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный JSON"})
		return
	}

	for _, it := range items {
		err := s.database.GORMWith(r.Context()).Exec(
			`MERGE PriceListRegion AS target
			 USING (SELECT CAST(@plid AS UNIQUEIDENTIFIER) AS PriceListID, CAST(@rid AS UNIQUEIDENTIFIER) AS RegionID) AS src
			 ON target.PriceListID = src.PriceListID AND target.RegionID = src.RegionID
			 WHEN MATCHED THEN
				UPDATE SET IsActive=@inwork, MarkupPct=@markup, BasePriceType=@bpt
			 WHEN NOT MATCHED THEN
				INSERT (PriceListRegionID, PriceListID, RegionID, IsActive, MarkupPct, BasePriceType, CreatedAt)
				VALUES (NEWID(), src.PriceListID, src.RegionID, @inwork, @markup, @bpt, GETUTCDATE());`,
			sql.Named("plid", plID),
			sql.Named("rid", it.RegionID),
			sql.Named("inwork", it.InWork),
			sql.Named("markup", it.MarkupPct),
			sql.Named("bpt", it.BasePriceType),
		).Error
		if err != nil {
			s.logger.Error("Ошибка MERGE PriceListRegion: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Price Positions ---

func (s *Server) handleSCPricePositions(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	priceListID := r.URL.Query().Get("price_list_id")
	if priceListID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "price_list_id обязателен"})
		return
	}

	filter := r.URL.Query().Get("filter")
	search := r.URL.Query().Get("search")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 50
	}

	baseWhere := `sp.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		AND sp.InvoiceImportID = (
			SELECT TOP 1 ii.InvoiceImportID FROM InvoiceImport ii
			JOIN SupplierPrice sp2 ON sp2.InvoiceImportID = ii.InvoiceImportID
			WHERE sp2.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
			  AND ii.ImportStatus = 'COMPLETED'
			ORDER BY ii.CompletedAt DESC
		)`
	where := baseWhere
	switch filter {
	case "formalized":
		where += ` AND sp.GUID_ES IS NOT NULL`
	case "unformalized":
		where += ` AND sp.GUID_ES IS NULL`
	}
	if search != "" {
		where += ` AND (sp.ItemName LIKE '%'+@search+'%' OR sp.Manufacturer LIKE '%'+@search+'%')`
	}

	var total int
	_ = s.database.GORMWith(r.Context()).Raw(
		fmt.Sprintf(`SELECT COUNT(*) FROM SupplierPrice sp WHERE %s`, where),
		sql.Named("sid", sid), sql.Named("plid", priceListID), sql.Named("search", search),
	).Row().Scan(&total)

	offset := (page - 1) * perPage
	q := fmt.Sprintf(`SELECT CAST(sp.SupplierPriceID AS NVARCHAR(50)) AS ID,
			ISNULL(sp.ItemName,'') AS ItemName,
			ISNULL(sp.Manufacturer,'') AS Manufacturer,
			CAST(sp.GUID_ES AS NVARCHAR(50)) AS GuidES,
			e.NAME AS ProductName,
			sp.Price AS Price,
			sp.FinalPrice AS FinalPrice,
			ISNULL(sp.Quantity,0) AS Quantity
		FROM SupplierPrice sp
		LEFT JOIN es_ef2 e ON sp.GUID_ES = e.GUID_ES
		WHERE %s
		ORDER BY sp.ItemName
		OFFSET @offset ROWS FETCH NEXT @limit ROWS ONLY`, where)

	type Pos struct {
		ID           string  `json:"id"`
		ItemName     string  `json:"item_name"`
		Manufacturer string  `json:"manufacturer"`
		GuidES       *string `json:"guid_es"`
		ProductName  *string `json:"product_name"`
		Price        float64 `json:"price"`
		FinalPrice   float64 `json:"final_price"`
		Quantity     float64 `json:"quantity"`
	}
	var items []Pos
	err := s.database.GORMWith(r.Context()).Raw(q,
		sql.Named("sid", sid), sql.Named("plid", priceListID),
		sql.Named("search", search),
		sql.Named("offset", offset), sql.Named("limit", perPage),
	).Scan(&items).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if items == nil {
		items = []Pos{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

// --- Price Upload Log ---

func (s *Server) handleSCPriceUploadLog(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	from := r.URL.Query().Get("from")
	if from == "" {
		from = r.URL.Query().Get("date_from")
	}
	to := r.URL.Query().Get("to")
	if to == "" {
		to = r.URL.Query().Get("date_to")
	}
	plID := r.URL.Query().Get("price_list_id")

	if from == "" {
		from = time.Now().AddDate(0, -1, 0).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}

	where := `pl.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		AND ii.CompletedAt >= @from AND ii.CompletedAt < DATEADD(day,1,CAST(@to AS DATE))`
	if plID != "" {
		where += ` AND pl.PriceListID=CAST(@plid AS UNIQUEIDENTIFIER)`
	}

	q := fmt.Sprintf(`SELECT pl.Name AS PriceName,
			ii.CompletedAt AS DownloadTime,
			ii.ImportStatus AS Status,
			ISNULL(ii.RecordsProcessed, 0) AS RecordsProcessed,
			(SELECT COUNT(*) FROM SupplierPrice sp WHERE sp.InvoiceImportID=ii.InvoiceImportID AND sp.GUID_ES IS NOT NULL) AS FormalizedCount,
			(SELECT COUNT(*) FROM SupplierPrice sp WHERE sp.InvoiceImportID=ii.InvoiceImportID AND sp.GUID_ES IS NULL) AS UnformalizedCount
		FROM InvoiceImport ii
		JOIN PriceList pl ON pl.ImportPointID = ii.ImportPointID
		WHERE %s
		ORDER BY ii.CompletedAt DESC`, where)

	type LogRow struct {
		PriceName         string     `json:"price_name"`
		DownloadTime      *time.Time `json:"-"`
		DownloadTimeStr   *string    `json:"download_time" gorm:"-"`
		Status            string     `json:"status"`
		RecordsProcessed  int        `json:"records_processed"`
		FormalizedCount   int        `json:"formalized_count"`
		UnformalizedCount int        `json:"unformalized_count"`
	}
	var result []LogRow
	err := s.database.GORMWith(r.Context()).Raw(q,
		sql.Named("sid", sid), sql.Named("from", from), sql.Named("to", to), sql.Named("plid", plID),
	).Scan(&result).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	for i := range result {
		if result[i].DownloadTime != nil {
			t := result[i].DownloadTime.Format(time.RFC3339)
			result[i].DownloadTimeStr = &t
		}
	}
	if result == nil {
		result = []LogRow{}
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Regions ---

func (s *Server) handleSCRegions(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	if r.Method == http.MethodPatch {
		srID := r.URL.Query().Get("id")
		if srID == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id обязателен"})
			return
		}
		var body struct {
			IsActive *bool `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IsActive == nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "is_active обязателен"})
			return
		}
		res := s.database.GORMWith(r.Context()).Exec(
			`UPDATE SupplierRegion SET IsActive=@active
			 WHERE SupplierRegionID=CAST(@id AS UNIQUEIDENTIFIER)
			   AND SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
			sql.Named("active", *body.IsActive),
			sql.Named("id", srID),
			sql.Named("sid", sid),
		)
		if res.Error != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: res.Error.Error()})
			return
		}
		if res.RowsAffected == 0 {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "Регион не найден"})
			return
		}
		// Каскад: синхронизировать PriceListRegion во всех прайсах поставщика
		var regionID string
		_ = s.database.GORMWith(r.Context()).Raw(
			`SELECT CAST(RegionID AS NVARCHAR(50)) FROM SupplierRegion
			 WHERE SupplierRegionID=CAST(@id AS UNIQUEIDENTIFIER)`,
			sql.Named("id", srID),
		).Row().Scan(&regionID)
		if regionID != "" {
			_ = s.database.GORMWith(r.Context()).Exec(
				`UPDATE plr SET plr.IsActive=@active
				 FROM PriceListRegion plr
				 JOIN PriceList pl ON pl.PriceListID = plr.PriceListID
				 WHERE pl.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
				   AND plr.RegionID=CAST(@rid AS UNIQUEIDENTIFIER)`,
				sql.Named("active", *body.IsActive),
				sql.Named("sid", sid),
				sql.Named("rid", regionID),
			).Error
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	type Reg struct {
		SupplierRegionID string `json:"supplier_region_id"`
		RegionID         string `json:"region_id"`
		Name             string `json:"name"`
		IsActive         bool   `json:"is_active"`
	}
	var result []Reg
	err := s.database.GORMWith(r.Context()).Raw(
		`SELECT CAST(sr.SupplierRegionID AS NVARCHAR(50)) AS SupplierRegionID,
			CAST(r.RegionID AS NVARCHAR(50)) AS RegionID,
			r.Name AS Name,
			sr.IsActive AS IsActive
		 FROM SupplierRegion sr
		 JOIN Region r ON sr.RegionID = r.RegionID
		 WHERE sr.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER) ORDER BY r.Name`,
		sql.Named("sid", sid),
	).Scan(&result).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if result == nil {
		result = []Reg{}
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Clients (Buyers) ---

func (s *Server) handleSCClients(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPatch {
		s.handleSCClientUpdate(w, r, sid)
		return
	}

	regionID := r.URL.Query().Get("region_id")

	q := `SELECT DISTINCT
			CAST(b.BuyerID AS NVARCHAR(50)) AS BuyerID,
			b.Name AS Name,
			ISNULL(b.INN,'') AS INN,
			ISNULL(r.Name,'') AS Region,
			ISNULL(bl.Address,'') AS Address,
			ISNULL(cfg.IsActive, 1) AS IsActive,
			ISNULL(cfg.PriceColumn, N'Базовая') AS PriceColumn,
			ISNULL(cfg.MarkupPct, 0) AS MarkupPct,
			ISNULL(cfg.ClientCode, '') AS ClientCode,
			ISNULL(cfg.PaymentCode, '') AS PaymentCode,
			ISNULL(cfg.DeliveryCode, '') AS DeliveryCode,
			ISNULL(cfg.ControlMinOrder, 0) AS ControlMinOrder,
			ISNULL(cfg.MinOrderAmount, 0) AS MinOrderAmount,
			ISNULL(cfg.MinReorderAmount, 0) AS MinReorderAmount,
			cfg.UpdatedAt AS UpdatedAt
		FROM Buyer b
		INNER JOIN SupplierRegion sr
			ON sr.RegionID = b.RegionID
			AND sr.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
			AND sr.IsActive = 1
		LEFT JOIN Region r ON r.RegionID = b.RegionID
		LEFT JOIN BuyerLocation bl ON bl.BuyerID = b.BuyerID AND bl.IsDefault = 1
		LEFT JOIN SupplierBuyerConfig cfg ON cfg.BuyerID = b.BuyerID AND cfg.SupplierID = CAST(@sid AS UNIQUEIDENTIFIER)
		WHERE b.IsActive = 1`
	if regionID != "" {
		q += ` AND b.RegionID = CAST(@rid AS UNIQUEIDENTIFIER)`
	}
	q += ` ORDER BY b.Name`

	type Client struct {
		BuyerID          string     `json:"buyer_id"`
		Name             string     `json:"name"`
		INN              string     `json:"inn"`
		Region           string     `json:"region"`
		Address          string     `json:"address"`
		IsActive         bool       `json:"is_active"`
		PriceColumn      string     `json:"price_column"`
		MarkupPct        float64    `json:"markup_pct"`
		ClientCode       string     `json:"client_code"`
		PaymentCode      string     `json:"payment_code"`
		DeliveryCode     string     `json:"delivery_code"`
		ControlMinOrder  bool       `json:"control_min_order"`
		MinOrderAmount   float64    `json:"min_order_amount"`
		MinReorderAmount float64    `json:"min_reorder_amount"`
		UpdatedAt        *time.Time `json:"-"`
		UpdatedAtStr     *string    `json:"updated_at" gorm:"-"`
	}
	var result []Client
	err := s.database.GORMWith(r.Context()).Raw(q,
		sql.Named("sid", sid), sql.Named("rid", regionID),
	).Scan(&result).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	for i := range result {
		if result[i].UpdatedAt != nil {
			s := result[i].UpdatedAt.Format("02.01.2006")
			result[i].UpdatedAtStr = &s
		}
	}
	if result == nil {
		result = []Client{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSCClientUpdate(w http.ResponseWriter, r *http.Request, sid string) {
	buyerID := r.URL.Query().Get("buyer_id")
	if buyerID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "buyer_id обязателен"})
		return
	}
	var body struct {
		IsActive         *bool    `json:"is_active"`
		PriceColumn      *string  `json:"price_column"`
		MarkupPct        *float64 `json:"markup_pct"`
		ClientCode       *string  `json:"client_code"`
		PaymentCode      *string  `json:"payment_code"`
		DeliveryCode     *string  `json:"delivery_code"`
		ControlMinOrder  *bool    `json:"control_min_order"`
		MinOrderAmount   *float64 `json:"min_order_amount"`
		MinReorderAmount *float64 `json:"min_reorder_amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный JSON"})
		return
	}

	// Upsert: вставить если нет, обновить если есть
	_ = s.database.GORMWith(r.Context()).Exec(
		`IF NOT EXISTS (SELECT 1 FROM SupplierBuyerConfig WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER) AND BuyerID=CAST(@bid AS UNIQUEIDENTIFIER))
		 INSERT INTO SupplierBuyerConfig (SupplierID, BuyerID) VALUES (CAST(@sid AS UNIQUEIDENTIFIER), CAST(@bid AS UNIQUEIDENTIFIER))`,
		sql.Named("sid", sid), sql.Named("bid", buyerID),
	).Error

	sets := []string{"UpdatedAt=GETUTCDATE()"}
	if body.IsActive != nil {
		sets = append(sets, fmt.Sprintf("IsActive=%d", boolToInt(*body.IsActive)))
	}
	if body.PriceColumn != nil {
		sets = append(sets, "PriceColumn=@pc")
	}
	if body.MarkupPct != nil {
		sets = append(sets, "MarkupPct=@mp")
	}
	if body.ClientCode != nil {
		sets = append(sets, "ClientCode=@cc")
	}
	if body.PaymentCode != nil {
		sets = append(sets, "PaymentCode=@payc")
	}
	if body.DeliveryCode != nil {
		sets = append(sets, "DeliveryCode=@dc")
	}
	if body.ControlMinOrder != nil {
		sets = append(sets, fmt.Sprintf("ControlMinOrder=%d", boolToInt(*body.ControlMinOrder)))
	}
	if body.MinOrderAmount != nil {
		sets = append(sets, "MinOrderAmount=@moa")
	}
	if body.MinReorderAmount != nil {
		sets = append(sets, "MinReorderAmount=@mra")
	}

	q := fmt.Sprintf(`UPDATE SupplierBuyerConfig SET %s WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER) AND BuyerID=CAST(@bid AS UNIQUEIDENTIFIER)`,
		joinStrings(sets, ", "))

	err := s.database.GORMWith(r.Context()).Exec(q,
		sql.Named("sid", sid), sql.Named("bid", buyerID),
		sql.Named("pc", ptrStr(body.PriceColumn)),
		sql.Named("mp", ptrFloat(body.MarkupPct)),
		sql.Named("cc", ptrStr(body.ClientCode)),
		sql.Named("payc", ptrStr(body.PaymentCode)),
		sql.Named("dc", ptrStr(body.DeliveryCode)),
		sql.Named("moa", ptrFloat(body.MinOrderAmount)),
		sql.Named("mra", ptrFloat(body.MinReorderAmount)),
	).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func ptrStr(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}
func ptrFloat(p *float64) float64 {
	if p != nil {
		return *p
	}
	return 0
}

// --- Orders ---

func (s *Server) handleSCOrders(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	// Страница шлёт date_from/date_to (поддержим и старые from/to). Пусто → последний месяц.
	// Даты по умолчанию в UTC — CreatedAt хранится в UTC, иначе на сервере в другом часовом
	// поясе свежие заказы «сегодня» выпадали из диапазона.
	from := r.URL.Query().Get("date_from")
	if from == "" {
		from = r.URL.Query().Get("from")
	}
	to := r.URL.Query().Get("date_to")
	if to == "" {
		to = r.URL.Query().Get("to")
	}
	search := r.URL.Query().Get("search")
	if from == "" {
		from = time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}

	where := `oi.SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)
		AND o.CreatedAt >= @from AND o.CreatedAt < DATEADD(day,1,CAST(@to AS DATE))`
	if search != "" {
		where += ` AND (b.Name LIKE '%'+@search+'%' OR CAST(o.OrderID AS NVARCHAR(50)) LIKE '%'+@search+'%')`
	}

	// Форма ответа и имена полей согласованы со страницей orders.html:
	// {items:[{order_id, order_date, buyer_name, delivery_address, total_items, total_amount, status_name}], total}.
	q := fmt.Sprintf(`SELECT CAST(o.OrderID AS NVARCHAR(50)) AS OrderID,
			b.Name AS BuyerName,
			ISNULL(bl.Address, '') AS DeliveryAddress,
			o.CreatedAt AS OrderDate,
			os.Name AS StatusName,
			SUM(oi.Qty * oi.UnitPrice) AS TotalAmount,
			COUNT(oi.OrderLineID) AS TotalItems
		FROM [Order] o
		JOIN BuyerUser bu ON o.BuyerUserID = bu.BuyerUserID
		JOIN Buyer b ON bu.BuyerID = b.BuyerID
		JOIN OrderItem oi ON oi.OrderID = o.OrderID
		LEFT JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		LEFT JOIN BuyerLocation bl ON bl.BuyerLocationID = o.BuyerLocationID
		WHERE %s
		GROUP BY o.OrderID, b.Name, bl.Address, o.CreatedAt, os.Name
		ORDER BY o.CreatedAt DESC`, where)

	type OrderRow struct {
		OrderID         string    `json:"order_id"`
		BuyerName       string    `json:"buyer_name"`
		DeliveryAddress string    `json:"delivery_address"`
		OrderDate       time.Time `json:"order_date"`
		StatusName      *string   `json:"status_name"`
		TotalAmount     float64   `json:"total_amount"`
		TotalItems      int       `json:"total_items"`
	}
	var result []OrderRow
	err := s.database.GORMWith(r.Context()).Raw(q,
		sql.Named("sid", sid), sql.Named("from", from), sql.Named("to", to), sql.Named("search", search),
	).Scan(&result).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if result == nil {
		result = []OrderRow{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": result,
		"total": len(result),
	})
}

// --- Change Password ---

func (s *Server) handleSCChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "PUT only"})
		return
	}
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный JSON"})
		return
	}
	if body.NewPassword == "" || len(body.NewPassword) < 4 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Пароль должен быть не менее 4 символов"})
		return
	}

	var currentPwd string
	err := s.database.GORMWith(r.Context()).Raw(
		`SELECT Password FROM Supplier WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
		sql.Named("sid", sid),
	).Row().Scan(&currentPwd)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка проверки пароля"})
		return
	}
	if !verifyPassword(currentPwd, body.OldPassword) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный текущий пароль"})
		return
	}

	// Сохраняем новый пароль как bcrypt-хэш (см. MD/11 — единый паттерн с supplier-login).
	newHash, err := hashPassword(body.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка обработки пароля"})
		return
	}
	err = s.database.GORMWith(r.Context()).Exec(
		`UPDATE Supplier SET Password=@pwd WHERE SupplierID=CAST(@sid AS UNIQUEIDENTIFIER)`,
		sql.Named("pwd", newHash), sql.Named("sid", sid),
	).Error
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Setup Routes ---

func (s *Server) setupSupplierRoutes(mux *http.ServeMux) {
	scRoute := func(path string, handler http.HandlerFunc) {
		mux.HandleFunc(path, s.corsMiddleware(s.loggingMiddleware(
			s.authService.JWTMiddleware(
				s.authService.RequireRole("supplier")(handler),
			).ServeHTTP,
		)))
	}

	scRoute("/api/sc/dashboard", s.handleSCDashboard)
	scRoute("/api/sc/profile", s.handleSCProfile)
	scRoute("/api/sc/price-lists", s.handleSCPriceLists)
	scRoute("/api/sc/price-list-update", s.handleSCPriceListUpdate)
	scRoute("/api/sc/price-list-regions", s.handleSCPriceListRegions)
	scRoute("/api/sc/price-positions", s.handleSCPricePositions)
	scRoute("/api/sc/price-upload-log", s.handleSCPriceUploadLog)
	scRoute("/api/sc/regions", s.handleSCRegions)
	scRoute("/api/sc/clients", s.handleSCClients)
	scRoute("/api/sc/orders", s.handleSCOrders)
	scRoute("/api/sc/orders/export", s.handleSCOrdersExport)
	scRoute("/api/sc/export-config", s.handleSCExportConfig)
	scRoute("/api/sc/order-delivery", s.handleSCOrderDelivery)
	scRoute("/api/sc/change-password", s.handleSCChangePassword)
}
