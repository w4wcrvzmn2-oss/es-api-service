package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/dbfimport"
	"es_api_service/internal/matching"
	"es_api_service/internal/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LindsayBradford/go-dbf/godbf"
	"github.com/google/uuid"
	"github.com/jlaffaye/ftp"
)

// writeJSON отправляет JSON ответ
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError отправляет ошибку в формате JSON
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	if status >= 500 {
		if s.logger != nil {
			s.logger.Error("HTTP %d: %s", status, message)
		}
		genericPrefixes := []string{
			"Ошибка получения", "Ошибка создания", "Ошибка обновления",
			"Ошибка удаления", "Ошибка сохранения", "Ошибка обработки",
			"Ошибка проверки", "Ошибка переключения", "Ошибка деактивации",
			"Ошибка начала транзакции", "Ошибка фиксации транзакции",
		}
		for _, prefix := range genericPrefixes {
			if strings.HasPrefix(message, prefix) {
				idx := strings.Index(message, ":")
				if idx > 0 {
					message = message[:idx]
				}
				break
			}
		}
	}
	s.sendJSONError(w, status, message)
}

// handleGetSuppliers возвращает список поставщиков
func (s *Server) handleGetSuppliers(w http.ResponseWriter, r *http.Request) {
	// Обработка паник
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleGetSuppliers: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	if s.logger != nil {
		s.logger.Info("Запрос списка поставщиков от %s", r.RemoteAddr)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Проверяем наличие подключения к БД
	if s.database == nil {
		if s.logger != nil {
			s.logger.Error("База данных не инициализирована")
		}
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	query := `
		SELECT TOP 500 CAST(s.SupplierID AS NVARCHAR(50)) AS SupplierID, s.Name, s.Address, s.Contacts, s.INN,
			s.ContractNumber, s.Login,
			s.IsActive, s.CreatedAt, s.UpdatedAt,
			(SELECT COUNT(*) FROM SupplierRegion sr WHERE sr.SupplierID = s.SupplierID AND sr.IsActive = 1) AS RegionsCount
		FROM Supplier s
		ORDER BY s.Name
	`

	if s.logger != nil {
		s.logger.Info("Начинаем выполнение SQL запроса для получения поставщиков")
	}

	// Выполняем запрос с таймаутом
	rows, err := s.database.QueryContext(ctx, query)
	if s.logger != nil {
		if err != nil {
			s.logger.Error("SQL запрос завершился с ошибкой")
		} else {
			s.logger.Info("SQL запрос выполнен успешно, начинаем чтение результатов")
		}
	}
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL запроса для поставщиков: %v", err)
			s.logger.Error("SQL: %s", query)

			// Проверяем, возможно таблица не существует
			if strings.Contains(err.Error(), "Invalid object name") ||
				strings.Contains(err.Error(), "таблицы") ||
				strings.Contains(err.Error(), "object name") ||
				strings.Contains(err.Error(), "does not exist") {
				s.writeError(w, http.StatusInternalServerError, "Таблица Supplier не найдена в базе данных")
				return
			}
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения поставщиков: %v", err))
		return
	}
	defer rows.Close()

	if s.logger != nil {
		s.logger.Info("Начинаем итерацию по строкам результатов")
	}

	type SupplierWithRegions struct {
		models.Supplier
		RegionsCount int `json:"regions_count"`
	}

	var suppliers []SupplierWithRegions
	count := 0
	for rows.Next() {
		count++
		if s.logger != nil && count%10 == 0 {
			s.logger.Debug("Обработано поставщиков: %d", count)
		}
		var supplier SupplierWithRegions
		var address, contacts, inn, contractNumber, login sql.NullString
		var createdAt, updatedAt time.Time

		err := rows.Scan(&supplier.SupplierID, &supplier.Name, &address, &contacts, &inn,
			&contractNumber, &login,
			&supplier.IsActive, &createdAt, &updatedAt, &supplier.RegionsCount)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования поставщика: %v", err)
			}
			continue
		}

		if address.Valid {
			supplier.Address = &address.String
		}
		if contacts.Valid {
			supplier.Contacts = &contacts.String
		}
		if inn.Valid {
			supplier.INN = &inn.String
		}
		if contractNumber.Valid {
			supplier.ContractNumber = &contractNumber.String
		}
		if login.Valid {
			supplier.Login = &login.String
		}
		supplier.CreatedAt = createdAt
		supplier.UpdatedAt = updatedAt

		suppliers = append(suppliers, supplier)
	}

	// Проверяем ошибки после итерации
	if err := rows.Err(); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка при итерации строк поставщиков: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обработки данных: %v", err))
		return
	}

	if s.logger != nil {
		s.logger.Info("Успешно получено поставщиков: %d", len(suppliers))
		s.logger.Info("Подготавливаем JSON ответ")
	}

	// Явно устанавливаем заголовки перед отправкой
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	if s.logger != nil {
		s.logger.Info("Отправляем JSON ответ клиенту")
	}

	s.writeJSON(w, http.StatusOK, suppliers)

	if s.logger != nil {
		s.logger.Info("Ответ успешно отправлен клиенту")
	}
}

// handleCreateSupplier создает нового поставщика
func (s *Server) handleCreateSupplier(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.SupplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка парсинга запроса создания поставщика: %v", err)
		}
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Название поставщика обязательно")
		return
	}

	if s.logger != nil {
		s.logger.Info("Создание поставщика: %s", req.Name)
	}

	supplierID := uuid.New().String()
	var passwordValue interface{}
	if req.Password != nil && *req.Password != "" {
		hashedPassword, err := hashPassword(*req.Password)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Ошибка подготовки пароля")
			return
		}
		passwordValue = hashedPassword
	}
	query := `
		INSERT INTO Supplier (SupplierID, Name, Address, Contacts, INN, ContractNumber, Login, Password, IsActive, CreatedAt, UpdatedAt)
		VALUES (CAST(@supplierID AS UNIQUEIDENTIFIER), @name, @address, @contacts, @inn, @contractNumber, @login, @password, @isActive, @createdAt, @updatedAt)
	`

	now := time.Now()
	_, err := s.database.ExecContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("name", req.Name),
		sql.Named("address", req.Address),
		sql.Named("contacts", req.Contacts),
		sql.Named("inn", req.INN),
		sql.Named("contractNumber", req.ContractNumber),
		sql.Named("login", req.Login),
		sql.Named("password", passwordValue),
		sql.Named("isActive", req.IsActive),
		sql.Named("createdAt", now),
		sql.Named("updatedAt", now))
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL при создании поставщика: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания поставщика: %v", err))
		return
	}

	if s.logger != nil {
		s.logger.Info("Поставщик успешно создан: %s (ID: %s)", req.Name, supplierID)
	}

	if len(req.RegionIDs) > 0 {
		if err := s.saveSupplierRegions(ctx, supplierID, req.RegionIDs); err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка сохранения регионов поставщика: %v", err)
			}
		}
	}

	s.writeJSON(w, http.StatusCreated, map[string]string{"supplier_id": supplierID})
}

