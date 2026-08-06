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
// ВНЕШНИЙ API ДЛЯ ПОКУПАТЕЛЕЙ
// ===============================

type BuyerOrderCreateRequest struct {
	LocationID  *string             `json:"location_id,omitempty"`
	Comment     *string             `json:"comment,omitempty"`
	GlobalSign  *string             `json:"global_sign,omitempty"`
	Items       []BuyerOrderItemReq `json:"items"`
}

type BuyerOrderItemReq struct {
	SupplierPriceID string  `json:"supplier_price_id"`
	Qty             float64 `json:"qty"`
	ItemName        *string `json:"item_name,omitempty"`
	ItemCode        *string `json:"item_code,omitempty"`
	Barcode         *string `json:"barcode,omitempty"`
}

type BuyerOrderResponse struct {
	OrderID     string    `json:"order_id"`
	Status      string    `json:"status"`
	GlobalSign  string    `json:"global_sign,omitempty"`
	TotalAmount float64   `json:"total_amount"`
	ItemsCount  int       `json:"items_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type BuyerOrderListItem struct {
	OrderID         string     `json:"order_id"`
	Status          string     `json:"status"`
	GlobalSign      *string    `json:"global_sign,omitempty"`
	TotalAmount     *float64   `json:"total_amount,omitempty"`
	ItemsCount      int        `json:"items_count"`
	CreatedAt       time.Time  `json:"created_at"`
	PlacedAt        *time.Time `json:"placed_at,omitempty"`
	Comment         *string    `json:"comment,omitempty"`
	LocationAddress *string    `json:"location_address,omitempty"`
}

type BuyerOrderDetail struct {
	OrderID         string              `json:"order_id"`
	Status          string              `json:"status"`
	GlobalSign      *string             `json:"global_sign,omitempty"`
	TotalAmount     *float64            `json:"total_amount,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	PlacedAt        *time.Time          `json:"placed_at,omitempty"`
	Comment         *string             `json:"comment,omitempty"`
	LocationAddress *string             `json:"location_address,omitempty"`
	Items           []BuyerOrderItemOut `json:"items"`
}

type BuyerOrderItemOut struct {
	Name      string  `json:"name"`
	Supplier  string  `json:"supplier"`
	Qty       float64 `json:"qty"`
	UnitPrice float64 `json:"unit_price"`
	Total     float64 `json:"total"`
}

type BuyerCatalogItem struct {
	SupplierPriceID string  `json:"supplier_price_id"`
	Name            string  `json:"name"`
	Supplier        string  `json:"supplier"`
	Price           float64 `json:"price"`
	InStock         bool    `json:"in_stock"`
}

