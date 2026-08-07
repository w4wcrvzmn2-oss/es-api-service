package httpserver

import (
	"context"
	"net/http"
	"time"

	"es_api_service/internal/db"
)

// buyerReportLine — одна позиция заказа покупателя для отчётов.
type buyerReportLine struct {
	OrderID    string    `json:"order_id" gorm:"column:order_id"`
	GlobalSign string    `json:"global_sign" gorm:"column:global_sign"`
	OrderDate  time.Time `json:"order_date" gorm:"column:order_date"`
	Status     string    `json:"status" gorm:"column:status"`
	Supplier   string    `json:"supplier" gorm:"column:supplier"`
	Location   string    `json:"location" gorm:"column:location"`
	ItemName   string    `json:"item_name" gorm:"column:item_name"`
	ItemCode   string    `json:"item_code" gorm:"column:item_code"`
	Qty        float64   `json:"qty" gorm:"column:qty"`
	UnitPrice  float64   `json:"unit_price" gorm:"column:unit_price"`
	Sum        float64   `json:"sum" gorm:"column:sum"`
}

// handleBuyerReport — GET /api/buyer/report?date_from=YYYY-MM-DD&date_to=YYYY-MM-DD
// Возвращает позиции заказов ТОЛЬКО этого покупателя (скоуп по BuyerUserID из JWT).
// Десктоп агрегирует их клиентски в отчёты: заказы / поставщики / товары / аптеки.
func (s *Server) handleBuyerReport(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleBuyerReport: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	from := r.URL.Query().Get("date_from")
	to := r.URL.Query().Get("date_to")
	if from == "" {
		from = time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}

	var rows []buyerReportLine
	err := s.database.GORMWith(ctx).Raw(`
		SELECT
			CAST(o."OrderID" AS TEXT) AS order_id,
			COALESCE(o."GlobalSign", '') AS global_sign,
			o."CreatedAt" AS order_date,
			os."Name" AS status,
			COALESCE(sup."Name", '') AS supplier,
			COALESCE(bl."Address", '') AS location,
			COALESCE(NULLIF(TRIM(oi."ItemName"), ''), sp."ItemName", '') AS item_name,
			COALESCE(NULLIF(TRIM(oi."ItemCode"), ''), sp."ItemCode", '') AS item_code,
			oi."Qty" AS qty,
			oi."UnitPrice" AS unit_price,
			(oi."Qty" * oi."UnitPrice") AS sum
		FROM "OrderItem" oi
		INNER JOIN "Order" o ON o."OrderID" = oi."OrderID"
		INNER JOIN "OrderStatus" os ON os."OrderStatusID" = o."OrderStatusID"
		LEFT JOIN "Supplier" sup ON sup."SupplierID" = oi."SupplierID"
		LEFT JOIN "BuyerLocation" bl ON bl."BuyerLocationID" = o."BuyerLocationID"
		LEFT JOIN "SupplierPrice" sp ON sp."SupplierPriceID" = oi."SupplierPriceID"
		WHERE o."BuyerUserID" = CAST(? AS UUID)
		  AND o."CreatedAt" >= CAST(? AS date)
		  AND o."CreatedAt" < (CAST(? AS date) + INTERVAL '1 day')
		ORDER BY o."CreatedAt" DESC, o."OrderID"
	`, db.UUIDParam(buyerUserID), from, to).Scan(&rows).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка отчёта покупателя: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка формирования отчёта: "+err.Error())
		return
	}

	orderSet := make(map[string]struct{}, len(rows))
	var totalSum float64
	for _, l := range rows {
		orderSet[l.OrderID] = struct{}{}
		totalSum += l.Sum
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"lines": rows,
		"summary": map[string]interface{}{
			"orders":    len(orderSet),
			"lines":     len(rows),
			"total_sum": totalSum,
			"date_from": from,
			"date_to":   to,
		},
	})
}