// handleSuppliersRouter маршрутизирует запросы /api/suppliers/{id}
func (s *Server) handleSuppliersRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/suppliers/")

	if path == "create" {
		s.handleCreateSupplier(w, r)
		return
	}

	if path == "" {
		return
	}

	parts := strings.Split(path, "/")
	supplierID := parts[0]
	if _, err := uuid.Parse(supplierID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат ID")
		return
	}

	if len(parts) >= 2 && parts[1] == "regions" {
		s.handleGetSupplierRegions(w, r, supplierID)
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleUpdateSupplier(w, r, supplierID)
	case http.MethodDelete:
		s.handleDeleteSupplier(w, r, supplierID)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// handleUpdateSupplier обновляет поставщика
func (s *Server) handleUpdateSupplier(w http.ResponseWriter, r *http.Request, supplierID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.SupplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Название поставщика обязательно")
		return
	}

	var passwordValue interface{}
	if req.Password != nil && *req.Password != "" {
		hashedPassword, err := hashPassword(*req.Password)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Ошибка подготовки пароля")
			return
		}
		passwordValue = hashedPassword
	}

	query := `
		UPDATE Supplier SET
			Name = @name,
			Address = @address,
			Contacts = @contacts,
			INN = @inn,
			ContractNumber = @contractNumber,
			Login = @login,
			Password = CASE WHEN @password IS NULL OR @password = '' THEN Password ELSE @password END,
			IsActive = @isActive,
			UpdatedAt = @updatedAt
		WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
	`

	result, err := s.database.ExecContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("name", req.Name),
		sql.Named("address", req.Address),
		sql.Named("contacts", req.Contacts),
		sql.Named("inn", req.INN),
		sql.Named("contractNumber", req.ContractNumber),
		sql.Named("login", req.Login),
		sql.Named("password", passwordValue),
		sql.Named("isActive", req.IsActive),
		sql.Named("updatedAt", time.Now()))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Поставщик не найден")
		return
	}

	var cleanedItems []orphanedRegionInfo
	if req.RegionIDs != nil {
		if err := s.saveSupplierRegions(ctx, supplierID, req.RegionIDs); err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка сохранения регионов поставщика: %v", err)
			}
			s.writeError(w, http.StatusInternalServerError, "Ошибка сохранения регионов")
			return
		}

		cleaned, err := s.cleanOrphanedPriceListRegions(ctx, supplierID)
		if err != nil && s.logger != nil {
			s.logger.Error("Ошибка каскадной очистки регионов прайсов: %v", err)
		}
		cleanedItems = cleaned
	}

	if s.logger != nil {
		s.logger.Info("Поставщик обновлён: %s (ID: %s)", req.Name, supplierID)
	}

	response := map[string]interface{}{
		"status":      "updated",
		"supplier_id": supplierID,
	}
	if len(cleanedItems) > 0 {
		response["cleaned_regions"] = cleanedItems
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleDeleteSupplier удаляет поставщика
func (s *Server) handleDeleteSupplier(w http.ResponseWriter, r *http.Request, supplierID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	query := `DELETE FROM Supplier WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)`
	result, err := s.database.ExecContext(ctx, query, sql.Named("supplierID", supplierID))
	if err != nil {
		if strings.Contains(err.Error(), "REFERENCE") || strings.Contains(err.Error(), "foreign key") {
			s.writeError(w, http.StatusConflict, "Невозможно удалить: поставщик используется в других записях")
			return
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка удаления: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Поставщик не найден")
		return
	}

	if s.logger != nil {
		s.logger.Info("Поставщик удалён: %s", supplierID)
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGetSupplierRegions возвращает регионы поставщика из SupplierRegion
func (s *Server) handleGetSupplierRegions(w http.ResponseWriter, r *http.Request, supplierID string) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	query := `
		SELECT TOP 200
			CAST(sr.RegionID AS NVARCHAR(50)) AS RegionID,
			r.Name AS RegionName,
			r.Code AS RegionCode,
			sr.IsActive
		FROM SupplierRegion sr
		INNER JOIN Region r ON sr.RegionID = r.RegionID
		WHERE sr.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
		  AND sr.IsActive = 1
		ORDER BY r.Name
	`

	rows, err := s.database.QueryContext(ctx, query, sql.Named("supplierID", supplierID))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка получения регионов поставщика")
		return
	}
	defer rows.Close()

	type RegionView struct {
		RegionID   string `json:"region_id"`
		RegionName string `json:"region_name"`
		RegionCode string `json:"region_code"`
		IsActive   bool   `json:"is_active"`
	}

	var regions []RegionView
	for rows.Next() {
		var rv RegionView
		var code sql.NullString
		if err := rows.Scan(&rv.RegionID, &rv.RegionName, &code, &rv.IsActive); err != nil {
			continue
		}
		if code.Valid {
			rv.RegionCode = code.String
		}
		regions = append(regions, rv)
	}

	if regions == nil {
		regions = []RegionView{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"regions": regions,
		"total":   len(regions),
	})
}

// saveSupplierRegions сохраняет регионы поставщика в транзакции (delete+insert)
func (s *Server) saveSupplierRegions(ctx context.Context, supplierID string, regionIDs []string) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	delQuery := `DELETE FROM SupplierRegion WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)`
	if _, err := tx.ExecContext(ctx, delQuery, sql.Named("supplierID", supplierID)); err != nil {
		return err
	}

	insertQuery := `
		INSERT INTO SupplierRegion (SupplierID, RegionID, IsActive, CreatedAt)
		VALUES (CAST(@supplierID AS UNIQUEIDENTIFIER), CAST(@regionID AS UNIQUEIDENTIFIER), 1, GETUTCDATE())
	`
	for _, regionID := range regionIDs {
		if regionID != "" {
			if _, err := tx.ExecContext(ctx, insertQuery,
				sql.Named("supplierID", supplierID),
				sql.Named("regionID", regionID),
			); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// orphanedRegionInfo описывает одну «осиротевшую» связку прайс-регион
type orphanedRegionInfo struct {
	PriceName  string `json:"price_name"`
	RegionName string `json:"region_name"`
}

// cleanOrphanedPriceListRegions удаляет из прайсов поставщика регионы, которых нет в SupplierRegion.
func (s *Server) cleanOrphanedPriceListRegions(ctx context.Context, supplierID string) ([]orphanedRegionInfo, error) {
	query := `
		SELECT pl.Name, r.Name
		FROM PriceListRegion plr
		INNER JOIN PriceList pl ON plr.PriceListID = pl.PriceListID
		INNER JOIN Region r ON plr.RegionID = r.RegionID
		WHERE pl.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
		  AND pl.IsActive = 1
		  AND plr.RegionID NOT IN (
			SELECT RegionID FROM SupplierRegion
			WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER) AND IsActive = 1
		  )
		ORDER BY pl.Name, r.Name
	`
	rows, err := s.database.QueryContext(ctx, query, sql.Named("supplierID", supplierID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []orphanedRegionInfo
	for rows.Next() {
		var item orphanedRegionInfo
		if rows.Scan(&item.PriceName, &item.RegionName) == nil {
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return nil, nil
	}

	deleteQuery := `
		DELETE plr FROM PriceListRegion plr
		INNER JOIN PriceList pl ON plr.PriceListID = pl.PriceListID
		WHERE pl.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
		  AND plr.RegionID NOT IN (
			SELECT RegionID FROM SupplierRegion
			WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER) AND IsActive = 1
		  )
	`
	_, err = s.database.ExecContext(ctx, deleteQuery, sql.Named("supplierID", supplierID))
	if err != nil {
		return items, err
	}

	if s.logger != nil {
		s.logger.Info("Каскадная очистка: удалено %d связок прайс-регион у поставщика %s", len(items), supplierID)
	}

	return items, nil
}

// handleGetImportPoints возвращает список точек импорта
func (s *Server) handleGetImportPoints(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	supplierID := r.URL.Query().Get("supplier_id")
	query := `
		SELECT TOP 500 CAST(ip.ImportPointID AS NVARCHAR(50)) AS ImportPointID, 
		       CAST(ip.SupplierID AS NVARCHAR(50)) AS SupplierID, 
		       s.Name AS SupplierName,
		       ip.Name, ip.Description, ip.SourceType, ip.DBFFilePath, ip.SourceFilePath,
		       ip.FtpHost, ip.FtpPort, ip.FtpUser, ip.FtpPassword, ip.FtpRemotePath,
		       ip.IsActive, ip.CreatedAt, ip.UpdatedAt
		FROM ImportPoint ip
		LEFT JOIN Supplier s ON ip.SupplierID = s.SupplierID
	`
	var args []interface{}

	if supplierID != "" {
		query += " WHERE ip.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)"
		args = append(args, sql.Named("supplierID", supplierID))
	}
	query += " ORDER BY ip.Name"

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		errorMsg := fmt.Sprintf("Ошибка получения точек импорта: %v", err)
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL при получении точек импорта: %v", err)
			s.logger.Error("SQL запрос: %s", query)

			if strings.Contains(err.Error(), "Invalid object name") ||
				strings.Contains(err.Error(), "таблицы") ||
				strings.Contains(err.Error(), "object name") {
				errorMsg = "Таблица ImportPoint не найдена в базе данных. Выполните SQL скрипт create_import_tables.sql"
			}
		}
		s.writeError(w, http.StatusInternalServerError, errorMsg)
		return
	}
	defer rows.Close()

	var importPoints []models.ImportPoint
	for rows.Next() {
		var ip models.ImportPoint
		var supplierID, supplierName, description, sourceType sql.NullString
		var dbfFilePath, sourceFilePath sql.NullString
		var ftpHost, ftpUser, ftpPassword, ftpRemotePath sql.NullString
		var ftpPort sql.NullInt32
		var createdAt, updatedAt time.Time

		err := rows.Scan(&ip.ImportPointID, &supplierID, &supplierName,
			&ip.Name, &description, &sourceType, &dbfFilePath, &sourceFilePath,
			&ftpHost, &ftpPort, &ftpUser, &ftpPassword, &ftpRemotePath,
			&ip.IsActive, &createdAt, &updatedAt)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования строки точки импорта: %v", err)
			}
			continue
		}

		if supplierID.Valid {
			ip.SupplierID = &supplierID.String
		}
		if supplierName.Valid {
			ip.SupplierName = &supplierName.String
		}
		if description.Valid {
			ip.Description = &description.String
		}
		ip.SourceType = "local"
		if sourceType.Valid && sourceType.String != "" {
			ip.SourceType = sourceType.String
		}
		if dbfFilePath.Valid {
			ip.DBFFilePath = &dbfFilePath.String
		}
		if sourceFilePath.Valid {
			ip.SourceFilePath = &sourceFilePath.String
		}
		if ftpHost.Valid {
			ip.FtpHost = &ftpHost.String
		}
		if ftpPort.Valid {
			port := int(ftpPort.Int32)
			ip.FtpPort = &port
		}
		if ftpUser.Valid {
			ip.FtpUser = &ftpUser.String
		}
		if ftpPassword.Valid {
			ip.FtpPassword = &ftpPassword.String
		}
		if ftpRemotePath.Valid {
			ip.FtpRemotePath = &ftpRemotePath.String
		}
		ip.CreatedAt = createdAt
		ip.UpdatedAt = updatedAt

		importPoints = append(importPoints, ip)
	}

	if importPoints == nil {
		importPoints = []models.ImportPoint{}
	}

	s.writeJSON(w, http.StatusOK, importPoints)
}

// handleCreateImportPoint создает новую точку импорта
func (s *Server) handleCreateImportPoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.ImportPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка парсинга запроса создания точки импорта: %v", err)
		}
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Название точки импорта обязательно")
		return
	}

	if req.SourceType == "" {
		req.SourceType = "local"
	}
	if req.SourceType != "local" && req.SourceType != "ftp" {
		s.writeError(w, http.StatusBadRequest, "SourceType должен быть 'local' или 'ftp'")
		return
	}

	if req.SourceType == "local" && (req.SourceFilePath == nil || *req.SourceFilePath == "") {
		s.writeError(w, http.StatusBadRequest, "Для типа 'local' необходимо указать путь к папке")
		return
	}
	if req.SourceType == "ftp" && (req.FtpHost == nil || *req.FtpHost == "") {
		s.writeError(w, http.StatusBadRequest, "Для типа 'ftp' необходимо указать FTP-хост")
		return
	}

	if s.logger != nil {
		s.logger.Info("Создание точки импорта: %s (тип: %s)", req.Name, req.SourceType)
	}

	importPointID := uuid.New().String()
	query := `
		INSERT INTO ImportPoint (ImportPointID, Name, Description, SourceType, SourceFilePath,
		                         FtpHost, FtpPort, FtpUser, FtpPassword, FtpRemotePath,
		                         IsActive, CreatedAt, UpdatedAt)
		VALUES (CAST(@importPointID AS UNIQUEIDENTIFIER), @name, @description, @sourceType, @sourceFilePath,
		        @ftpHost, @ftpPort, @ftpUser, @ftpPassword, @ftpRemotePath,
		        @isActive, @createdAt, @updatedAt)
	`

	now := time.Now()

	result, err := s.database.ExecContext(ctx, query,
		sql.Named("importPointID", importPointID),
		sql.Named("name", req.Name),
		sql.Named("description", req.Description),
		sql.Named("sourceType", req.SourceType),
		sql.Named("sourceFilePath", req.SourceFilePath),
		sql.Named("ftpHost", req.FtpHost),
		sql.Named("ftpPort", req.FtpPort),
		sql.Named("ftpUser", req.FtpUser),
		sql.Named("ftpPassword", req.FtpPassword),
		sql.Named("ftpRemotePath", req.FtpRemotePath),
		sql.Named("isActive", req.IsActive),
		sql.Named("createdAt", now),
		sql.Named("updatedAt", now))
	if err != nil {
		errorMsg := fmt.Sprintf("Ошибка создания точки импорта: %v", err)
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL при создании точки импорта: %v", err)

			if strings.Contains(err.Error(), "Invalid object name") ||
				strings.Contains(err.Error(), "object name") {
				errorMsg = "Таблица ImportPoint не найдена. Выполните SQL скрипт migrate_import_point_v2.sql"
			}
		}
		s.writeError(w, http.StatusInternalServerError, errorMsg)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if s.logger != nil && rowsAffected == 0 {
		s.logger.Warn("Точка импорта не была создана (0 строк затронуто)")
	}

	if s.logger != nil {
		s.logger.Info("Точка импорта успешно создана: %s (ID: %s)", req.Name, importPointID)
	}

	s.writeJSON(w, http.StatusCreated, map[string]string{"import_point_id": importPointID})
}

