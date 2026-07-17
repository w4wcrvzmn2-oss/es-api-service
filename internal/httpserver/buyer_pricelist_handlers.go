package httpserver

import (
	"context"
	"encoding/json"
	"es_api_service/internal/db"
	"es_api_service/internal/models"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// handleGetBuyerPriceLists — GET /api/buyers/{id}/price-lists
// Возвращает прайс-листы, назначенные покупателю.
func (s *Server) handleGetBuyerPriceLists(w http.ResponseWriter, r *http.Request, buyerID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerPriceLists: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type row struct {
		PriceListID      string  `json:"price_list_id"`
		Name             string  `json:"name"`
		SupplierName     string  `json:"supplier_name"`
		MarkupPct        float64 `json:"markup_pct"`         // индивидуальная наценка клиента
		DefaultMarkupPct float64 `json:"default_markup_pct"` // наценка самого прайса
	}
	rows := []row{}
	err := s.database.GORMWith(ctx).Raw(`
		SELECT CAST(pl.PriceListID AS NVARCHAR(50)) AS PriceListID,
		       pl.Name AS Name,
		       ISNULL(sup.Name, '') AS SupplierName,
		       CAST(ISNULL(bpl.MarkupPct, 0) AS FLOAT) AS MarkupPct,
		       CAST(ISNULL(pl.DefaultMarkupPct, 0) AS FLOAT) AS DefaultMarkupPct
		FROM BuyerPriceList bpl
		INNER JOIN PriceList pl ON pl.PriceListID = bpl.PriceListID
		LEFT JOIN Supplier sup ON sup.SupplierID = pl.SupplierID
		WHERE bpl.BuyerID = ? AND bpl.IsActive = 1
		ORDER BY pl.Name`, db.UUIDParam(buyerID)).Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения прайсов покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения прайсов покупателя")
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleSetBuyerPriceLists — PUT /api/buyers/{id}/price-lists
// Body: {"price_list_ids": ["uuid", ...]}. Полностью заменяет набор назначений.
func (s *Server) handleSetBuyerPriceLists(w http.ResponseWriter, r *http.Request, buyerID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleSetBuyerPriceLists: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req struct {
		Items []struct {
			PriceListID string  `json:"price_list_id"`
			MarkupPct   float64 `json:"markup_pct"`
		} `json:"items"`
		// Совместимость со старым форматом без наценки.
		PriceListIDs []string `json:"price_list_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	// Нормализуем к единому виду [{PriceListID, MarkupPct}].
	type item struct {
		id     string
		markup float64
	}
	var items []item
	if len(req.Items) > 0 {
		for _, it := range req.Items {
			items = append(items, item{id: it.PriceListID, markup: it.MarkupPct})
		}
	} else {
		for _, id := range req.PriceListIDs {
			items = append(items, item{id: id, markup: 0})
		}
	}

	now := time.Now().UTC()
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		// Полная замена: удаляем старые, вставляем новые.
		if e := tx.Exec(`DELETE FROM BuyerPriceList WHERE BuyerID = ?`, db.UUIDParam(buyerID)).Error; e != nil {
			return e
		}
		seen := make(map[string]bool)
		for _, it := range items {
			if it.id == "" || seen[it.id] {
				continue
			}
			seen[it.id] = true
			link := models.BuyerPriceList{
				BuyerPriceListID: uuid.New().String(),
				BuyerID:          buyerID,
				PriceListID:      it.id,
				MarkupPct:        it.markup,
				IsActive:         true,
				CreatedAt:        now,
			}
			if e := tx.Create(&link).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		s.logger.Error("Ошибка сохранения прайсов покупателя %s: %v", buyerID, err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка сохранения прайсов покупателя")
		return
	}

	s.logger.Info("Покупателю %s назначено прайсов: %d", buyerID, len(items))
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Прайсы покупателя обновлены",
		"buyer_id": buyerID,
		"count":    len(items),
	})
}

// handleGetPriceListBuyers — GET /api/price-lists/{id}/buyers
// Возвращает покупателей, подключённых к данному прайс-листу.
func (s *Server) handleGetPriceListBuyers(w http.ResponseWriter, r *http.Request, priceListID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetPriceListBuyers: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type row struct {
		BuyerID    string `json:"buyer_id"`
		Name       string `json:"name"`
		INN        string `json:"inn"`
		RegionName string `json:"region_name"`
	}
	rows := []row{}
	err := s.database.GORMWith(ctx).Raw(`
		SELECT CAST(b.BuyerID AS NVARCHAR(50)) AS BuyerID,
		       b.Name AS Name,
		       ISNULL(b.INN, '') AS INN,
		       ISNULL(r.Name, '') AS RegionName
		FROM BuyerPriceList bpl
		INNER JOIN Buyer b ON b.BuyerID = bpl.BuyerID
		LEFT JOIN Region r ON r.RegionID = b.RegionID
		WHERE bpl.PriceListID = ? AND bpl.IsActive = 1
		ORDER BY b.Name`, db.UUIDParam(priceListID)).Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения клиентов прайса: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения клиентов прайса")
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}