// handleBuyerOrdersRouter роутит запросы /api/buyer/*
func (s *Server) handleBuyerOrdersRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/buyer/")

	if strings.HasPrefix(path, "catalog") {
		if r.Method == http.MethodGet {
			s.handleBuyerCatalog(w, r)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.HasPrefix(path, "locations") {
		if r.Method == http.MethodGet {
			s.handleBuyerGetMyLocations(w, r)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.HasPrefix(path, "global-sign") || strings.HasPrefix(path, "order-numbers") {
		if r.Method == http.MethodPost || r.Method == http.MethodGet {
			s.handleBuyerNextGlobalSign(w, r)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if !strings.HasPrefix(path, "orders") {
		s.writeError(w, http.StatusNotFound, "Маршрут не найден")
		return
	}

	sub := strings.TrimPrefix(path, "orders")
	sub = strings.TrimPrefix(sub, "/")

	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			s.handleBuyerGetOrders(w, r)
		case http.MethodPost:
			s.handleBuyerCreateOrder(w, r)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.HasSuffix(sub, "/cancel") {
		orderID := strings.TrimSuffix(sub, "/cancel")
		orderID = strings.TrimSuffix(orderID, "/")
		if r.Method == http.MethodPost {
			s.handleBuyerCancelOrder(w, r, orderID)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if strings.HasSuffix(sub, "/place") {
		orderID := strings.TrimSuffix(sub, "/place")
		orderID = strings.TrimSuffix(orderID, "/")
		if r.Method == http.MethodPost {
			s.handleBuyerPlaceOrder(w, r, orderID)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if !strings.Contains(sub, "/") {
		if r.Method == http.MethodGet {
			s.handleBuyerGetOrderByID(w, r, sub)
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	s.writeError(w, http.StatusNotFound, "Маршрут не найден")
}

// resolveBuyerUser ищет BuyerUserID + BuyerApplicationID из JWT.
// Сначала по claims.BuyerUserID (после Phase 3 /auth/login),
// иначе fallback по claims.Username = BuyerUser.Email (legacy).
func (s *Server) resolveBuyerUser(ctx context.Context, r *http.Request) (string, string, bool) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		return "", "", false
	}

	type result struct {
		BuyerUserID        string
		BuyerApplicationID string
	}
	var res result

	query := `
		SELECT
			CAST(bu."BuyerUserID" AS TEXT) AS "BuyerUserID",
			CAST(ba."BuyerApplicationID" AS TEXT) AS "BuyerApplicationID"
		FROM "BuyerUser" bu
		INNER JOIN "BuyerApplication" ba
			ON ba."BuyerUserID" = bu."BuyerUserID" AND ba."IsActive" = TRUE
		WHERE bu."IsActive" = TRUE
	`
	args := []interface{}{}
	if claims.BuyerUserID != "" {
		query += ` AND bu."BuyerUserID" = CAST(? AS UUID)`
		args = append(args, db.UUIDParam(claims.BuyerUserID))
	} else {
		query += ` AND bu."Email" = ?`
		args = append(args, claims.Username)
	}
	query += ` LIMIT 1`

	if err := s.database.GORMWith(ctx).Raw(query, args...).Scan(&res).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.logger.Error("resolveBuyerUser: не найден BuyerUser (claims=%+v)", claims)
		} else {
			s.logger.Error("resolveBuyerUser: ошибка БД: %v", err)
		}
		return "", "", false
	}
	return res.BuyerUserID, res.BuyerApplicationID, true
}

// resolveOrderLocationID проверяет location_id покупателя.
// Если передан валидный — возвращает его; иначе подставляет default/первый адрес
// покупателя; если адресов нет — создаёт «Основной адрес».
func (s *Server) resolveOrderLocationID(ctx context.Context, buyerUserID string, requested *string) (*string, error) {
	if requested != nil && *requested != "" {
		var cnt int64
		if err := s.database.GORMWith(ctx).Raw(`
			SELECT COUNT(*)
			FROM "BuyerLocation" bl
			INNER JOIN "BuyerUser" bu ON bu."BuyerID" = bl."BuyerID"
			WHERE bl."BuyerLocationID" = CAST(? AS UUID) AND bu."BuyerUserID" = CAST(? AS UUID)`,
			db.UUIDParam(*requested), db.UUIDParam(buyerUserID)).Scan(&cnt).Error; err != nil {
			return nil, err
		}
		if cnt > 0 {
			return requested, nil
		}
		s.logger.Warn("location_id %s не принадлежит покупателю %s — подставляем адрес покупателя", *requested, buyerUserID)
	}

	var buyerID string
	if err := s.database.GORMWith(ctx).Raw(`
		SELECT CAST("BuyerID" AS TEXT) FROM "BuyerUser" WHERE "BuyerUserID" = CAST(? AS UUID) LIMIT 1`,
		db.UUIDParam(buyerUserID)).Scan(&buyerID).Error; err != nil {
		return nil, err
	}
	if buyerID == "" {
		return nil, fmt.Errorf("BuyerID не найден для BuyerUser %s", buyerUserID)
	}

	var locID string
	if err := s.database.GORMWith(ctx).Raw(`
		SELECT CAST("BuyerLocationID" AS TEXT)
		FROM "BuyerLocation"
		WHERE "BuyerID" = CAST(? AS UUID)
		ORDER BY "IsDefault" DESC, "CreatedAt"
		LIMIT 1`,
		db.UUIDParam(buyerID)).Scan(&locID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if locID != "" {
		return &locID, nil
	}

	newID := uuid.New().String()
	now := time.Now().UTC()
	if err := s.database.GORMWith(ctx).Exec(`
		INSERT INTO "BuyerLocation" ("BuyerLocationID", "BuyerID", "Address", "RegionID", "IsDefault", "CreatedAt")
		VALUES (?, ?, ?, NULL, TRUE, ?)`,
		db.UUIDParam(newID), db.UUIDParam(buyerID), "Основной адрес", now).Error; err != nil {
		return nil, fmt.Errorf("создание BuyerLocation: %w", err)
	}
	s.logger.Info("Создан BuyerLocation %s для BuyerID %s (авто при заказе)", newID, buyerID)
	return &newID, nil
}

// handleBuyerGetMyLocations — GET /api/buyer/locations (адреса текущего покупателя из JWT)
func (s *Server) handleBuyerGetMyLocations(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerGetMyLocations: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден для данного пользователя")
		return
	}

	locations := []locationWithRegion{}
	err := s.database.GORMWith(ctx).Raw(`
		SELECT
			CAST(bl."BuyerLocationID" AS TEXT) AS "BuyerLocationID",
			CAST(bl."BuyerID" AS TEXT) AS "BuyerID",
			bl."Address" AS "Address",
			CAST(bl."RegionID" AS TEXT) AS "RegionID",
			bl."IsDefault" AS "IsDefault",
			bl."CreatedAt" AS "CreatedAt",
			r."Name" AS "RegionName"
		FROM "BuyerLocation" bl
		INNER JOIN "BuyerUser" bu ON bu."BuyerID" = bl."BuyerID"
		LEFT JOIN "Region" r ON bl."RegionID" = r."RegionID"
		WHERE bu."BuyerUserID" = CAST(? AS UUID)
		ORDER BY bl."IsDefault" DESC, bl."CreatedAt"
		LIMIT 100
	`, db.UUIDParam(buyerUserID)).Scan(&locations).Error
	if err != nil {
		s.logger.Error("Ошибка получения адресов покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения адресов")
		return
	}
	s.writeJSON(w, http.StatusOK, locations)
}

// ensureOrderStatusID возвращает OrderStatusID по имени; при отсутствии создаёт.
func (s *Server) ensureOrderStatusID(tx *gorm.DB, name, description string) (string, error) {
	var id string
	err := tx.Raw(`SELECT CAST(OrderStatusID AS TEXT) FROM OrderStatus WHERE Name = ?`, name).Scan(&id).Error
	if err == nil && id != "" {
		return id, nil
	}
	id = uuid.New().String()
	st := models.OrderStatus{OrderStatusID: id, Name: name, Description: &description, IsActive: true}
	if err := tx.Table("OrderStatus").Create(&st).Error; err != nil {
		return "", err
	}
	return id, nil
}

// resolvedPriceRow — результат резолва SupplierPrice в позицию заказа.
type resolvedPriceRow struct {
	SupplierID  string
	ProductID   *string
	RegionID    *string
	PriceListID *string
	UnitPrice   float64
	ItemName    sql.NullString
	ItemCode    sql.NullString
	Barcode     sql.NullString
	SuppName    sql.NullString
}

// handleBuyerCreateOrder — POST /api/buyer/orders
func (s *Server) handleBuyerCreateOrder(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerCreateOrder: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, appID, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден для данного пользователя")
		return
	}

	var req BuyerOrderCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if len(req.Items) == 0 {
		s.writeError(w, http.StatusBadRequest, "Заказ должен содержать хотя бы одну позицию")
		return
	}

	pc := s.resolvePricingContext(ctx, r)

	// Резолвим позиции до транзакции — чтобы битый supplier_price_id возвращал 404/400
	// без открытия транзакции.
	var resolvedItems []struct {
		row  resolvedPriceRow
		qty  float64
		spID string
		req  BuyerOrderItemReq
	}
	var totalAmount float64

	// Резолвим каждую позицию отдельным запросом по PK (sp.SupplierPriceID = ?).
	// Скалярный '=' даёт чистый seek по кластерному PK. Вариант через IN (...) на
	// uniqueidentifier делал фильтр не-sargable → полный скан SupplierPrice и таймаут.
	for _, ri := range req.Items {
		if ri.Qty <= 0 {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Количество должно быть > 0 (supplier_price_id: %s)", ri.SupplierPriceID))
			return
		}

		// Денормализованная схема: см. MD/15. LEFT JOIN'ы гасят висячие FK-ссылки → NULL в OrderItem.
		var row resolvedPriceRow
		err := s.database.GORMWith(ctx).Raw(`
			SELECT
				CAST(sp."SupplierID" AS TEXT) AS "SupplierID",
				CASE WHEN p."ProductID" IS NULL THEN NULL ELSE CAST(p."ProductID" AS TEXT) END AS "ProductID",
				CASE WHEN r."RegionID" IS NULL THEN NULL ELSE CAST(r."RegionID" AS TEXT) END AS "RegionID",
				CASE WHEN spl."PriceListID" IS NULL THEN NULL ELSE CAST(spl."PriceListID" AS TEXT) END AS "PriceListID",
				`+sqlAdditiveFinalPriceExpr("sp", pc)+` AS "UnitPrice",
				sp."ItemName" AS "ItemName",
				sp."ItemCode" AS "ItemCode",
				sp."Barcode" AS "Barcode",
				sp."SupplierItemName" AS "SuppName"
			FROM "SupplierPrice" sp
			LEFT JOIN "Product" p ON p."ProductID" = sp."GUID_ES"
			LEFT JOIN "Region" r ON r."RegionID" = sp."RegionID"
			LEFT JOIN "SupplierPriceList" spl ON spl."PriceListID" = sp."PriceListID"
			WHERE sp."SupplierPriceID" = CAST(? AS UUID) AND sp."IsActive" = TRUE
			LIMIT 1
		`, db.UUIDParam(ri.SupplierPriceID)).Scan(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.writeError(w, http.StatusNotFound, fmt.Sprintf("Товар не найден или недоступен: %s", ri.SupplierPriceID))
			return
		}
		if err == nil && row.SupplierID == "" {
			s.writeError(w, http.StatusNotFound, fmt.Sprintf("Товар не найден или недоступен: %s", ri.SupplierPriceID))
			return
		}
		if err != nil {
			s.logger.Error("Ошибка получения SupplierPrice %s: %v", ri.SupplierPriceID, err)
			s.writeError(w, http.StatusInternalServerError, "Ошибка получения данных товара")
			return
		}
		totalAmount += ri.Qty * row.UnitPrice
		resolvedItems = append(resolvedItems, struct {
			row  resolvedPriceRow
			qty  float64
			spID string
			req  BuyerOrderItemReq
		}{row: row, qty: ri.Qty, spID: ri.SupplierPriceID, req: ri})
	}

	orderID := uuid.New().String()
	var locationID *string
	if req.LocationID != nil && *req.LocationID != "" {
		locationID = req.LocationID
	}

	// location_id должен принадлежать покупателю. Если прислали чужой/тестовый UUID
	// (часто из desktop seed) — подставляем реальный адрес покупателя или создаём его.
	resolvedLoc, errLoc := s.resolveOrderLocationID(ctx, buyerUserID, locationID)
	if errLoc != nil {
		s.logger.Error("Ошибка разрешения адреса доставки: %v", errLoc)
		s.writeError(w, http.StatusInternalServerError, "Ошибка проверки адреса доставки")
		return
	}
	locationID = resolvedLoc

	commentStr := ""
	if req.Comment != nil {
		commentStr = *req.Comment
	}

	now := time.Now().UTC()
	var assignedSign string

	// Атомарная транзакция: заказ + все позиции одной транзакцией.
	// Закрывает [QG-RISK] из MD/15 (раньше при сбое INSERT позиции заказ оставался «пустым»).
	// Desktop «Отправить заказ» — разовый POST с позициями, не черновик корзины.
	// Сразу Placed, иначе менеджер/поставщик видят Draft, а аптека уже «ОТПРАВЛЕН».
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		placedStatusID, err := s.ensureOrderStatusID(tx, "Placed", "Заказ размещён")
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}

		sign, err := s.resolveGlobalSignForCreate(tx, req.GlobalSign)
		if err != nil {
			return fmt.Errorf("global_sign: %w", err)
		}
		assignedSign = sign

		// GORM-driver/sqlserver квотирует имена через "...", а в MSSQL имя "Order"
		// (зарезервированное слово) понимается только в квадратных скобках.
		// Поэтому INSERT и UPDATE по "Order" делаем через сырой Exec.
		if err := tx.Exec(`
			INSERT INTO "Order" (OrderID, BuyerUserID, BuyerApplicationID, BuyerLocationID, OrderStatusID, TotalAmount, Comment, CreatedAt, PlacedAt, "GlobalSign")
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			db.UUIDParam(orderID),
			db.UUIDParam(buyerUserID),
			db.UUIDParam(appID),
			db.UUIDParamPtr(locationID),
			db.UUIDParam(placedStatusID),
			totalAmount,
			commentStr,
			now,
			now,
			sign,
		).Error; err != nil {
			return fmt.Errorf("Order insert: %w", err)
		}

		for _, it := range resolvedItems {
			spID := it.spID
			itemName, itemCode, barcode := pickOrderItemSnapshot(it.req, it.row)
			line := models.OrderItem{
				OrderLineID:     uuid.New().String(),
				OrderID:         orderID,
				SupplierID:      it.row.SupplierID,
				SupplierItemID:  nil,
				SupplierPriceID: &spID,
				ProductID:       it.row.ProductID,
				RegionID:        it.row.RegionID,
				ItemName:        itemName,
				ItemCode:        itemCode,
				Barcode:         barcode,
				Qty:             it.qty,
				UnitPrice:       it.row.UnitPrice,
				PriceListID:     it.row.PriceListID,
				CreatedAt:       now,
			}
			if err := tx.Table("OrderItem").Create(&line).Error; err != nil {
				return fmt.Errorf("OrderItem insert: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		s.logger.Error("Ошибка создания заказа покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания заказа")
		return
	}

	s.logger.Info("Покупатель %s создал заказ %s (Placed, %s), позиций: %d, сумма: %.2f", buyerUserID, orderID, assignedSign, len(resolvedItems), totalAmount)
	s.writeJSON(w, http.StatusCreated, BuyerOrderResponse{
		OrderID:     orderID,
		Status:      "Placed",
		GlobalSign:  assignedSign,
		TotalAmount: totalAmount,
		ItemsCount:  len(resolvedItems),
		CreatedAt:   now,
	})
}

// handleBuyerGetOrders — GET /api/buyer/orders
func (s *Server) handleBuyerGetOrders(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerGetOrders: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	limit := 50
	offset := 0
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	base := s.database.GORMWith(ctx).
		Table(`"Order" AS o`).
		Joins("INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID").
		Where("o.BuyerUserID = ?", db.UUIDParam(buyerUserID))
	if statusFilter != "" {
		base = base.Where("os.Name = ?", statusFilter)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		s.logger.Error("Ошибка подсчёта заказов покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказов")
		return
	}

	orders := []BuyerOrderListItem{}
	err := base.
		Select(`CAST(o.OrderID AS TEXT) AS OrderID,
			os.Name AS Status,
			o."GlobalSign" AS GlobalSign,
			o.TotalAmount,
			(SELECT COUNT(*) FROM OrderItem WHERE OrderID = o.OrderID) AS ItemsCount,
			o.CreatedAt,
			o.PlacedAt,
			o.Comment,
			bl.Address AS LocationAddress`).
		Joins("LEFT JOIN BuyerLocation bl ON o.BuyerLocationID = bl.BuyerLocationID").
		Order("o.CreatedAt DESC").
		Offset(offset).
		Limit(limit).
		Scan(&orders).Error
	if err != nil {
		s.logger.Error("Ошибка получения заказов покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказов")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"orders": orders,
		"total":  total,
	})
}

// handleBuyerGetOrderByID — GET /api/buyer/orders/{id}
func (s *Server) handleBuyerGetOrderByID(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerGetOrderByID: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	type orderRow struct {
		OrderID         string
		OwnerID         string
		Status          string
		GlobalSign      *string
		TotalAmount     *float64
		CreatedAt       time.Time
		PlacedAt        *time.Time
		Comment         *string
		LocationAddress *string
	}
	var row orderRow
	err := s.database.GORMWith(ctx).
		Table(`"Order" AS o`).
		Select(`CAST(o.OrderID AS TEXT) AS OrderID,
			CAST(o.BuyerUserID AS TEXT) AS OwnerID,
			os.Name AS Status,
			o."GlobalSign" AS GlobalSign,
			o.TotalAmount,
			o.CreatedAt,
			o.PlacedAt,
			o.Comment,
			bl.Address AS LocationAddress`).
		Joins("INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID").
		Joins("LEFT JOIN BuyerLocation bl ON o.BuyerLocationID = bl.BuyerLocationID").
		Where("o.OrderID = ?", db.UUIDParam(orderID)).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка получения заказа покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказа")
		return
	}
	if !strings.EqualFold(row.OwnerID, buyerUserID) {
		s.writeError(w, http.StatusForbidden, "Нет доступа к данному заказу")
		return
	}

	d := BuyerOrderDetail{
		OrderID:         row.OrderID,
		Status:          row.Status,
		GlobalSign:      row.GlobalSign,
		TotalAmount:     row.TotalAmount,
		CreatedAt:       row.CreatedAt,
		PlacedAt:        row.PlacedAt,
		Comment:         row.Comment,
		LocationAddress: row.LocationAddress,
		Items:           []BuyerOrderItemOut{},
	}

	// Имя позиции теперь берём из Product (если ProductID не NULL), иначе пусто.
	// SupplierItem не используется (см. MD/15).
	type itemRow struct {
		Name      *string
		Supplier  string
		Qty       float64
		UnitPrice float64
	}
	var rows []itemRow
	err = s.database.GORMWith(ctx).
		Table("OrderItem AS oi").
		Select(`p.Name AS Name,
			sup.Name AS Supplier,
			oi.Qty,
			oi.UnitPrice`).
		Joins("INNER JOIN Supplier sup ON oi.SupplierID = sup.SupplierID").
		Joins("LEFT JOIN Product p ON oi.ProductID = p.ProductID").
		Where("oi.OrderID = ?", db.UUIDParam(orderID)).
		Order("oi.CreatedAt").
		Scan(&rows).Error
	if err != nil {
		s.logger.Error("Ошибка получения позиций заказа покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения позиций")
		return
	}
	for _, ir := range rows {
		name := ""
		if ir.Name != nil {
			name = *ir.Name
		}
		d.Items = append(d.Items, BuyerOrderItemOut{
			Name:      name,
			Supplier:  ir.Supplier,
			Qty:       ir.Qty,
			UnitPrice: ir.UnitPrice,
			Total:     ir.Qty * ir.UnitPrice,
		})
	}

	s.writeJSON(w, http.StatusOK, d)
}

// handleBuyerNextGlobalSign — GET/POST /api/buyer/global-sign/next
// Резервирует следующий глобальный номер EX-####### для десктопа до создания заказа.
func (s *Server) handleBuyerNextGlobalSign(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerNextGlobalSign: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if _, _, ok := s.resolveBuyerUser(ctx, r); !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	var sign string
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		sign, e = s.reserveGlobalSign(tx)
		return e
	})
	if err != nil {
		s.logger.Error("Ошибка резервирования GlobalSign: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Не удалось выделить номер заказа")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"global_sign": sign,
	})
}

// handleBuyerPlaceOrder — POST /api/buyer/orders/{id}/place
// Оформляет черновик (Draft → Placed). Для совместимости со старыми клиентами.
func (s *Server) handleBuyerPlaceOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerPlaceOrder: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	type checkRow struct {
		OwnerID       string
		CurrentStatus string
	}
	var chk checkRow
	err := s.database.GORMWith(ctx).
		Table(`"Order" AS o`).
		Select(`CAST(o.BuyerUserID AS TEXT) AS OwnerID, os.Name AS CurrentStatus`).
		Joins("INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID").
		Where("o.OrderID = ?", db.UUIDParam(orderID)).
		Take(&chk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказа")
		return
	}
	if !strings.EqualFold(chk.OwnerID, buyerUserID) {
		s.writeError(w, http.StatusForbidden, "Нет доступа к данному заказу")
		return
	}
	if chk.CurrentStatus == "Placed" {
		s.handleBuyerGetOrderByID(w, r, orderID)
		return
	}
	if chk.CurrentStatus != "Draft" {
		s.writeError(w, http.StatusConflict, fmt.Sprintf("Нельзя оформить заказ в статусе «%s». Допустимый: Draft", chk.CurrentStatus))
		return
	}

	err = s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		placedID, err := s.ensureOrderStatusID(tx, "Placed", "Заказ размещён")
		if err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE "Order"
			SET OrderStatusID = CAST(? AS UUID),
			    PlacedAt = (NOW() AT TIME ZONE 'utc')
			WHERE OrderID = CAST(? AS UUID)`,
			placedID, orderID).Error
	})
	if err != nil {
		s.logger.Error("Ошибка оформления заказа покупателем: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка оформления заказа")
		return
	}

	s.logger.Info("Покупатель %s оформил заказ %s (Placed)", buyerUserID, orderID)
	s.handleBuyerGetOrderByID(w, r, orderID)
}

// handleBuyerCancelOrder — POST /api/buyer/orders/{id}/cancel
func (s *Server) handleBuyerCancelOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerCancelOrder: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "Покупатель не найден")
		return
	}

	type checkRow struct {
		OwnerID       string
		CurrentStatus string
	}
	var chk checkRow
	err := s.database.GORMWith(ctx).
		Table(`"Order" AS o`).
		Select(`CAST(o.BuyerUserID AS TEXT) AS OwnerID, os.Name AS CurrentStatus`).
		Joins("INNER JOIN OrderStatus os ON o.OrderStatusID = os.OrderStatusID").
		Where("o.OrderID = ?", db.UUIDParam(orderID)).
		Take(&chk).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Заказ не найден")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения заказа")
		return
	}

	if !strings.EqualFold(chk.OwnerID, buyerUserID) {
		s.writeError(w, http.StatusForbidden, "Нет доступа к данному заказу")
		return
	}

	if chk.CurrentStatus != "Draft" && chk.CurrentStatus != "Placed" {
		s.writeError(w, http.StatusConflict, fmt.Sprintf("Нельзя отменить заказ в статусе «%s». Допустимые: Draft, Placed", chk.CurrentStatus))
		return
	}

	err = s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		cancelledID, err := s.ensureOrderStatusID(tx, "Cancelled", "Заказ отменён")
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE "Order" SET OrderStatusID = CAST(? AS UUID) WHERE OrderID = CAST(? AS UUID)`, cancelledID, orderID).Error
	})
	if err != nil {
		s.logger.Error("Ошибка отмены заказа покупателем: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка отмены заказа")
		return
	}

	s.logger.Info("Покупатель %s отменил заказ %s", buyerUserID, orderID)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Заказ отменён",
		"order_id": orderID,
		"status":   "Cancelled",
	})
}

// handleBuyerCatalog — GET /api/buyer/catalog
// Переписан под актуальную схему (см. MD/15): SupplierItem не используется,
// активность — sp.IsActive, имя берётся из самого SupplierPrice.ItemName.
func (s *Server) handleBuyerCatalog(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleBuyerCatalog: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	search := r.URL.Query().Get("q")
	regionID := r.URL.Query().Get("region_id")
	limit := 50
	offset := 0
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	base := s.database.GORMWith(ctx).
		Table("SupplierPrice AS sp").
		Joins("INNER JOIN Supplier sup ON sp.SupplierID = sup.SupplierID").
		Where("sp.IsActive = ?", true)
	if search != "" && len(search) >= 2 {
		base = base.Where("sp.ItemName LIKE ?", "%"+search+"%")
	}
	if regionID != "" {
		base = base.Where("sp.RegionID = ?", db.UUIDParam(regionID))
	}

	// Ограничение по назначенным прайс-листам покупателя.
	// Страховка: если назначений нет (или покупатель не резолвится) — фильтр не применяется,
	// каталог работает как раньше (по региону). Предикат принадлежности прайсу повторяет
	// логику price_list_handlers.go (прямой PriceListID ИЛИ цепочка ImportPoint→InvoiceImport).
	if buyerUserID, _, ok := s.resolveBuyerUser(ctx, r); ok {
		var buyerID string
		_ = s.database.GORMWith(ctx).
			Raw(`SELECT CAST(BuyerID AS TEXT) FROM BuyerUser WHERE BuyerUserID = ?`, db.UUIDParam(buyerUserID)).
			Scan(&buyerID).Error
		if buyerID != "" {
			var assignedCount int64
			_ = s.database.GORMWith(ctx).
				Raw(`SELECT COUNT(*) FROM BuyerPriceList WHERE BuyerID = ? AND IsActive = 1`, db.UUIDParam(buyerID)).
				Scan(&assignedCount).Error
			if assignedCount > 0 {
				base = base.Where(`EXISTS (
					SELECT 1 FROM BuyerPriceList bpl
					INNER JOIN PriceList pl ON pl.PriceListID = bpl.PriceListID
					LEFT JOIN InvoiceImport ii ON ii.InvoiceImportID = sp.InvoiceImportID
					WHERE bpl.BuyerID = ? AND bpl.IsActive = 1
					  AND ( sp.PriceListID = pl.PriceListID
					        OR (pl.ImportPointID IS NOT NULL AND ii.ImportPointID = pl.ImportPointID AND pl.SupplierID = sp.SupplierID) )
				)`, db.UUIDParam(buyerID))
			}
		}
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		s.logger.Error("Ошибка подсчёта каталога: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения каталога")
		return
	}

	items := []BuyerCatalogItem{}
	pc := s.resolvePricingContext(ctx, r)
	err := base.
		Select(`CAST(sp.SupplierPriceID AS TEXT) AS SupplierPriceID,
			COALESCE(sp.ItemName, '') AS Name,
			sup.Name AS Supplier,
			`+sqlAdditiveFinalPriceExpr("sp", pc)+` AS Price,
			sp.IsActive AS InStock`).
		Order("sp.ItemName").
		Offset(offset).
		Limit(limit).
		Scan(&items).Error
	if err != nil {
		s.logger.Error("Ошибка каталога покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения каталога")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

func pickOrderItemSnapshot(req BuyerOrderItemReq, row resolvedPriceRow) (name, code, barcode *string) {
	if req.ItemName != nil {
		if v := strings.TrimSpace(*req.ItemName); v != "" {
			name = &v
		}
	}
	if name == nil {
		if row.ItemName.Valid {
			if v := strings.TrimSpace(row.ItemName.String); v != "" {
				name = &v
			}
		} else if row.SuppName.Valid {
			if v := strings.TrimSpace(row.SuppName.String); v != "" {
				name = &v
			}
		}
	}

	if req.ItemCode != nil {
		if v := strings.TrimSpace(*req.ItemCode); v != "" {
			code = &v
		}
	}
	if code == nil && row.ItemCode.Valid {
		if v := strings.TrimSpace(row.ItemCode.String); v != "" {
			code = &v
		}
	}

	if req.Barcode != nil {
		if v := strings.TrimSpace(*req.Barcode); v != "" {
			barcode = &v
		}
	}
	if barcode == nil && row.Barcode.Valid {
		if v := strings.TrimSpace(row.Barcode.String); v != "" {
			barcode = &v
		}
	}
	return name, code, barcode
}