// handleImportPointsRouter маршрутизирует запросы /api/import-points/{id}
func (s *Server) handleImportPointsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/import-points/")

	if path == "create" {
		s.handleCreateImportPoint(w, r)
		return
	}

	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			s.handleGetImportPoints(w, r)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	parts := strings.Split(path, "/")
	pointID := parts[0]
	if _, err := uuid.Parse(pointID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат ID")
		return
	}

	if len(parts) > 1 && parts[1] == "analyze" {
		s.handleAnalyzeImportPoint(w, r, pointID)
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleUpdateImportPoint(w, r, pointID)
	case http.MethodDelete:
		s.handleDeleteImportPoint(w, r, pointID)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// handleUpdateImportPoint обновляет точку импорта
func (s *Server) handleUpdateImportPoint(w http.ResponseWriter, r *http.Request, pointID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req models.ImportPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Название точки импорта обязательно")
		return
	}

	if req.SourceType == "" {
		req.SourceType = "local"
	}
	if req.SourceType != "local" && req.SourceType != "ftp" {
		s.writeError(w, http.StatusBadRequest, "SourceType должен быть 'local' или 'ftp'")
		return
	}

	query := `
		UPDATE ImportPoint SET
			Name = @name,
			Description = @description,
			SourceType = @sourceType,
			SourceFilePath = @sourceFilePath,
			FtpHost = @ftpHost,
			FtpPort = @ftpPort,
			FtpUser = @ftpUser,
			FtpPassword = @ftpPassword,
			FtpRemotePath = @ftpRemotePath,
			IsActive = @isActive,
			UpdatedAt = @updatedAt
		WHERE ImportPointID = CAST(@pointID AS UNIQUEIDENTIFIER)
	`

	result, err := s.database.ExecContext(ctx, query,
		sql.Named("pointID", pointID),
		sql.Named("name", req.Name),
		sql.Named("description", req.Description),
		sql.Named("sourceType", req.SourceType),
		sql.Named("sourceFilePath", req.SourceFilePath),
		sql.Named("ftpHost", req.FtpHost),
		sql.Named("ftpPort", req.FtpPort),
		sql.Named("ftpUser", req.FtpUser),
		sql.Named("ftpPassword", req.FtpPassword),
		sql.Named("ftpRemotePath", req.FtpRemotePath),
		sql.Named("isActive", req.IsActive),
		sql.Named("updatedAt", time.Now()))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления: %v", err))
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Точка импорта не найдена")
		return
	}

	if s.logger != nil {
		s.logger.Info("Точка импорта обновлена: %s (ID: %s)", req.Name, pointID)
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "import_point_id": pointID})
}

