package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"es_api_service/internal/db"
	"es_api_service/internal/models"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ===============================
// ОБРАБОТЧИКИ ДЛЯ ЗАКАЗОВ (admin-side)
// ===============================

// handleOrdersRouter роутит запросы к /api/orders
func (s *Server) handleOrdersRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/orders")
	path = strings.TrimPrefix(path, "/")

	if path != "" && !strings.Contains(path, "/") {
		switch r.Method {
		case http.MethodGet:
			s.handleGetOrderByID(w, r, path)
		case http.MethodPut:
			s.handleUpdateOrder(w, r, path)
		case http.MethodDelete:
			s.handleCancelOrder(w, r, path)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.HasSuffix(path, "/place") {
		orderID := strings.TrimSuffix(path, "/place")
		if r.Method == http.MethodPost {
			s.handlePlaceOrder(w, r, orderID)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.Contains(path, "/items") {
		parts := strings.Split(path, "/items")
		orderID := parts[0]
		if r.Method == http.MethodPost {
			s.handleAddOrderItem(w, r, orderID)
		} else if r.Method == http.MethodGet {
			s.handleGetOrderItems(w, r, orderID)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetOrders(w, r)
	case http.MethodPost:
		s.handleCreateOrder(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// orderListRow — DTO для GET-ответов со списком заказов.
type orderListRow struct {
	OrderID            string
	BuyerUserID        string
	BuyerApplicationID string
	BuyerLocationID    *string
	OrderStatusID      string
	CreatedAt          time.Time
	PlacedAt           *time.Time
	TotalAmount        *float64
	Comment            *string
	BuyerUserName      *string
	BuyerName          *string
	OrderStatusName    *string
	LocationAddress    *string
}

func (row orderListRow) toModel() models.Order {
	return models.Order{
		OrderID:            row.OrderID,
		BuyerUserID:        row.BuyerUserID,
		BuyerApplicationID: row.BuyerApplicationID,
		BuyerLocationID:    row.BuyerLocationID,
		OrderStatusID:      row.OrderStatusID,
		CreatedAt:          row.CreatedAt,
		PlacedAt:           row.PlacedAt,
		TotalAmount:        row.TotalAmount,
		Comment:            row.Comment,
		BuyerUserName:      row.BuyerUserName,
		BuyerName:          row.BuyerName,
		OrderStatusName:    row.OrderStatusName,
		LocationAddress:    row.LocationAddress,
	}
}

// handleGetOrders возвращает список заказов
func (s *Server) handleGetOrders(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetOrders: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerID := r.URL.Query().Get("buyer_id")
	buyerUserID := r.URL.Query().Get("buyer_user_id")
	statusID := r.URL.Query().Get("status_id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 100
	offset := 0
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}
	if offsetStr != "" {
		fmt.Sscanf(offsetStr, "%d", &offset)
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			CAST(o.OrderID AS TEXT) AS OrderID,
			CAST(o.BuyerUserID AS TEXT) AS BuyerUserID,
			CAST(o.BuyerApplicationID AS TEXT) AS BuyerApplicationID,
			CAST(o.BuyerLocationID AS TEXT) AS BuyerLocationID,
			CAST(o.OrderStatusID AS TEXT) AS OrderStatusID,
			o.CreatedAt, o.PlacedAt, o.TotalAmount, o.Comment,
			bu.FullName AS BuyerUserName,
			b.Name AS BuyerName,
			os.Name AS OrderStatusName,
			bl.Address AS LocationAddress
		FROM "Order" o
		INNER JOIN BuyerUser bu ON o.BuyerUserID = bu.BuyerUserID
		INNER JOIN Buyer b ON bu.BuyerID = b.BuyerID
		INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		LEFT JOIN BuyerLocation bl ON o.BuyerLocationID = bl.BuyerLocationID
		WHERE 1=1
	`
	var args []interface{}
	if buyerUserID != "" {
		query += " AND o.BuyerUserID = CAST(@buyerUserID AS UUID)"
		args = append(args, sql.Named("buyerUserID", buyerUserID))
	}
	if buyerID != "" {
		query += " AND bu.BuyerID = CAST(@buyerID AS UUID)"
		args = append(args, sql.Named("buyerID", buyerID))
	}
	if statusID != "" {
		query += " AND o.OrderStatusID = CAST(@statusID AS UUID)"
		args = append(args, sql.Named("statusID", statusID))
	}
	query += " ORDER BY o.CreatedAt DESC OFFSET @offset LIMIT @limit"
	args = append(args, sql.Named("offset", offset), sql.Named("limit", limit))

	var rows []orderListRow
	err := s.database.GORMWith(ctx).Raw(query, args...).Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения заказов: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказов")
		return
	}
	orders := make([]models.Order, 0, len(rows))
	for _, row := range rows {
		orders = append(orders, row.toModel())
	}
	s.writeJSON(w, http.StatusOK, orders)
}

// handleGetOrderByID возвращает заказ по ID с позициями
func (s *Server) handleGetOrderByID(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetOrderByID: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var row orderListRow
	err := s.database.GORMWith(ctx).Raw(
		`SELECT
			CAST(o.OrderID AS TEXT) AS OrderID,
			CAST(o.BuyerUserID AS TEXT) AS BuyerUserID,
			CAST(o.BuyerApplicationID AS TEXT) AS BuyerApplicationID,
			CAST(o.BuyerLocationID AS TEXT) AS BuyerLocationID,
			CAST(o.OrderStatusID AS TEXT) AS OrderStatusID,
			o.CreatedAt, o.PlacedAt, o.TotalAmount, o.Comment,
			bu.FullName AS BuyerUserName,
			b.Name AS BuyerName,
			os.Name AS OrderStatusName,
			bl.Address AS LocationAddress
		FROM "Order" o
		INNER JOIN BuyerUser bu ON o.BuyerUserID = bu.BuyerUserID
		INNER JOIN Buyer b ON bu.BuyerID = b.BuyerID
		INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID
		LEFT JOIN BuyerLocation bl ON o.BuyerLocationID = bl.BuyerLocationID
		WHERE o.OrderID = ?`,
		db.UUIDParam(orderID),
	).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка получения заказа: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказа")
		return
	}
	o := row.toModel()

	// Позиции заказа. SupplierItem не используется (см. MD/15), LEFT JOIN на Product.
	type itemRow struct {
		OrderLineID      string
		OrderID          string
		SupplierID       string
		SupplierItemID   *string
		ProductID        *string
		RegionID         *string
		Qty              float64
		UnitPrice        float64
		PriceListID      *string
		CreatedAt        time.Time
		SupplierName     *string
		SupplierItemName *string
		ProductName      *string
	}
	var rows []itemRow
	err = s.database.GORMWith(ctx).Raw(
		`SELECT CAST(oi.OrderLineID AS TEXT) AS OrderLineID,
			CAST(oi.OrderID AS TEXT) AS OrderID,
			CAST(oi.SupplierID AS TEXT) AS SupplierID,
			CAST(oi.SupplierItemID AS TEXT) AS SupplierItemID,
			CAST(oi.ProductID AS TEXT) AS ProductID,
			CAST(oi.RegionID AS TEXT) AS RegionID,
			oi.Qty, oi.UnitPrice,
			CAST(oi.PriceListID AS TEXT) AS PriceListID,
			oi.CreatedAt,
			s.Name AS SupplierName,
			NULL AS SupplierItemName,
			p.Name AS ProductName
		FROM OrderItem oi
		INNER JOIN Supplier s ON oi.SupplierID = s.SupplierID
		LEFT JOIN Product p ON oi.ProductID = p.ProductID
		WHERE oi.OrderID = ?
		ORDER BY oi.CreatedAt
LIMIT 500
`,
		db.UUIDParam(orderID),
	).Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения позиций заказа: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения позиций заказа")
		return
	}
	items := make([]models.OrderItem, 0, len(rows))
	for _, ir := range rows {
		items = append(items, models.OrderItem{
			OrderLineID:      ir.OrderLineID,
			OrderID:          ir.OrderID,
			SupplierID:       ir.SupplierID,
			SupplierItemID:   ir.SupplierItemID,
			ProductID:        ir.ProductID,
			RegionID:         ir.RegionID,
			Qty:              ir.Qty,
			UnitPrice:        ir.UnitPrice,
			PriceListID:      ir.PriceListID,
			CreatedAt:        ir.CreatedAt,
			SupplierName:     ir.SupplierName,
			SupplierItemName: ir.SupplierItemName,
			ProductName:      ir.ProductName,
		})
	}

	s.writeJSON(w, http.StatusOK, models.OrderWithItems{Order: o, Items: items})
}

// handleCreateOrder создает новый заказ
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleCreateOrder: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.OrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.BuyerUserID == "" {
		s.writeError(w, http.StatusBadRequest, "buyer_user_id обязателен")
		return
	}
	if req.BuyerApplicationID == "" {
		s.writeError(w, http.StatusBadRequest, "buyer_application_id обязателен")
		return
	}
	if len(req.Items) == 0 {
		s.writeError(w, http.StatusBadRequest, "Заказ должен содержать хотя бы одну позицию")
		return
	}

	var totalAmount float64
	for _, item := range req.Items {
		totalAmount += item.Qty * item.UnitPrice
	}

	orderID := uuid.New().String()
	buyerLocationID := ""
	if req.BuyerLocationID != nil {
		buyerLocationID = *req.BuyerLocationID
	}
	comment := ""
	if req.Comment != nil {
		comment = *req.Comment
	}

	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		draftStatusID, err := s.ensureOrderStatusID(tx, "Draft", "Черновик заказа")
		if err != nil {
			return err
		}
		err = tx.Exec(`
			INSERT INTO "Order" (OrderID, BuyerUserID, BuyerApplicationID, BuyerLocationID, OrderStatusID, TotalAmount, Comment, CreatedAt)
			VALUES (?, ?, ?, ?, ?, ?, ?, (NOW() AT TIME ZONE 'utc'))`,
			db.UUIDParam(orderID),
			db.UUIDParam(req.BuyerUserID),
			db.UUIDParam(req.BuyerApplicationID),
			db.UUIDParamPtr(&buyerLocationID),
			db.UUIDParam(draftStatusID),
			totalAmount,
			comment,
		).Error
		if err != nil {
			return fmt.Errorf("Order insert: %w", err)
		}

		for _, item := range req.Items {
			plID := ""
			if item.PriceListID != nil {
				plID = *item.PriceListID
			}
			err = tx.Exec(`
				INSERT INTO OrderItem (OrderLineID, OrderID, SupplierID, SupplierItemID, ProductID, RegionID, Qty, UnitPrice, PriceListID, CreatedAt)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (NOW() AT TIME ZONE 'utc'))`,
				db.UUIDParam(uuid.New().String()),
				db.UUIDParam(orderID),
				db.UUIDParam(item.SupplierID),
				db.UUIDParamPtr(&item.SupplierItemID),
				db.UUIDParamPtr(&item.ProductID),
				db.UUIDParamPtr(&item.RegionID),
				item.Qty,
				item.UnitPrice,
				db.UUIDParamPtr(&plID),
			).Error
			if err != nil {
				return fmt.Errorf("OrderItem insert: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		s.logger.Error("Ошибка создания заказа: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания заказа")
		return
	}

	s.logger.Info("Создан заказ: %s с %d позициями, сумма: %.2f", orderID, len(req.Items), totalAmount)
	s.handleGetOrderByID(w, r, orderID)
}

// handleUpdateOrder обновляет заказ
func (s *Server) handleUpdateOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleUpdateOrder: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.OrderUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	updateParts := []string{}
	args := []interface{}{sql.Named("orderID", orderID)}

	if req.OrderStatusID != nil {
		updateParts = append(updateParts, "OrderStatusID = CAST(@statusID AS UUID)")
		args = append(args, sql.Named("statusID", *req.OrderStatusID))
	}
	if req.BuyerLocationID != nil {
		updateParts = append(updateParts, "BuyerLocationID = CAST(@locationID AS UUID)")
		args = append(args, sql.Named("locationID", *req.BuyerLocationID))
	}
	if req.Comment != nil {
		updateParts = append(updateParts, "Comment = @comment")
		args = append(args, sql.Named("comment", *req.Comment))
	}
	if len(updateParts) == 0 {
		s.writeError(w, http.StatusBadRequest, "Нет данных для обновления")
		return
	}

	query := fmt.Sprintf(`UPDATE "Order" SET %s WHERE OrderID = CAST(@orderID AS UUID)`, strings.Join(updateParts, ", "))
	res := s.database.GORMWith(ctx).Exec(query, args...)
	if res.Error != nil {
		s.logger.Error("Ошибка обновления заказа: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка обновления заказа")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}

	s.logger.Info("Обновлен заказ: %s", orderID)
	s.handleGetOrderByID(w, r, orderID)
}

// handlePlaceOrder оформляет заказ (меняет статус на Placed)
func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	s.transitionOrderStatus(w, r, orderID, "Placed", "Заказ размещён", true, "Заказ оформлен")
}

// handleCancelOrder отменяет заказ
func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	s.transitionOrderStatus(w, r, orderID, "Cancelled", "Заказ отменён", false, "Заказ отменен")
}

// transitionOrderStatus — общий путь смены статуса заказа.
// setPlacedAt = true для оформления (Placed), иначе только смена статуса.
func (s *Server) transitionOrderStatus(w http.ResponseWriter, r *http.Request, orderID, statusName, description string, setPlacedAt bool, successMsg string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в transitionOrderStatus(%s): %v", statusName, rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		statusID, err := s.ensureOrderStatusID(tx, statusName, description)
		if err != nil {
			return err
		}
		q := `UPDATE "Order" SET OrderStatusID = ?`
		if setPlacedAt {
			q += `, PlacedAt = (NOW() AT TIME ZONE 'utc')`
		}
		q += ` WHERE OrderID = ?`
		res := tx.Exec(q, db.UUIDParam(statusID), db.UUIDParam(orderID))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка %s заказа: %v", statusName, err)
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка %s заказа", statusName))
		return
	}

	s.logger.Info("%s: %s", successMsg, orderID)
	if statusName == "Placed" {
		s.handleGetOrderByID(w, r, orderID)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  successMsg,
		"order_id": orderID,
	})
}

// handleGetOrderItems возвращает позиции заказа
func (s *Server) handleGetOrderItems(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetOrderItems: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type itemRow struct {
		OrderLineID      string
		OrderID          string
		SupplierID       string
		SupplierItemID   *string
		ProductID        *string
		RegionID         *string
		Qty              float64
		UnitPrice        float64
		PriceListID      *string
		CreatedAt        time.Time
		SupplierName     *string
		SupplierItemName *string
		ProductName      *string
	}
	var rows []itemRow
	err := s.database.GORMWith(ctx).Raw(
		`SELECT CAST(oi.OrderLineID AS TEXT) AS OrderLineID,
			CAST(oi.OrderID AS TEXT) AS OrderID,
			CAST(oi.SupplierID AS TEXT) AS SupplierID,
			CAST(oi.SupplierItemID AS TEXT) AS SupplierItemID,
			CAST(oi.ProductID AS TEXT) AS ProductID,
			CAST(oi.RegionID AS TEXT) AS RegionID,
			oi.Qty, oi.UnitPrice,
			CAST(oi.PriceListID AS TEXT) AS PriceListID,
			oi.CreatedAt,
			s.Name AS SupplierName,
			NULL AS SupplierItemName,
			p.Name AS ProductName
		FROM OrderItem oi
		INNER JOIN Supplier s ON oi.SupplierID = s.SupplierID
		LEFT JOIN Product p ON oi.ProductID = p.ProductID
		WHERE oi.OrderID = ?
		ORDER BY oi.CreatedAt
LIMIT 500
`,
		db.UUIDParam(orderID),
	).Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения позиций заказа: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения позиций заказа")
		return
	}
	items := make([]models.OrderItem, 0, len(rows))
	for _, ir := range rows {
		items = append(items, models.OrderItem{
			OrderLineID:      ir.OrderLineID,
			OrderID:          ir.OrderID,
			SupplierID:       ir.SupplierID,
			SupplierItemID:   ir.SupplierItemID,
			ProductID:        ir.ProductID,
			RegionID:         ir.RegionID,
			Qty:              ir.Qty,
			UnitPrice:        ir.UnitPrice,
			PriceListID:      ir.PriceListID,
			CreatedAt:        ir.CreatedAt,
			SupplierName:     ir.SupplierName,
			SupplierItemName: ir.SupplierItemName,
			ProductName:      ir.ProductName,
		})
	}
	s.writeJSON(w, http.StatusOK, items)
}

// handleAddOrderItem добавляет позицию в заказ
func (s *Server) handleAddOrderItem(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleAddOrderItem: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.OrderItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.SupplierID == "" || req.SupplierItemID == "" || req.ProductID == "" || req.RegionID == "" {
		s.writeError(w, http.StatusBadRequest, "Все обязательные поля должны быть заполнены")
		return
	}
	if req.Qty <= 0 || req.UnitPrice <= 0 {
		s.writeError(w, http.StatusBadRequest, "Количество и цена должны быть больше 0")
		return
	}

	orderLineID := uuid.New().String()
	plID := ""
	if req.PriceListID != nil {
		plID = *req.PriceListID
	}

	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Exec(`
			INSERT INTO OrderItem (OrderLineID, OrderID, SupplierID, SupplierItemID, ProductID, RegionID, Qty, UnitPrice, PriceListID, CreatedAt)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, (NOW() AT TIME ZONE 'utc'))`,
			db.UUIDParam(orderLineID),
			db.UUIDParam(orderID),
			db.UUIDParam(req.SupplierID),
			db.UUIDParamPtr(&req.SupplierItemID),
			db.UUIDParamPtr(&req.ProductID),
			db.UUIDParamPtr(&req.RegionID),
			req.Qty,
			req.UnitPrice,
			db.UUIDParamPtr(&plID),
		).Error
		if err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE "Order"
			SET TotalAmount = (SELECT SUM(Qty * UnitPrice) FROM OrderItem WHERE OrderID = ?)
			WHERE OrderID = ?`,
			db.UUIDParam(orderID),
			db.UUIDParam(orderID),
		).Error
	})
	if err != nil {
		s.logger.Error("Ошибка добавления позиции в заказ: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка добавления позиции в заказ")
		return
	}

	s.logger.Info("Добавлена позиция в заказ %s: %s", orderID, orderLineID)
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"message":       "Позиция добавлена",
		"order_line_id": orderLineID,
		"order_id":      orderID,
	})
}

// handleGetOrderStatuses возвращает список статусов заказов
func (s *Server) handleGetOrderStatuses(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetOrderStatuses: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var statuses []models.OrderStatus
	err := s.database.GORMWith(ctx).Raw(
		`SELECT CAST(OrderStatusID AS TEXT) AS OrderStatusID,
			Name,
			Description,
			IsActive
		FROM OrderStatus
		WHERE IsActive = 1
		ORDER BY Name
LIMIT 100
`,
	).Scan(&statuses).Error
	if err != nil {
		s.logger.Error("Ошибка получения статусов заказов: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения статусов")
		return
	}
	if statuses == nil {
		statuses = []models.OrderStatus{}
	}
	s.writeJSON(w, http.StatusOK, statuses)
}
