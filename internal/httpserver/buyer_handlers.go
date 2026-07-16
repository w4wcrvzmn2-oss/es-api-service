package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"es_api_service/internal/db"
	"es_api_service/internal/models"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ===============================
// ОБРАБОТЧИКИ ДЛЯ ПОКУПАТЕЛЕЙ
// ===============================

// buyerWithRegion — DTO для GET-ответов с подгруженным регионом.
type buyerWithRegion struct {
	models.Buyer
	RegionName *string `json:"region_name,omitempty"`
}

// buyerUserWithBuyer — DTO для пользователей с именем покупателя.
type buyerUserWithBuyer struct {
	models.BuyerUser
	BuyerName string `json:"buyer_name"`
}

// locationWithRegion — DTO для адресов с именем региона.
type locationWithRegion struct {
	models.BuyerLocation
	RegionName *string `json:"region_name,omitempty"`
}

// handleBuyersRouter роутит запросы к /api/buyers
func (s *Server) handleBuyersRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/buyers")
	path = strings.TrimPrefix(path, "/")

	// Подпуть {id}/price-lists — назначенные покупателю прайс-листы.
	if parts := strings.Split(path, "/"); len(parts) == 2 && parts[0] != "" && parts[1] == "price-lists" {
		switch r.Method {
		case http.MethodGet:
			s.handleGetBuyerPriceLists(w, r, parts[0])
		case http.MethodPut:
			s.handleSetBuyerPriceLists(w, r, parts[0])
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if path != "" && !strings.Contains(path, "/") {
		switch r.Method {
		case http.MethodGet:
			s.handleGetBuyerByID(w, r, path)
		case http.MethodPut:
			s.handleUpdateBuyer(w, r, path)
		case http.MethodDelete:
			s.handleDeleteBuyer(w, r, path)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetBuyers(w, r)
	case http.MethodPost:
		s.handleCreateBuyer(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// handleGetBuyers возвращает список покупателей
func (s *Server) handleGetBuyers(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyers: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyers := []buyerWithRegion{}
	err := s.database.GORMWith(ctx).
		Table("Buyer AS b").
		Select(`CAST(b.BuyerID AS NVARCHAR(50)) AS BuyerID,
			b.Name, b.INN,
			CAST(b.RegionID AS NVARCHAR(50)) AS RegionID,
			b.Code, b.Phone, b.Address, b.Email,
			b.IsActive, b.CreatedAt,
			r.Name AS RegionName`).
		Joins("LEFT JOIN Region r ON b.RegionID = r.RegionID").
		Where("b.IsActive = ?", true).
		Order("b.Name").
		Limit(500).
		Scan(&buyers).Error
	if err != nil {
		s.logger.Error("Ошибка получения покупателей: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения покупателей")
		return
	}
	s.writeJSON(w, http.StatusOK, buyers)
}

// handleGetBuyerByID возвращает покупателя по ID
func (s *Server) handleGetBuyerByID(w http.ResponseWriter, r *http.Request, buyerID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerByID: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var b buyerWithRegion
	err := s.database.GORMWith(ctx).
		Table("Buyer AS b").
		Select(`CAST(b.BuyerID AS NVARCHAR(50)) AS BuyerID,
			b.Name, b.INN,
			CAST(b.RegionID AS NVARCHAR(50)) AS RegionID,
			b.Code, b.Phone, b.Address, b.Email,
			b.IsActive, b.CreatedAt,
			r.Name AS RegionName`).
		Joins("LEFT JOIN Region r ON b.RegionID = r.RegionID").
		Where("b.BuyerID = ?", db.UUIDParam(buyerID)).
		Take(&b).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Покупатель не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка получения покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения покупателя")
		return
	}
	s.writeJSON(w, http.StatusOK, b)
}

// handleCreateBuyer создает нового покупателя
func (s *Server) handleCreateBuyer(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleCreateBuyer: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.BuyerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Наименование покупателя обязательно")
		return
	}

	buyerID := uuid.New().String()
	buyer := models.Buyer{
		BuyerID:   buyerID,
		Name:      req.Name,
		INN:       req.INN,
		RegionID:  nilIfEmpty(req.RegionID),
		Code:      nilIfEmpty(req.Code),
		Phone:     nilIfEmpty(req.Phone),
		Address:   nilIfEmpty(req.Address),
		Email:     nilIfEmpty(req.Email),
		IsActive:  req.IsActive,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.database.GORMWith(ctx).Create(&buyer).Error; err != nil {
		s.logger.Error("Ошибка создания покупателя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания покупателя")
		return
	}

	s.logger.Info("Создан покупатель: %s - %s", buyerID, req.Name)
	s.handleGetBuyerByID(w, r, buyerID)
}

// handleUpdateBuyer обновляет покупателя
func (s *Server) handleUpdateBuyer(w http.ResponseWriter, r *http.Request, buyerID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleUpdateBuyer: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.BuyerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Наименование покупателя обязательно")
		return
	}

	res := s.database.GORMWith(ctx).
		Model(&models.Buyer{}).
		Where("BuyerID = ?", db.UUIDParam(buyerID)).
		Updates(map[string]interface{}{
			"Name":     req.Name,
			"INN":      req.INN,
			"RegionID": nilIfEmpty(req.RegionID),
			"Code":     nilIfEmpty(req.Code),
			"Phone":    nilIfEmpty(req.Phone),
			"Address":  nilIfEmpty(req.Address),
			"Email":    nilIfEmpty(req.Email),
			"IsActive": req.IsActive,
		})
	if res.Error != nil {
		s.logger.Error("Ошибка обновления покупателя: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка обновления покупателя")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Покупатель не найден")
		return
	}

	s.logger.Info("Обновлен покупатель: %s", buyerID)
	s.handleGetBuyerByID(w, r, buyerID)
}

// handleDeleteBuyer удаляет (деактивирует) покупателя
func (s *Server) handleDeleteBuyer(w http.ResponseWriter, r *http.Request, buyerID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleDeleteBuyer: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := s.database.GORMWith(ctx).
		Model(&models.Buyer{}).
		Where("BuyerID = ?", db.UUIDParam(buyerID)).
		Update("IsActive", false)
	if res.Error != nil {
		s.logger.Error("Ошибка удаления покупателя: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления покупателя")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Покупатель не найден")
		return
	}

	s.logger.Info("Удален покупатель: %s", buyerID)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Покупатель удален",
		"buyer_id": buyerID,
	})
}

// ===============================
// ОБРАБОТЧИКИ ДЛЯ ПОЛЬЗОВАТЕЛЕЙ ПОКУПАТЕЛЕЙ
// ===============================

// handleBuyerUsersRouter роутит запросы к /api/buyer-users
func (s *Server) handleBuyerUsersRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/buyer-users")
	path = strings.TrimPrefix(path, "/")

	// Подпуть {id}/password — установка пароля покупателю
	if parts := strings.Split(path, "/"); len(parts) == 2 && parts[1] == "password" && parts[0] != "" {
		if r.Method == http.MethodPost {
			s.handleSetBuyerUserPassword(w, r, parts[0])
		} else {
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	if path != "" && !strings.Contains(path, "/") {
		switch r.Method {
		case http.MethodGet:
			s.handleGetBuyerUserByID(w, r, path)
		case http.MethodPut:
			s.handleUpdateBuyerUser(w, r, path)
		case http.MethodDelete:
			s.handleDeleteBuyerUser(w, r, path)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetBuyerUsers(w, r)
	case http.MethodPost:
		s.handleCreateBuyerUser(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// handleGetBuyerUsers возвращает список пользователей покупателей
func (s *Server) handleGetBuyerUsers(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerUsers: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerID := r.URL.Query().Get("buyer_id")

	q := s.database.GORMWith(ctx).
		Table("BuyerUser AS bu").
		Select(`CAST(bu.BuyerUserID AS NVARCHAR(50)) AS BuyerUserID,
			CAST(bu.BuyerID AS NVARCHAR(50)) AS BuyerID,
			bu.FullName, bu.Email, bu.Phone, bu.Role,
			bu.IsActive, bu.CreatedAt,
			b.Name AS BuyerName`).
		Joins("INNER JOIN Buyer b ON bu.BuyerID = b.BuyerID").
		Where("bu.IsActive = ?", true)
	if buyerID != "" {
		q = q.Where("bu.BuyerID = ?", db.UUIDParam(buyerID))
	}

	users := []buyerUserWithBuyer{}
	if err := q.Order("bu.FullName").Limit(500).Scan(&users).Error; err != nil {
		s.logger.Error("Ошибка получения пользователей: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения пользователей")
		return
	}
	s.writeJSON(w, http.StatusOK, users)
}

// handleGetBuyerUserByID возвращает пользователя по ID
func (s *Server) handleGetBuyerUserByID(w http.ResponseWriter, r *http.Request, userID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerUserByID: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var u buyerUserWithBuyer
	err := s.database.GORMWith(ctx).
		Table("BuyerUser AS bu").
		Select(`CAST(bu.BuyerUserID AS NVARCHAR(50)) AS BuyerUserID,
			CAST(bu.BuyerID AS NVARCHAR(50)) AS BuyerID,
			bu.FullName, bu.Email, bu.Phone, bu.Role,
			bu.IsActive, bu.CreatedAt,
			b.Name AS BuyerName`).
		Joins("INNER JOIN Buyer b ON bu.BuyerID = b.BuyerID").
		Where("bu.BuyerUserID = ?", db.UUIDParam(userID)).
		Take(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка получения пользователя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения пользователя")
		return
	}
	s.writeJSON(w, http.StatusOK, u)
}

// handleCreateBuyerUser создает нового пользователя покупателя
func (s *Server) handleCreateBuyerUser(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleCreateBuyerUser: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.BuyerUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.BuyerID == "" {
		s.writeError(w, http.StatusBadRequest, "buyer_id обязателен")
		return
	}
	if req.FullName == "" {
		s.writeError(w, http.StatusBadRequest, "full_name обязателен")
		return
	}
	if req.Email == "" {
		s.writeError(w, http.StatusBadRequest, "email обязателен")
		return
	}

	var passwordPtr *string
	if req.Password != nil && *req.Password != "" {
		hashed, hashErr := hashPassword(*req.Password)
		if hashErr != nil {
			s.logger.Error("Ошибка хэширования пароля BuyerUser: %v", hashErr)
			s.writeError(w, http.StatusInternalServerError, "Ошибка обработки пароля")
			return
		}
		passwordPtr = &hashed
	}

	userID := uuid.New().String()
	now := time.Now().UTC()
	bu := models.BuyerUser{
		BuyerUserID: userID,
		BuyerID:     req.BuyerID,
		FullName:    req.FullName,
		Email:       req.Email,
		Phone:       req.Phone,
		Role:        req.Role,
		IsActive:    req.IsActive,
		Password:    passwordPtr,
		CreatedAt:   now,
	}

	// Создаём пользователя вместе с BuyerApplication одной транзакцией.
	// Без активной BuyerApplication пользователь не может оформить заказ
	// (resolveBuyerUser делает INNER JOIN BuyerApplication ... IsActive=1).
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&bu).Error; err != nil {
			return err
		}
		app := models.BuyerApplication{
			BuyerApplicationID: uuid.New().String(),
			BuyerID:            req.BuyerID,
			BuyerUserID:        userID,
			IsActive:           true,
			CreatedAt:          now,
		}
		return tx.Create(&app).Error
	})
	if err != nil {
		s.logger.Error("Ошибка создания пользователя: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания пользователя")
		return
	}

	s.logger.Info("Создан пользователь покупателя: %s - %s (+ BuyerApplication)", userID, req.FullName)
	s.handleGetBuyerUserByID(w, r, userID)
}

// handleUpdateBuyerUser обновляет пользователя покупателя
func (s *Server) handleUpdateBuyerUser(w http.ResponseWriter, r *http.Request, userID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleUpdateBuyerUser: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.BuyerUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}

	res := s.database.GORMWith(ctx).
		Model(&models.BuyerUser{}).
		Where("BuyerUserID = ?", db.UUIDParam(userID)).
		Updates(map[string]interface{}{
			"FullName": req.FullName,
			"Email":    req.Email,
			"Phone":    req.Phone,
			"Role":     req.Role,
			"IsActive": req.IsActive,
		})
	if res.Error != nil {
		s.logger.Error("Ошибка обновления пользователя: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка обновления пользователя")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	s.logger.Info("Обновлен пользователь: %s", userID)
	s.handleGetBuyerUserByID(w, r, userID)
}

// handleDeleteBuyerUser удаляет (деактивирует) пользователя
func (s *Server) handleDeleteBuyerUser(w http.ResponseWriter, r *http.Request, userID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleDeleteBuyerUser: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := s.database.GORMWith(ctx).
		Model(&models.BuyerUser{}).
		Where("BuyerUserID = ?", db.UUIDParam(userID)).
		Update("IsActive", false)
	if res.Error != nil {
		s.logger.Error("Ошибка удаления пользователя: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления пользователя")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	s.logger.Info("Удален пользователь: %s", userID)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Пользователь удален",
		"user_id": userID,
	})
}

// handleSetBuyerUserPassword устанавливает/меняет пароль покупателю.
// Доступно только администратору.
// POST /api/buyer-users/{id}/password
// Body: {"password": "..."}
func (s *Server) handleSetBuyerUserPassword(w http.ResponseWriter, r *http.Request, userID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleSetBuyerUserPassword: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	claims := ClaimsFromContext(r.Context())
	if claims == nil || claims.Role != "admin" {
		s.writeError(w, http.StatusForbidden, "Доступ запрещён")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.Password == "" {
		s.writeError(w, http.StatusBadRequest, "password обязателен")
		return
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		s.logger.Error("Ошибка хэширования пароля BuyerUser %s: %v", userID, err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка обработки пароля")
		return
	}

	res := s.database.GORMWith(ctx).
		Model(&models.BuyerUser{}).
		Where("BuyerUserID = ?", db.UUIDParam(userID)).
		Update("Password", hashed)
	if res.Error != nil {
		s.logger.Error("Ошибка установки пароля BuyerUser %s: %v", userID, res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка установки пароля")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	s.logger.Info("Установлен пароль BuyerUser: %s (admin=%s)", userID, claims.Username)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Пароль установлен",
		"user_id": userID,
	})
}

// ===============================
// ОБРАБОТЧИКИ ДЛЯ АДРЕСОВ ПОКУПАТЕЛЕЙ
// ===============================

// handleBuyerLocationsRouter роутит запросы к /api/buyer-locations
func (s *Server) handleBuyerLocationsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/buyer-locations")
	path = strings.TrimPrefix(path, "/")

	if path != "" && !strings.Contains(path, "/") {
		switch r.Method {
		case http.MethodGet:
			s.handleGetBuyerLocationByID(w, r, path)
		case http.MethodDelete:
			s.handleDeleteBuyerLocation(w, r, path)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetBuyerLocations(w, r)
	case http.MethodPost:
		s.handleCreateBuyerLocation(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// handleGetBuyerLocations возвращает список адресов покупателя
func (s *Server) handleGetBuyerLocations(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerLocations: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	buyerID := r.URL.Query().Get("buyer_id")
	if buyerID == "" {
		s.writeError(w, http.StatusBadRequest, "buyer_id обязателен")
		return
	}

	locations := []locationWithRegion{}
	err := s.database.GORMWith(ctx).
		Table("BuyerLocation AS bl").
		Select(`CAST(bl.BuyerLocationID AS NVARCHAR(50)) AS BuyerLocationID,
			CAST(bl.BuyerID AS NVARCHAR(50)) AS BuyerID,
			bl.Address,
			CAST(bl.RegionID AS NVARCHAR(50)) AS RegionID,
			bl.IsDefault, bl.CreatedAt,
			r.Name AS RegionName`).
		Joins("LEFT JOIN Region r ON bl.RegionID = r.RegionID").
		Where("bl.BuyerID = ?", db.UUIDParam(buyerID)).
		Order("bl.IsDefault DESC, bl.CreatedAt").
		Limit(100).
		Scan(&locations).Error
	if err != nil {
		s.logger.Error("Ошибка получения адресов: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения адресов")
		return
	}
	s.writeJSON(w, http.StatusOK, locations)
}

// handleGetBuyerLocationByID возвращает адрес по ID
func (s *Server) handleGetBuyerLocationByID(w http.ResponseWriter, r *http.Request, locationID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleGetBuyerLocationByID: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var loc locationWithRegion
	err := s.database.GORMWith(ctx).
		Table("BuyerLocation AS bl").
		Select(`CAST(bl.BuyerLocationID AS NVARCHAR(50)) AS BuyerLocationID,
			CAST(bl.BuyerID AS NVARCHAR(50)) AS BuyerID,
			bl.Address,
			CAST(bl.RegionID AS NVARCHAR(50)) AS RegionID,
			bl.IsDefault, bl.CreatedAt,
			r.Name AS RegionName`).
		Joins("LEFT JOIN Region r ON bl.RegionID = r.RegionID").
		Where("bl.BuyerLocationID = ?", db.UUIDParam(locationID)).
		Take(&loc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s.writeError(w, http.StatusNotFound, "Адрес не найден")
		return
	}
	if err != nil {
		s.logger.Error("Ошибка получения адреса: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения адреса")
		return
	}
	s.writeJSON(w, http.StatusOK, loc)
}

// handleCreateBuyerLocation создает новый адрес покупателя
func (s *Server) handleCreateBuyerLocation(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleCreateBuyerLocation: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.BuyerLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	if req.BuyerID == "" {
		s.writeError(w, http.StatusBadRequest, "buyer_id обязателен")
		return
	}
	if req.Address == "" {
		s.writeError(w, http.StatusBadRequest, "address обязателен")
		return
	}

	// Если это адрес по умолчанию — сбрасываем флаг у других адресов того же покупателя.
	if req.IsDefault {
		_ = s.database.GORMWith(ctx).
			Model(&models.BuyerLocation{}).
			Where("BuyerID = ?", db.UUIDParam(req.BuyerID)).
			Update("IsDefault", false).Error
	}

	locationID := uuid.New().String()
	loc := models.BuyerLocation{
		BuyerLocationID: locationID,
		BuyerID:         req.BuyerID,
		Address:         req.Address,
		RegionID:        nilIfEmpty(req.RegionID),
		IsDefault:       req.IsDefault,
		CreatedAt:       time.Now().UTC(),
	}
	if err := s.database.GORMWith(ctx).Create(&loc).Error; err != nil {
		s.logger.Error("Ошибка создания адреса: %v", err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания адреса")
		return
	}

	s.logger.Info("Создан адрес покупателя: %s", locationID)
	s.handleGetBuyerLocationByID(w, r, locationID)
}

// handleDeleteBuyerLocation удаляет адрес покупателя
func (s *Server) handleDeleteBuyerLocation(w http.ResponseWriter, r *http.Request, locationID string) {
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("Паника в handleDeleteBuyerLocation: %v", rec)
			s.writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := s.database.GORMWith(ctx).
		Where("BuyerLocationID = ?", db.UUIDParam(locationID)).
		Delete(&models.BuyerLocation{})
	if res.Error != nil {
		s.logger.Error("Ошибка удаления адреса: %v", res.Error)
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления адреса")
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Адрес не найден")
		return
	}

	s.logger.Info("Удален адрес: %s", locationID)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "Адрес удален",
		"location_id": locationID,
	})
}

// nilIfEmpty возвращает nil, если строка пустая или указатель пустой — иначе сам указатель.
// Используется чтобы пустая строка не уходила в БД как "" вместо NULL для опциональных GUID.
func nilIfEmpty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}