// handleDeleteImportPoint удаляет точку импорта
func (s *Server) handleDeleteImportPoint(w http.ResponseWriter, r *http.Request, pointID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	delMappings := `DELETE FROM DBFFieldMapping WHERE ImportPointID = CAST(@pointID AS UNIQUEIDENTIFIER)`
	if _, err := tx.ExecContext(ctx, delMappings, sql.Named("pointID", pointID)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления маппингов")
		return
	}

	query := `DELETE FROM ImportPoint WHERE ImportPointID = CAST(@pointID AS UNIQUEIDENTIFIER)`
	result, err := tx.ExecContext(ctx, query, sql.Named("pointID", pointID))
	if err != nil {
		if strings.Contains(err.Error(), "REFERENCE") || strings.Contains(err.Error(), "foreign key") {
			s.writeError(w, http.StatusConflict, "Невозможно удалить: точка импорта используется в прайс-листах или импортах")
			return
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления точки импорта")
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Точка импорта не найдена")
		return
	}

	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка фиксации транзакции")
		return
	}

	if s.logger != nil {
		s.logger.Info("Точка импорта удалена: %s", pointID)
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGetFieldMappings возвращает маппинг полей для точки импорта
func (s *Server) handleGetFieldMappings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	importPointID := r.URL.Query().Get("import_point_id")
	if importPointID == "" {
		s.writeError(w, http.StatusBadRequest, "Требуется параметр import_point_id")
		return
	}

	query := `
		SELECT TOP 200 CAST(MappingID AS NVARCHAR(50)) AS MappingID, 
		       CAST(ImportPointID AS NVARCHAR(50)) AS ImportPointID, 
		       DBFFieldName, TargetFieldName,
		       DataType, IsRequired, DefaultValue, TransformRule, DisplayOrder,
		       CreatedAt, UpdatedAt
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
		ORDER BY DisplayOrder, DBFFieldName
	`

	rows, err := s.database.QueryContext(ctx, query, sql.Named("importPointID", importPointID))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения маппинга: %v", err))
		return
	}
	defer rows.Close()

	var mappings []models.DBFFieldMapping
	for rows.Next() {
		var m models.DBFFieldMapping
		var defaultValue, transformRule sql.NullString
		var createdAt, updatedAt time.Time

		err := rows.Scan(&m.MappingID, &m.ImportPointID, &m.DBFFieldName, &m.TargetFieldName,
			&m.DataType, &m.IsRequired, &defaultValue, &transformRule, &m.DisplayOrder,
			&createdAt, &updatedAt)
		if err != nil {
			continue
		}

		if defaultValue.Valid {
			m.DefaultValue = &defaultValue.String
		}
		if transformRule.Valid {
			m.TransformRule = &transformRule.String
		}
		m.CreatedAt = createdAt
		m.UpdatedAt = updatedAt

		mappings = append(mappings, m)
	}

	s.writeJSON(w, http.StatusOK, mappings)
}

