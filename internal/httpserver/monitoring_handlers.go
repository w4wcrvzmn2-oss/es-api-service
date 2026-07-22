package httpserver

import (
	"context"
	"net/http"
	"time"
)

// handleMonitoring — GET /api/monitoring
// Сводка по всей платформе для раздела «Мониторинг» (админка + управление).
// Каждый блок считается отдельным дешёвым запросом; ошибка одного не роняет весь ответ.
func (s *Server) handleMonitoring(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleMonitoring: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	g := s.database.GORMWith(ctx)

	scalar := func(q string) int64 {
		var n int64
		_ = g.Raw(q).Row().Scan(&n)
		return n
	}

	suppliers := scalar(`SELECT COUNT(*) FROM Supplier WHERE IsActive = 1`)
	buyers := scalar(`SELECT COUNT(*) FROM Buyer WHERE IsActive = 1`)
	priceLists := scalar(`SELECT COUNT(*) FROM PriceList`)

	// Прайсы: сопоставлено / не сопоставлено среди активных.
	var matched, unmatched int64
	_ = g.Raw(`
		SELECT
			COUNT(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS NVARCHAR(50)) <> '' THEN 1 END),
			COUNT(CASE WHEN sp.GUID_ES IS NULL OR CAST(sp.GUID_ES AS NVARCHAR(50)) = '' THEN 1 END)
		FROM SupplierPrice sp WHERE sp.IsActive = 1
	`).Row().Scan(&matched, &unmatched)

	// Заказы: всего, сегодня (UTC), сумма.
	var ordersTotal, ordersToday int64
	var ordersSum float64
	_ = g.Raw(`
		SELECT COUNT(*),
		       SUM(CASE WHEN o.CreatedAt >= CAST(GETUTCDATE() AS DATE) THEN 1 ELSE 0 END),
		       ISNULL(SUM(o.TotalAmount), 0)
		FROM [Order] o
	`).Row().Scan(&ordersTotal, &ordersToday, &ordersSum)

	// Заказы по статусам.
	type statusRow struct {
		Status string  `json:"status"`
		Count  int64   `json:"count"`
		Sum    float64 `json:"sum"`
	}
	byStatus := []statusRow{}
	_ = g.Raw(`
		SELECT ISNULL(os.Name, N'Без статуса') AS Status, COUNT(*) AS Count, ISNULL(SUM(o.TotalAmount),0) AS Sum
		FROM [Order] o
		LEFT JOIN OrderStatus os ON os.OrderStatusID = o.OrderStatusID
		GROUP BY os.Name
		ORDER BY COUNT(*) DESC
	`).Scan(&byStatus)

	type recentRow struct {
		OrderID   string    `json:"order_id"`
		CreatedAt time.Time `json:"created_at"`
		BuyerName string    `json:"buyer_name"`
		Status    *string   `json:"status"`
		Total     float64   `json:"total"`
	}
	recent := []recentRow{}
	_ = g.Raw(`
		SELECT TOP 10
			CAST(o.OrderID AS NVARCHAR(50)) AS OrderID,
			o.CreatedAt AS CreatedAt,
			b.Name AS BuyerName,
			os.Name AS Status,
			ISNULL(o.TotalAmount, 0) AS Total
		FROM [Order] o
		JOIN BuyerUser bu ON bu.BuyerUserID = o.BuyerUserID
		JOIN Buyer b ON b.BuyerID = bu.BuyerID
		LEFT JOIN OrderStatus os ON os.OrderStatusID = o.OrderStatusID
		ORDER BY o.CreatedAt DESC
	`).Scan(&recent)

	// Топ-500 наименований по заказанному количеству.
	type topItemRow struct {
		ItemName    string  `json:"item_name"`
		OrdersCount int64   `json:"orders_count"`
		TotalQty    float64 `json:"total_qty"`
		TotalSum    float64 `json:"total_sum"`
	}
	topItems := []topItemRow{}
	_ = g.Raw(`
		SELECT TOP 500
			COALESCE(NULLIF(LTRIM(RTRIM(p.Name)), ''), N'—') AS ItemName,
			COUNT(DISTINCT oi.OrderID) AS OrdersCount,
			ISNULL(SUM(oi.Qty), 0) AS TotalQty,
			ISNULL(SUM(oi.Qty * oi.UnitPrice), 0) AS TotalSum
		FROM OrderItem oi
		LEFT JOIN Product p ON p.ProductID = oi.ProductID
		GROUP BY COALESCE(NULLIF(LTRIM(RTRIM(p.Name)), ''), N'—')
		ORDER BY SUM(oi.Qty) DESC, SUM(oi.Qty * oi.UnitPrice) DESC
	`).Scan(&topItems)

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"suppliers":   suppliers,
		"buyers":      buyers,
		"price_lists": priceLists,
		"prices": map[string]int64{
			"total":     matched + unmatched,
			"matched":   matched,
			"unmatched": unmatched,
		},
		"orders": map[string]interface{}{
			"total":     ordersTotal,
			"today":     ordersToday,
			"sum":       ordersSum,
			"by_status": byStatus,
		},
		"recent_orders": recent,
		"top_items":     topItems,
		"generated_at":  time.Now().UTC(),
	})
}
