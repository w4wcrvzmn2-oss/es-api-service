package httpserver

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// handleManagerDashboard — сводка для кабинета менеджера.
func (s *Server) handleManagerDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	g := s.database.GORMWith(ctx)
	scalar := func(q string, args ...interface{}) int64 {
		var n int64
		_ = g.Raw(q, args...).Row().Scan(&n)
		return n
	}

	draftOrders := scalar(`
		SELECT COUNT(*) FROM "Order" o
		INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		WHERE os.Name = 'Draft'`)
	placedOrders := scalar(`
		SELECT COUNT(*) FROM "Order" o
		INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		WHERE os.Name = 'Placed'`)
	failedImports := scalar(`SELECT COUNT(*) FROM InvoiceImport WHERE ImportStatus = 'FAILED'`)
	processingImports := scalar(`SELECT COUNT(*) FROM InvoiceImport WHERE ImportStatus = 'PROCESSING'`)
	activePriceLists := scalar(`SELECT COUNT(*) FROM PriceList WHERE IsActive = TRUE`)

	var matched, unmatched int64
	_ = g.Raw(`
		SELECT
			COUNT(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS TEXT) <> '' THEN 1 END),
			COUNT(CASE WHEN sp.GUID_ES IS NULL OR CAST(sp.GUID_ES AS TEXT) = '' THEN 1 END)
		FROM SupplierPrice sp
		INNER JOIN PriceList pl ON pl.PriceListID = sp.PriceListID AND pl.IsActive = TRUE
		WHERE sp.IsActive = TRUE
	`).Row().Scan(&matched, &unmatched)

	type recentOrder struct {
		OrderID         string     `json:"order_id"`
		BuyerName       *string    `json:"buyer_name,omitempty"`
		StatusName      *string    `json:"status_name,omitempty"`
		TotalAmount     *float64   `json:"total_amount,omitempty"`
		CreatedAt       time.Time  `json:"created_at"`
		LocationAddress *string    `json:"location_address,omitempty"`
	}
	var recent []recentOrder
	_ = g.Raw(`
		SELECT CAST(o.OrderID AS TEXT) AS OrderID,
			b.Name AS BuyerName,
			os.Name AS StatusName,
			o.TotalAmount,
			o.CreatedAt,
			bl.Address AS LocationAddress
		FROM "Order" o
		INNER JOIN BuyerUser bu ON o.BuyerUserID = bu.BuyerUserID
		INNER JOIN Buyer b ON bu.BuyerID = b.BuyerID
		INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		LEFT JOIN BuyerLocation bl ON o.BuyerLocationID = bl.BuyerLocationID
		ORDER BY o.CreatedAt DESC
		LIMIT 10
	`).Scan(&recent).Error
	if recent == nil {
		recent = []recentOrder{}
	}

	type recentImport struct {
		InvoiceImportID string  `json:"invoice_import_id"`
		FileName        string  `json:"file_name"`
		ImportStatus    string  `json:"import_status"`
		SupplierName    *string `json:"supplier_name,omitempty"`
		ErrorMessage    *string `json:"error_message,omitempty"`
		CreatedAt       string  `json:"created_at"`
	}
	var failedList []recentImport
	rows, err := s.database.QueryContext(ctx, `
		SELECT CAST(ii.InvoiceImportID AS TEXT),
			COALESCE(ii.FileName, ''),
			ii.ImportStatus,
			s.Name,
			ii.ErrorMessage,
			ii.CreatedAt
		FROM InvoiceImport ii
		LEFT JOIN ImportPoint ip ON ii.ImportPointID = ip.ImportPointID
		LEFT JOIN Supplier s ON ip.SupplierID = s.SupplierID
		WHERE ii.ImportStatus = 'FAILED'
		ORDER BY ii.CreatedAt DESC
		LIMIT 10
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ri recentImport
			var supplier sql.NullString
			var errMsg sql.NullString
			var created sql.NullTime
			if scanErr := rows.Scan(&ri.InvoiceImportID, &ri.FileName, &ri.ImportStatus, &supplier, &errMsg, &created); scanErr != nil {
				continue
			}
			if supplier.Valid {
				ri.SupplierName = &supplier.String
			}
			if errMsg.Valid {
				ri.ErrorMessage = &errMsg.String
			}
			if created.Valid {
				ri.CreatedAt = created.Time.Format("2006-01-02 15:04:05")
			}
			failedList = append(failedList, ri)
		}
	}
	if failedList == nil {
		failedList = []recentImport{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"draft_orders":        draftOrders,
		"placed_orders":       placedOrders,
		"failed_imports":      failedImports,
		"processing_imports":  processingImports,
		"active_price_lists":  activePriceLists,
		"matched_prices":      matched,
		"unmatched_prices":    unmatched,
		"recent_orders":       recent,
		"recent_failed_imports": failedList,
	})
}

// handleManagerDrugOffers — поиск препаратов + цены поставщиков.
func (s *Server) handleManagerDrugOffers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	guidFilter := strings.TrimSpace(r.URL.Query().Get("guid_es"))
	if q == "" && guidFilter == "" {
		s.writeError(w, http.StatusBadRequest, "Укажите q или guid_es")
		return
	}

	drugLimit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			drugLimit = n
		}
	}

	type drugRow struct {
		GUID_ES       string   `json:"guid_es"`
		Name          string   `json:"name"`
		INN           *string  `json:"inn,omitempty"`
		CureForm      *string  `json:"cure_form,omitempty"`
		Barcode       *string  `json:"barcode,omitempty"`
		TradeName     *string  `json:"trade_name,omitempty"`
		ProducerName  *string  `json:"producer_name,omitempty"`
		RegistryPrice *float64 `json:"registry_price,omitempty"`
	}

	var drugs []drugRow
	if guidFilter != "" {
		if _, err := uuid.Parse(guidFilter); err != nil {
			s.writeError(w, http.StatusBadRequest, "Недопустимый guid_es")
			return
		}
		var d drugRow
		var inn, cure, barcode, trade, producer sql.NullString
		var reg sql.NullFloat64
		err := s.database.QueryRowContext(ctx, `
			SELECT CAST(ef2.GUID_ES AS TEXT), ef2.NAME, ef2.INN_NAME_RUS, ef2.CUREFORM_NAME,
				ef2.BARCODE, ef2.TRN_NAME_RUS, ep.PRODUCER_NAME, ef2.REESTR_PRICE
			FROM es_ef2 ef2
			LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
			WHERE ef2.GUID_ES = CAST(@g AS UUID) AND ef2.is_active = 1 AND ef2.DELETED IS NULL
		`, sql.Named("g", guidFilter)).Scan(
			&d.GUID_ES, &d.Name, &inn, &cure, &barcode, &trade, &producer, &reg,
		)
		if err == sql.ErrNoRows {
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"query": q, "drugs": []drugRow{}, "total": 0,
			})
			return
		}
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка поиска: %v", err))
			return
		}
		if inn.Valid {
			d.INN = &inn.String
		}
		if cure.Valid {
			d.CureForm = &cure.String
		}
		if barcode.Valid {
			d.Barcode = &barcode.String
		}
		if trade.Valid {
			d.TradeName = &trade.String
		}
		if producer.Valid {
			d.ProducerName = &producer.String
		}
		if reg.Valid {
			d.RegistryPrice = &reg.Float64
		}
		drugs = append(drugs, d)
	} else {
		words := strings.Fields(q)
		var whereParts []string
		var args []interface{}
		for i, word := range words {
			pat := "%" + strings.ReplaceAll(word, "%", "") + "%"
			p := fmt.Sprintf("p%d", i)
			whereParts = append(whereParts, fmt.Sprintf(`(
				ef2.NAME LIKE @%s OR ef2.TRN_NAME_RUS LIKE @%s
				OR ef2.INN_NAME_RUS LIKE @%s OR ef2.BARCODE LIKE @%s
				OR CAST(ef2.KOD_ES AS TEXT) LIKE @%s
			)`, p, p, p, p, p))
			args = append(args, sql.Named(p, pat))
		}
		searchQ := fmt.Sprintf(`
			SELECT CAST(ef2.GUID_ES AS TEXT), ef2.NAME, ef2.INN_NAME_RUS, ef2.CUREFORM_NAME,
				ef2.BARCODE, ef2.TRN_NAME_RUS, ep.PRODUCER_NAME, ef2.REESTR_PRICE
			FROM es_ef2 ef2
			LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
			WHERE %s AND ef2.is_active = 1 AND ef2.DELETED IS NULL
			ORDER BY ef2.NAME
			LIMIT %d
		`, strings.Join(whereParts, " AND "), drugLimit)
		rows, err := s.database.QueryContext(ctx, searchQ, args...)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка поиска: %v", err))
			return
		}
		defer rows.Close()
		for rows.Next() {
			var d drugRow
			var inn, cure, barcode, trade, producer sql.NullString
			var reg sql.NullFloat64
			if err := rows.Scan(&d.GUID_ES, &d.Name, &inn, &cure, &barcode, &trade, &producer, &reg); err != nil {
				continue
			}
			if inn.Valid {
				d.INN = &inn.String
			}
			if cure.Valid {
				d.CureForm = &cure.String
			}
			if barcode.Valid {
				d.Barcode = &barcode.String
			}
			if trade.Valid {
				d.TradeName = &trade.String
			}
			if producer.Valid {
				d.ProducerName = &producer.String
			}
			if reg.Valid {
				d.RegistryPrice = &reg.Float64
			}
			drugs = append(drugs, d)
		}
	}
	if drugs == nil {
		drugs = []drugRow{}
	}

	guids := make([]string, 0, len(drugs))
	for _, d := range drugs {
		guids = append(guids, d.GUID_ES)
	}

	type offerRow struct {
		GUID_ES          string   `json:"guid_es"`
		SupplierPriceID  string   `json:"supplier_price_id"`
		SupplierID       string   `json:"supplier_id"`
		SupplierName     string   `json:"supplier_name"`
		Price            float64  `json:"price"`
		FinalPrice       float64  `json:"final_price"`
		MarkupPct        float64  `json:"markup_pct"`
		Quantity         *float64 `json:"quantity,omitempty"`
		RegionID         *string  `json:"region_id,omitempty"`
		RegionName       *string  `json:"region_name,omitempty"`
		InvoiceDate      *string  `json:"invoice_date,omitempty"`
		ItemName         *string  `json:"item_name,omitempty"`
		ItemCode         *string  `json:"item_code,omitempty"`
	}

	offersByGUID := map[string][]offerRow{}
	if len(guids) > 0 {
		placeholders := make([]string, len(guids))
		args := make([]interface{}, 0, len(guids))
		for i, g := range guids {
			name := fmt.Sprintf("g%d", i)
			placeholders[i] = fmt.Sprintf("CAST(@%s AS UUID)", name)
			args = append(args, sql.Named(name, g))
		}
		offerQ := fmt.Sprintf(`
			SELECT CAST(sp.GUID_ES AS TEXT),
				CAST(sp.SupplierPriceID AS TEXT),
				CAST(sp.SupplierID AS TEXT),
				s.Name,
				sp.Price,
				sp.Price * (1 + COALESCE(sp.MarkupPct, 0) / 100.0),
				COALESCE(sp.MarkupPct, 0),
				sp.Quantity,
				CAST(sp.RegionID AS TEXT),
				r.Name,
				sp.InvoiceDate,
				sp.ItemName,
				sp.ItemCode
			FROM SupplierPrice sp
			INNER JOIN Supplier s ON sp.SupplierID = s.SupplierID
			LEFT JOIN Region r ON sp.RegionID = r.RegionID
			WHERE sp.IsActive = TRUE
			  AND sp.GUID_ES IN (%s)
			ORDER BY sp.Price ASC, s.Name
			LIMIT 2000
		`, strings.Join(placeholders, ","))
		orows, err := s.database.QueryContext(ctx, offerQ, args...)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка загрузки офферов: %v", err)
			}
		} else {
			defer orows.Close()
			for orows.Next() {
				var o offerRow
				var qty sql.NullFloat64
				var regionID, regionName, itemName, itemCode sql.NullString
				var invoiceDate sql.NullTime
				if err := orows.Scan(
					&o.GUID_ES, &o.SupplierPriceID, &o.SupplierID, &o.SupplierName,
					&o.Price, &o.FinalPrice, &o.MarkupPct,
					&qty, &regionID, &regionName, &invoiceDate, &itemName, &itemCode,
				); err != nil {
					continue
				}
				if qty.Valid {
					o.Quantity = &qty.Float64
				}
				if regionID.Valid {
					o.RegionID = &regionID.String
				}
				if regionName.Valid {
					o.RegionName = &regionName.String
				}
				if invoiceDate.Valid {
					s := invoiceDate.Time.Format("2006-01-02")
					o.InvoiceDate = &s
				}
				if itemName.Valid {
					o.ItemName = &itemName.String
				}
				if itemCode.Valid {
					o.ItemCode = &itemCode.String
				}
				offersByGUID[o.GUID_ES] = append(offersByGUID[o.GUID_ES], o)
			}
		}
	}

	type drugWithOffers struct {
		drugRow
		Offers []offerRow `json:"offers"`
	}
	out := make([]drugWithOffers, 0, len(drugs))
	for _, d := range drugs {
		offers := offersByGUID[d.GUID_ES]
		if offers == nil {
			offers = []offerRow{}
		}
		out = append(out, drugWithOffers{drugRow: d, Offers: offers})
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"query": q,
		"total": len(out),
		"drugs": out,
	})
}