// handleGetInvoiceImports возвращает список импортов прайсов
func (s *Server) handleGetInvoiceImports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	importPointID := r.URL.Query().Get("import_point_id")
	supplierID := r.URL.Query().Get("supplier_id")

	var query string
	var args []interface{}

	if importPointID != "" {
		query = `
			SELECT TOP 200
				CAST(InvoiceImportID AS NVARCHAR(50)) AS InvoiceImportID,
				CAST(ImportPointID AS NVARCHAR(50)) AS ImportPointID,
				FileName,
				FilePath,
				FileSize,
				RecordsTotal,
				RecordsProcessed,
				RecordsSkipped,
				RecordsError,
				ImportStatus,
				ErrorMessage,
				StartedAt,
				CompletedAt,
				CreatedAt
			FROM InvoiceImport
			WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
			ORDER BY CreatedAt DESC
		`
		args = []interface{}{sql.Named("importPointID", importPointID)}
	} else if supplierID != "" {
		query = `
			SELECT TOP 200
				CAST(ii.InvoiceImportID AS NVARCHAR(50)) AS InvoiceImportID,
				CAST(ii.ImportPointID AS NVARCHAR(50)) AS ImportPointID,
				ii.FileName,
				ii.FilePath,
				ii.FileSize,
				ii.RecordsTotal,
				ii.RecordsProcessed,
				ii.RecordsSkipped,
				ii.RecordsError,
				ii.ImportStatus,
				ii.ErrorMessage,
				ii.StartedAt,
				ii.CompletedAt,
				ii.CreatedAt
			FROM InvoiceImport ii
			INNER JOIN ImportPoint ip ON ii.ImportPointID = ip.ImportPointID
			WHERE ip.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
			ORDER BY ii.CreatedAt DESC
		`
		args = []interface{}{sql.Named("supplierID", supplierID)}
	} else {
		s.writeError(w, http.StatusBadRequest, "Не указан import_point_id или supplier_id")
		return
	}

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения импортов: %v", err))
		return
	}
	defer rows.Close()

	type InvoiceImportView struct {
		InvoiceImportID  string  `json:"invoice_import_id"`
		ImportPointID    string  `json:"import_point_id"`
		FileName         string  `json:"file_name"`
		FilePath         string  `json:"file_path"`
		FileSize         *int64  `json:"file_size,omitempty"`
		RecordsTotal     int     `json:"records_total"`
		RecordsProcessed int     `json:"records_processed"`
		RecordsSkipped   int     `json:"records_skipped"`
		RecordsError     int     `json:"records_error"`
		ImportStatus     string  `json:"import_status"`
		ErrorMessage     *string `json:"error_message,omitempty"`
		StartedAt        *string `json:"started_at,omitempty"`
		CompletedAt      *string `json:"completed_at,omitempty"`
		CreatedAt        string  `json:"created_at"`
	}

	var imports []InvoiceImportView
	for rows.Next() {
		var imp InvoiceImportView
		var fileSize sql.NullInt64
		var errorMessage sql.NullString
		var startedAt, completedAt, createdAt sql.NullTime

		err := rows.Scan(
			&imp.InvoiceImportID,
			&imp.ImportPointID,
			&imp.FileName,
			&imp.FilePath,
			&fileSize,
			&imp.RecordsTotal,
			&imp.RecordsProcessed,
			&imp.RecordsSkipped,
			&imp.RecordsError,
			&imp.ImportStatus,
			&errorMessage,
			&startedAt,
			&completedAt,
			&createdAt,
		)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования InvoiceImport: %v", err)
			}
			continue
		}

		if fileSize.Valid {
			imp.FileSize = &fileSize.Int64
		}
		if errorMessage.Valid {
			imp.ErrorMessage = &errorMessage.String
		}
		if startedAt.Valid {
			imp.StartedAt = stringPtr(startedAt.Time.Format("2006-01-02 15:04:05"))
		}
		if completedAt.Valid {
			imp.CompletedAt = stringPtr(completedAt.Time.Format("2006-01-02 15:04:05"))
		}
		if createdAt.Valid {
			imp.CreatedAt = createdAt.Time.Format("2006-01-02 15:04:05")
		}

		imports = append(imports, imp)
	}

	if imports == nil {
		imports = []InvoiceImportView{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"imports": imports,
		"total":   len(imports),
	})
}

// handleSaveFieldMapping сохраняет маппинг полей
func (s *Server) handleSaveFieldMapping(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	importPointID := r.URL.Query().Get("import_point_id")
	if importPointID == "" {
		s.writeError(w, http.StatusBadRequest, "Требуется параметр import_point_id")
		return
	}

	var req models.DBFFieldMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	// Валидация обязательных полей
	if req.DBFFieldName == "" {
		s.writeError(w, http.StatusBadRequest, "DBFFieldName не может быть пустым")
		return
	}
	if req.TargetFieldName == "" {
		s.writeError(w, http.StatusBadRequest, "TargetFieldName не может быть пустым")
		return
	}
	if req.DataType == "" {
		s.writeError(w, http.StatusBadRequest, "DataType не может быть пустым")
		return
	}

	// Валидация TargetFieldName - проверяем, что это допустимое поле
	validTargetFields := map[string]bool{
		"item_code":      true,
		"item_name":      true,
		"barcode":        true,
		"price":          true,
		"quantity":       true,
		"invoice_number": true,
		"invoice_date":   true,
		"batch_number":   true,
		"expiry_date":    true,
		"manufacturer":   true,
		"country":        true,
	}
	if !validTargetFields[req.TargetFieldName] {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимое целевое поле: %s", req.TargetFieldName))
		return
	}

	// Валидация DataType
	validDataTypes := map[string]bool{
		"NVARCHAR": true,
		"INT":      true,
		"DECIMAL":  true,
		"FLOAT":    true,
		"DATE":     true,
		"DATETIME": true,
	}
	if !validDataTypes[req.DataType] {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый тип данных: %s", req.DataType))
		return
	}

	// Используем MERGE (UPSERT) для обновления или вставки
	// Это предотвращает ошибку дублирования ключа при повторном сохранении
	query := `
		MERGE DBFFieldMapping AS target
		USING (SELECT CAST(@importPointID AS UNIQUEIDENTIFIER) AS ImportPointID, @dbfFieldName AS DBFFieldName) AS source
		ON target.ImportPointID = source.ImportPointID
		   AND target.DBFFieldName = source.DBFFieldName
		WHEN MATCHED THEN
			UPDATE SET
				TargetFieldName = @targetFieldName,
				DataType = @dataType,
				IsRequired = @isRequired,
				DefaultValue = @defaultValue,
				TransformRule = @transformRule,
				DisplayOrder = @displayOrder,
				UpdatedAt = @updatedAt
		WHEN NOT MATCHED THEN
			INSERT (MappingID, ImportPointID, DBFFieldName, TargetFieldName, DataType,
			        IsRequired, DefaultValue, TransformRule, DisplayOrder, CreatedAt, UpdatedAt)
			VALUES (NEWID(), CAST(@importPointID AS UNIQUEIDENTIFIER), @dbfFieldName, @targetFieldName, @dataType,
			        @isRequired, @defaultValue, @transformRule, @displayOrder, @createdAt, @updatedAt);
		
		-- Получаем MappingID для ответа
		SELECT CAST(MappingID AS NVARCHAR(50)) AS MappingID
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
		  AND DBFFieldName = @dbfFieldName;
	`

	now := time.Now()
	var mappingID string
	err := s.database.QueryRowContext(ctx, query,
		sql.Named("importPointID", importPointID),
		sql.Named("dbfFieldName", req.DBFFieldName),
		sql.Named("targetFieldName", req.TargetFieldName),
		sql.Named("dataType", req.DataType),
		sql.Named("isRequired", req.IsRequired),
		sql.Named("defaultValue", req.DefaultValue),
		sql.Named("transformRule", req.TransformRule),
		sql.Named("displayOrder", req.DisplayOrder),
		sql.Named("createdAt", now),
		sql.Named("updatedAt", now)).Scan(&mappingID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка сохранения маппинга: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"mapping_id": mappingID})
}

// handleSaveAllFieldMappings атомарно заменяет все маппинги для точки импорта
func (s *Server) handleSaveAllFieldMappings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	importPointID := r.URL.Query().Get("import_point_id")
	if importPointID == "" {
		s.writeError(w, http.StatusBadRequest, "Требуется параметр import_point_id")
		return
	}

	var body struct {
		Mappings []models.DBFFieldMappingRequest `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга: %v", err))
		return
	}

	if len(body.Mappings) == 0 {
		s.writeError(w, http.StatusBadRequest, "Список маппингов пуст")
		return
	}

	validTargetFields := map[string]bool{
		"item_code": true, "item_name": true, "barcode": true, "price": true,
		"quantity": true, "invoice_number": true, "invoice_date": true,
		"batch_number": true, "expiry_date": true, "manufacturer": true, "country": true,
	}
	validDataTypes := map[string]bool{
		"NVARCHAR": true, "INT": true, "DECIMAL": true, "FLOAT": true, "DATE": true, "DATETIME": true,
	}

	usedTargets := make(map[string]bool)
	for _, m := range body.Mappings {
		if m.DBFFieldName == "" || m.TargetFieldName == "" || m.DataType == "" {
			s.writeError(w, http.StatusBadRequest, "Все маппинги должны содержать dbf_field_name, target_field_name, data_type")
			return
		}
		if !validTargetFields[m.TargetFieldName] {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимое целевое поле: %s", m.TargetFieldName))
			return
		}
		if !validDataTypes[m.DataType] {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый тип данных: %s", m.DataType))
			return
		}
		if usedTargets[m.TargetFieldName] {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Дубликат целевого поля: %s", m.TargetFieldName))
			return
		}
		usedTargets[m.TargetFieldName] = true
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`DELETE FROM DBFFieldMapping WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)`,
		sql.Named("importPointID", importPointID))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка очистки старых маппингов")
		return
	}

	now := time.Now()
	for i, m := range body.Mappings {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO DBFFieldMapping
				(MappingID, ImportPointID, DBFFieldName, TargetFieldName, DataType,
				 IsRequired, DefaultValue, TransformRule, DisplayOrder, CreatedAt, UpdatedAt)
			VALUES
				(NEWID(), CAST(@importPointID AS UNIQUEIDENTIFIER), @dbfFieldName, @targetFieldName, @dataType,
				 @isRequired, @defaultValue, @transformRule, @displayOrder, @createdAt, @updatedAt)`,
			sql.Named("importPointID", importPointID),
			sql.Named("dbfFieldName", m.DBFFieldName),
			sql.Named("targetFieldName", m.TargetFieldName),
			sql.Named("dataType", m.DataType),
			sql.Named("isRequired", m.IsRequired),
			sql.Named("defaultValue", m.DefaultValue),
			sql.Named("transformRule", m.TransformRule),
			sql.Named("displayOrder", i),
			sql.Named("createdAt", now),
			sql.Named("updatedAt", now))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка вставки маппинга %s: %v", m.DBFFieldName, err))
			return
		}
	}

	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка фиксации транзакции")
		return
	}

	if s.logger != nil {
		s.logger.Info("Маппинг полностью пересохранён для ImportPoint %s: %d записей", importPointID, len(body.Mappings))
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "saved",
		"count":  len(body.Mappings),
	})
}

// handleImportFile импортирует DBF файл
func (s *Server) handleImportFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	var req models.ImportFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.ImportPointID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ImportPointID")
		return
	}

	if req.FilePath == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан путь к файлу")
		return
	}

	// Получаем маппинг полей в отдельном контексте
	getMappingCtx, getMappingCancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer getMappingCancel()

	mappings, err := s.getFieldMappings(getMappingCtx, req.ImportPointID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения маппинга: %v", err))
		return
	}

	if len(mappings) == 0 {
		s.writeError(w, http.StatusBadRequest, "Для точки импорта не настроен маппинг полей. Сначала сохраните маппинг.")
		return
	}

	// Проверяем наличие обязательных полей в маппинге
	requiredFields := map[string]bool{
		"item_code": true,
		"item_name": true,
		"barcode":   true,
		"price":     true,
	}
	mappedFields := make(map[string]bool)
	for _, mapping := range mappings {
		if mapping.IsRequired || requiredFields[mapping.TargetFieldName] {
			mappedFields[mapping.TargetFieldName] = true
		}
	}

	var missingRequired []string
	for field := range requiredFields {
		if !mappedFields[field] {
			missingRequired = append(missingRequired, field)
		}
	}

	if len(missingRequired) > 0 {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("В маппинге отсутствуют обязательные поля: %s. Пожалуйста, добавьте их в маппинг перед импортом.", strings.Join(missingRequired, ", ")))
		return
	}

	// Проверяем, есть ли незавершенные импорты для этой точки импорта
	checkCtx, checkCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer checkCancel()

	// Сначала очищаем зависшие импорты (PROCESSING старше 30 минут), помечаем их как FAILED
	cleanupQuery := `
		UPDATE InvoiceImport 
		SET ImportStatus = 'FAILED',
		    ErrorMessage = 'Импорт завис (превышено время ожидания 30 минут)',
		    CompletedAt = GETUTCDATE()
		WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt <= DATEADD(minute, -30, GETUTCDATE())
	`
	result, err := s.database.ExecContext(checkCtx, cleanupQuery, sql.Named("importPointID", req.ImportPointID))
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Ошибка очистки зависших импортов: %v", err)
		}
	} else {
		rowsAffected, _ := result.RowsAffected()
		if rowsAffected > 0 && s.logger != nil {
			s.logger.Info("Очищено зависших импортов: %d для точки импорта %s", rowsAffected, req.ImportPointID)
		}
	}

	// Теперь проверяем активные импорты (PROCESSING), но не старше 30 минут (защита от зависших импортов)
	activeImportsQuery := `
		SELECT COUNT(*), MAX(StartedAt) as LastStartedAt
		FROM InvoiceImport
		WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt > DATEADD(minute, -30, GETUTCDATE())
	`
	var activeCount int
	var lastStartedAt sql.NullTime
	err = s.database.QueryRowContext(checkCtx, activeImportsQuery, sql.Named("importPointID", req.ImportPointID)).Scan(&activeCount, &lastStartedAt)
	if err != nil && err != sql.ErrNoRows {
		if s.logger != nil {
			s.logger.Error("Ошибка проверки активных импортов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка проверки активных импортов: %v", err))
		return
	}

	if activeCount > 0 {
		var lastStartedMsg string
		if lastStartedAt.Valid {
			lastStartedMsg = fmt.Sprintf(" (начат: %s)", lastStartedAt.Time.Format("2006-01-02 15:04:05"))
		}
		if s.logger != nil {
			s.logger.Warn("Для точки импорта %s уже есть активный импорт (PROCESSING)%s. Запрос отклонен.", req.ImportPointID, lastStartedMsg)
		}
		// Проверяем, не слишком ли долго идет импорт - возможно завис
		// Если импорт идет более 5 минут, считаем его зависшим (обычно импорт должен закончиться быстрее)
		// Или если импорт начался более 30 минут назад (старые зависшие импорты)
		importDuration := time.Duration(0)
		if lastStartedAt.Valid {
			importDuration = time.Since(lastStartedAt.Time)
		}

		// Если импорт идет более 5 минут - очищаем (большие файлы могут обрабатываться дольше, но 5 минут обычно достаточно)
		// Или если импорт начался более 30 минут назад (старые зависшие импорты)
		if lastStartedAt.Valid && (importDuration > 5*time.Minute || importDuration > 30*time.Minute) {
			if s.logger != nil {
				s.logger.Warn("Импорт идет %v, возможно завис. Принудительно очищаем...", importDuration)
			}
			// Принудительно очищаем импорт, который идет более 5 минут или старше 30 минут
			forceCleanupQuery := `
				UPDATE InvoiceImport 
				SET ImportStatus = 'FAILED',
				    ErrorMessage = 'Импорт завис (принудительно прерван)',
				    CompletedAt = GETUTCDATE()
				WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
				  AND ImportStatus = 'PROCESSING'
				  AND StartedAt <= DATEADD(minute, -5, GETUTCDATE())
			`
			result, err := s.database.ExecContext(checkCtx, forceCleanupQuery, sql.Named("importPointID", req.ImportPointID))
			if err != nil {
				if s.logger != nil {
					s.logger.Error("Ошибка принудительной очистки зависшего импорта: %v", err)
				}
			} else {
				rowsAffected, _ := result.RowsAffected()
				if rowsAffected > 0 && s.logger != nil {
					s.logger.Info("Принудительно очищено зависших импортов: %d для точки импорта %s", rowsAffected, req.ImportPointID)
					// После очистки разрешаем новый импорт
					// Продолжаем выполнение функции
				} else {
					// Если не удалось очистить, возвращаем ошибку
					s.writeError(w, http.StatusConflict, "Для этой точки импорта уже выполняется импорт. Дождитесь завершения текущего импорта.")
					return
				}
			}
		} else {
			// Импорт идет менее 5 минут и не старше 30 минут, отказываем в новом импорте
			s.writeError(w, http.StatusConflict, "Для этой точки импорта уже выполняется импорт. Дождитесь завершения текущего импорта.")
			return
		}
	}

	if s.logger != nil {
		s.logger.Info("Запуск импорта файла: %s для точки импорта %s", req.FilePath, req.ImportPointID)
	}

	// Создаем новый контекст для горутины, который не будет отменен при завершении HTTP запроса
	// НЕ используем defer cancel() здесь, так как контекст нужен для горутины
	// Используем контекст без таймаута для импорта больших файлов (может занять много времени)
	// Для очень больших файлов (100,000+ записей) импорт может занимать несколько часов
	importCtx, importCancel := context.WithCancel(context.Background()) // Без таймаута для больших файлов

	// Запускаем импорт в отдельной горутине с независимым контекстом
	go func() {
		defer importCancel() // Освобождаем ресурсы только когда горутина завершится

		importer := dbfimport.NewDBFImporter(s.database, s.logger)
		invoiceImport, err := importer.ImportInvoice(importCtx, req.ImportPointID, req.FilePath, mappings)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка импорта файла %s: %v", req.FilePath, err)
			}
		} else {
			if s.logger != nil {
				s.logger.Info("Импорт файла %s успешно завершен. InvoiceImportID: %s", req.FilePath, invoiceImport.InvoiceImportID)
			}

			// Обновляем LastUpdateAt в связанном PriceList
			updatePLCtx, updatePLCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, plErr := s.database.ExecContext(updatePLCtx, `
				UPDATE PriceList
				SET LastUpdateAt = GETUTCDATE(), UpdatedAt = GETUTCDATE()
				WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER) AND IsActive = 1
			`, sql.Named("importPointID", req.ImportPointID))
			updatePLCancel()
			if plErr != nil && s.logger != nil {
				s.logger.Warn("Ошибка обновления LastUpdateAt в PriceList: %v", plErr)
			} else if s.logger != nil {
				s.logger.Info("PriceList.LastUpdateAt обновлён для ImportPointID=%s", req.ImportPointID)
			}

			// Автоматически запускаем сопоставление после успешного завершения импорта
			if invoiceImport != nil && invoiceImport.ImportStatus == "COMPLETED" {
				if s.logger != nil {
					s.logger.Info("Автоматический запуск сопоставления для импорта %s", invoiceImport.InvoiceImportID)
				}

				// Создаем новый контекст для сопоставления
				// Используем контекст без таймаута для сопоставления больших файлов (может занять много времени)
				// Для очень больших файлов сопоставление может занимать несколько часов
				matchCtx, matchCancel := context.WithCancel(context.Background()) // Без таймаута для больших файлов
				go func() {
					defer matchCancel()

					matcher := matching.NewPriceMatcher(s.database, s.logger)
					err := matcher.MatchInvoiceData(matchCtx, invoiceImport.InvoiceImportID)
					if err != nil {
						if s.logger != nil {
							s.logger.Error("Ошибка сопоставления данных импорта %s: %v", invoiceImport.InvoiceImportID, err)
						}
					} else {
						if s.logger != nil {
							s.logger.Info("Сопоставление данных импорта %s успешно завершено", invoiceImport.InvoiceImportID)
						}
					}
				}()
			}
		}
	}()

	s.writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":    "import_started",
		"message":   "Импорт запущен в фоновом режиме",
		"file_path": req.FilePath,
	})
}

// getFieldMappings получает маппинг полей для точки импорта
func (s *Server) getFieldMappings(ctx context.Context, importPointID string) ([]models.DBFFieldMapping, error) {
	query := `
		SELECT TOP 200 CAST(MappingID AS NVARCHAR(50)) AS MappingID, 
		       CAST(ImportPointID AS NVARCHAR(50)) AS ImportPointID, 
		       DBFFieldName, TargetFieldName,
		       DataType, IsRequired, DefaultValue, TransformRule, DisplayOrder
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UNIQUEIDENTIFIER)
		ORDER BY DisplayOrder
	`

	rows, err := s.database.QueryContext(ctx, query, sql.Named("importPointID", importPointID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []models.DBFFieldMapping
	for rows.Next() {
		var m models.DBFFieldMapping
		var defaultValue, transformRule sql.NullString

		err := rows.Scan(&m.MappingID, &m.ImportPointID, &m.DBFFieldName, &m.TargetFieldName,
			&m.DataType, &m.IsRequired, &defaultValue, &transformRule, &m.DisplayOrder)
		if err != nil {
			continue
		}

		if defaultValue.Valid {
			m.DefaultValue = &defaultValue.String
		}
		if transformRule.Valid {
			m.TransformRule = &transformRule.String
		}

		mappings = append(mappings, m)
	}

	return mappings, nil
}

// handleAnalyzeImportPoint скачивает DBF с FTP или берёт локально и возвращает список полей
func (s *Server) handleAnalyzeImportPoint(w http.ResponseWriter, r *http.Request, pointID string) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var sourceType string
	var sourceFilePath, ftpHost, ftpUser, ftpPassword, ftpRemotePath sql.NullString
	var ftpPort sql.NullInt32

	query := `
		SELECT SourceType, SourceFilePath, FtpHost, FtpPort, FtpUser, FtpPassword, FtpRemotePath
		FROM ImportPoint
		WHERE ImportPointID = CAST(@id AS UNIQUEIDENTIFIER) AND IsActive = 1
	`
	err := s.database.QueryRowContext(ctx, query, sql.Named("id", pointID)).
		Scan(&sourceType, &sourceFilePath, &ftpHost, &ftpPort, &ftpUser, &ftpPassword, &ftpRemotePath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Точка импорта не найдена")
		return
	}

	var filePath string

	switch sourceType {
	case "local":
		if !sourceFilePath.Valid || sourceFilePath.String == "" {
			s.writeError(w, http.StatusBadRequest, "У точки доступа не указан путь к файлу")
			return
		}
		filePath = sourceFilePath.String

	case "ftp":
		if !ftpHost.Valid || ftpHost.String == "" {
			s.writeError(w, http.StatusBadRequest, "У точки доступа не указан FTP-хост")
			return
		}

		port := 21
		if ftpPort.Valid {
			port = int(ftpPort.Int32)
		}
		user := "anonymous"
		if ftpUser.Valid && ftpUser.String != "" {
			user = ftpUser.String
		}
		pass := ""
		if ftpPassword.Valid {
			pass = ftpPassword.String
		}
		remotePath := "/"
		if ftpRemotePath.Valid && ftpRemotePath.String != "" {
			remotePath = ftpRemotePath.String
		}

		downloaded, err := s.downloadSampleFromFTP(ftpHost.String, port, user, pass, remotePath)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка получения файла с FTP: %v", err))
			return
		}
		filePath = downloaded
		defer os.Remove(filePath)

	default:
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Неизвестный тип источника: %s", sourceType))
		return
	}

	dbfTable, err := godbf.NewFromFile(filePath, "CP866")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось открыть DBF файл: %v", err))
		return
	}

	fieldNames := dbfTable.FieldNames()
	fields := make([]DBFFieldInfo, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		fieldType := "C"
		fieldLength := 0
		decimals := 0
		if dbfTable.NumberOfRecords() > 0 {
			value, err := dbfTable.FieldValueByName(0, fieldName)
			if err == nil && value != "" {
				fieldLength = len(value)
				isNumeric := true
				hasDecimal := false
				for _, ch := range value {
					if ch >= '0' && ch <= '9' || ch == '-' || ch == '+' {
						continue
					} else if ch == '.' || ch == ',' {
						hasDecimal = true
					} else {
						isNumeric = false
						break
					}
				}
				if isNumeric && fieldLength > 0 {
					fieldType = "N"
					if hasDecimal {
						decimals = 2
					}
				}
			}
		}
		fields = append(fields, DBFFieldInfo{Name: fieldName, Type: fieldType, Length: fieldLength, Decimals: decimals})
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"file_path":     filePath,
		"records_count": dbfTable.NumberOfRecords(),
		"fields":        fields,
		"field_count":   len(fields),
		"source_type":   sourceType,
	})
}

// downloadSampleFromFTP подключается к FTP и скачивает первый DBF-файл
func (s *Server) downloadSampleFromFTP(host string, port int, user, pass, remotePath string) (string, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return "", fmt.Errorf("подключение к %s: %v", addr, err)
	}
	defer conn.Quit()

	if err := conn.Login(user, pass); err != nil {
		return "", fmt.Errorf("авторизация: %v", err)
	}

	entries, err := conn.List(remotePath)
	if err != nil {
		return "", fmt.Errorf("листинг %s: %v", remotePath, err)
	}

	var targetFile string
	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFile {
			lower := strings.ToLower(entry.Name)
			if strings.HasSuffix(lower, ".dbf") {
				rp := remotePath
				if !strings.HasSuffix(rp, "/") {
					rp += "/"
				}
				targetFile = rp + entry.Name
				break
			}
		}
	}
	if targetFile == "" {
		return "", fmt.Errorf("DBF-файлы не найдены в %s", remotePath)
	}

	resp, err := conn.Retr(targetFile)
	if err != nil {
		return "", fmt.Errorf("скачивание %s: %v", targetFile, err)
	}
	defer resp.Close()

	tmpDir := filepath.Join(os.TempDir(), "elfapi_analyze")
	os.MkdirAll(tmpDir, 0755)
	tmpFile := filepath.Join(tmpDir, filepath.Base(targetFile))

	f, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("создание временного файла: %v", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp); err != nil {
		return "", fmt.Errorf("запись файла: %v", err)
	}

	if s.logger != nil {
		s.logger.Info("FTP: скачан образец %s → %s", targetFile, tmpFile)
	}

	return tmpFile, nil
}
