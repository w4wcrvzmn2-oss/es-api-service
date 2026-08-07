package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/db"
	"es_api_service/internal/dbfimport"
	"es_api_service/internal/matching"
	"es_api_service/internal/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
		SELECT CAST(s.SupplierID AS TEXT) AS SupplierID, s.Name, s.Code, s.Address, s.Contacts, s.INN,
			s.ContractNumber, s.Login,
			s.IsActive, s.CreatedAt, s.UpdatedAt,
			(SELECT COUNT(*) FROM SupplierRegion sr WHERE sr.SupplierID = s.SupplierID AND sr.IsActive = 1) AS RegionsCount
		FROM Supplier s
		ORDER BY s.Name
LIMIT 500
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
		var code, address, contacts, inn, contractNumber, login sql.NullString
		var createdAt, updatedAt time.Time

		err := rows.Scan(&supplier.SupplierID, &supplier.Name, &code, &address, &contacts, &inn,
			&contractNumber, &login,
			&supplier.IsActive, &createdAt, &updatedAt, &supplier.RegionsCount)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования поставщика: %v", err)
			}
			continue
		}

		if code.Valid {
			supplier.Code = &code.String
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
	if !isValidSupplierCode(req.Code) {
		s.writeError(w, http.StatusBadRequest, "Код поставщика должен содержать только цифры")
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
		INSERT INTO Supplier (SupplierID, Name, Code, Address, Contacts, INN, ContractNumber, Login, Password, IsActive, CreatedAt, UpdatedAt)
		VALUES (CAST(? AS UUID), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	now := time.Now()
	_, err := s.database.ExecContext(ctx, query,
		supplierID,
		req.Name,
		normalizeSupplierCode(req.Code),
		req.Address,
		req.Contacts,
		req.INN,
		req.ContractNumber,
		req.Login,
		passwordValue,
		req.IsActive,
		now,
		now)
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

// isValidSupplierCode: код поставщика — только цифры (или пусто).
func isValidSupplierCode(code *string) bool {
	if code == nil {
		return true
	}
	c := strings.TrimSpace(*code)
	if c == "" {
		return true
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeSupplierCode приводит код к значению для БД: обрезает пробелы, пусто → NULL.
func normalizeSupplierCode(code *string) interface{} {
	if code == nil {
		return nil
	}
	c := strings.TrimSpace(*code)
	if c == "" {
		return nil
	}
	return c
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
	if !isValidSupplierCode(req.Code) {
		s.writeError(w, http.StatusBadRequest, "Код поставщика должен содержать только цифры")
		return
	}

	var passwordValue interface{}
	updatePassword := false
	if req.Password != nil && *req.Password != "" {
		hashedPassword, err := hashPassword(*req.Password)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "Ошибка подготовки пароля")
			return
		}
		passwordValue = hashedPassword
		updatePassword = true
	}

	query := `
		UPDATE Supplier SET
			Name = ?,
			Code = ?,
			Address = ?,
			Contacts = ?,
			INN = ?,
			ContractNumber = ?,
			Login = ?,
			IsActive = ?,
			UpdatedAt = ?
		WHERE SupplierID = CAST(? AS UUID)
	`
	args := []interface{}{
		req.Name,
		normalizeSupplierCode(req.Code),
		req.Address,
		req.Contacts,
		req.INN,
		req.ContractNumber,
		req.Login,
		req.IsActive,
		time.Now(),
		supplierID,
	}
	if updatePassword {
		query = `
			UPDATE Supplier SET
				Name = ?,
				Code = ?,
				Address = ?,
				Contacts = ?,
				INN = ?,
				ContractNumber = ?,
				Login = ?,
				Password = ?,
				IsActive = ?,
				UpdatedAt = ?
			WHERE SupplierID = CAST(? AS UUID)
		`
		args = []interface{}{
			req.Name,
			normalizeSupplierCode(req.Code),
			req.Address,
			req.Contacts,
			req.INN,
			req.ContractNumber,
			req.Login,
			passwordValue,
			req.IsActive,
			time.Now(),
			supplierID,
		}
	}

	result, err := s.database.ExecContext(ctx, query, args...)
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

	query := `DELETE FROM Supplier WHERE SupplierID = CAST(@supplierID AS UUID)`
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
		SELECT CAST(sr.RegionID AS TEXT) AS RegionID,
			r.Name AS RegionName,
			r.Code AS RegionCode,
			sr.IsActive
		FROM SupplierRegion sr
		INNER JOIN Region r ON sr.RegionID = r.RegionID
		WHERE sr.SupplierID = CAST(@supplierID AS UUID)
		  AND sr.IsActive = 1
		ORDER BY r.Name
LIMIT 200
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

	delQuery := `DELETE FROM SupplierRegion WHERE SupplierID = CAST(? AS UUID)`
	if _, err := db.ExecRaw(ctx, tx, delQuery, supplierID); err != nil {
		return err
	}

	insertQuery := `
		INSERT INTO SupplierRegion (SupplierRegionID, SupplierID, RegionID, IsActive, CreatedAt)
		VALUES (gen_random_uuid(), CAST(? AS UUID), CAST(? AS UUID), TRUE, (NOW() AT TIME ZONE 'utc'))
	`
	for _, regionID := range regionIDs {
		if regionID != "" {
			if _, err := db.ExecRaw(ctx, tx, insertQuery, supplierID, regionID); err != nil {
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
		WHERE pl.SupplierID = CAST(@supplierID AS UUID)
		  AND pl.IsActive = 1
		  AND plr.RegionID NOT IN (
			SELECT RegionID FROM SupplierRegion
			WHERE SupplierID = CAST(@supplierID AS UUID) AND IsActive = 1
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
		DELETE FROM PriceListRegion plr
		USING PriceList pl
		WHERE plr.PriceListID = pl.PriceListID
		  AND pl.SupplierID = CAST(? AS UUID)
		  AND plr.RegionID NOT IN (
			SELECT RegionID FROM SupplierRegion
			WHERE SupplierID = CAST(? AS UUID) AND IsActive = TRUE
		  )
	`
	_, err = s.database.ExecContext(ctx, deleteQuery, supplierID, supplierID)
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
		SELECT CAST(ip.ImportPointID AS TEXT) AS ImportPointID, 
		       CAST(ip.SupplierID AS TEXT) AS SupplierID, 
		       s.Name AS SupplierName,
		       ip.Name, ip.Description, ip.SourceType, ip.DBFFilePath, ip.SourceFilePath,
		       ip.FtpHost, ip.FtpPort, ip.FtpUser, ip.FtpPassword, ip.FtpRemotePath,
		       ip.IsActive, ip.CreatedAt, ip.UpdatedAt
		FROM ImportPoint ip
		LEFT JOIN Supplier s ON ip.SupplierID = s.SupplierID
		WHERE 1=1`
	var args []interface{}

	if supplierID != "" {
		query += ` AND ip.SupplierID = CAST(? AS UUID)`
		args = append(args, supplierID)
	}
	query += `
		ORDER BY ip.Name
		LIMIT 500`

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
	now := time.Now()
	// Позиционные ? — надёжнее для PostgreSQL/GORM, чем @named (иначе бывают паники/500).
	query := `
		INSERT INTO ImportPoint (
			ImportPointID, Name, Description, SourceType, SourceFilePath,
			FtpHost, FtpPort, FtpUser, FtpPassword, FtpRemotePath,
			IsActive, CreatedAt, UpdatedAt
		) VALUES (
			CAST(? AS UUID), ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?
		)`

	result, err := s.database.ExecContext(ctx, query,
		importPointID,
		req.Name,
		req.Description,
		req.SourceType,
		req.SourceFilePath,
		req.FtpHost,
		req.FtpPort,
		req.FtpUser,
		req.FtpPassword,
		req.FtpRemotePath,
		req.IsActive,
		now,
		now,
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения SQL при создании точки импорта: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Не удалось создать точку импорта: %v", err))
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
			Name = ?,
			Description = ?,
			SourceType = ?,
			SourceFilePath = ?,
			FtpHost = ?,
			FtpPort = ?,
			FtpUser = ?,
			FtpPassword = ?,
			FtpRemotePath = ?,
			IsActive = ?,
			UpdatedAt = ?
		WHERE ImportPointID = CAST(? AS UUID)
	`

	result, err := s.database.ExecContext(ctx, query,
		req.Name,
		req.Description,
		req.SourceType,
		req.SourceFilePath,
		req.FtpHost,
		req.FtpPort,
		req.FtpUser,
		req.FtpPassword,
		req.FtpRemotePath,
		req.IsActive,
		time.Now(),
		pointID,
	)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Не удалось обновить точку импорта: %v", err))
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

	delMappings := `DELETE FROM DBFFieldMapping WHERE ImportPointID = CAST(? AS UUID)`
	if _, err := db.ExecRaw(ctx, tx, delMappings, pointID); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка удаления маппингов ImportPoint %s: %v", pointID, err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка удаления маппингов: %v", err))
		return
	}

	query := `DELETE FROM ImportPoint WHERE ImportPointID = CAST(? AS UUID)`
	result, err := db.ExecRaw(ctx, tx, query, pointID)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "foreign key") || strings.Contains(errStr, "violates foreign key") ||
			strings.Contains(errStr, "REFERENCE") {
			s.writeError(w, http.StatusConflict, "Невозможно удалить: точка импорта используется в прайс-листах или импортах")
			return
		}
		if s.logger != nil {
			s.logger.Error("Ошибка удаления ImportPoint %s: %v", pointID, err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка удаления точки импорта: %v", err))
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
		SELECT CAST(MappingID AS TEXT) AS MappingID, 
		       CAST(ImportPointID AS TEXT) AS ImportPointID, 
		       DBFFieldName, TargetFieldName,
		       DataType, IsRequired, DefaultValue, TransformRule, DisplayOrder,
		       CreatedAt, UpdatedAt
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		ORDER BY DisplayOrder, DBFFieldName
LIMIT 200
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

// invoiceImportView — DTO списка импортов для менеджера/админа.
type invoiceImportView struct {
	InvoiceImportID  string  `json:"invoice_import_id"`
	ImportPointID    string  `json:"import_point_id"`
	ImportPointName  *string `json:"import_point_name,omitempty"`
	SupplierID       *string `json:"supplier_id,omitempty"`
	SupplierName     *string `json:"supplier_name,omitempty"`
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

// handleInvoiceImportsRouter — /api/invoice-imports и /api/invoice-imports/{id}/...
func (s *Server) handleInvoiceImportsRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/invoice-imports")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		if r.Method == http.MethodGet {
			s.handleGetInvoiceImports(w, r)
			return
		}
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	parts := strings.Split(path, "/")
	importID := parts[0]
	if _, err := uuid.Parse(importID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый ID импорта")
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "":
		if r.Method == http.MethodDelete {
			s.handleDeleteInvoiceImport(w, r, importID)
			return
		}
		if r.Method == http.MethodGet {
			s.handleGetInvoiceImports(w, r) // filter by id via query not needed; list is enough
			return
		}
	case "download":
		if r.Method == http.MethodGet {
			s.handleDownloadInvoiceImport(w, r, importID)
			return
		}
	case "reassign":
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			s.handleReassignInvoiceImport(w, r, importID)
			return
		}
	}
	s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
}

// handleGetInvoiceImports возвращает список импортов прайсов.
// Фильтры import_point_id / supplier_id / status опциональны (для менеджера — полный список с LIMIT).
func (s *Server) handleGetInvoiceImports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	importPointID := r.URL.Query().Get("import_point_id")
	supplierID := r.URL.Query().Get("supplier_id")
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
		if limit > 500 {
			limit = 500
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	query := `
		SELECT CAST(ii.InvoiceImportID AS TEXT) AS InvoiceImportID,
			CAST(ii.ImportPointID AS TEXT) AS ImportPointID,
			ip.Name AS ImportPointName,
			CAST(ip.SupplierID AS TEXT) AS SupplierID,
			s.Name AS SupplierName,
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
		LEFT JOIN ImportPoint ip ON ii.ImportPointID = ip.ImportPointID
		LEFT JOIN Supplier s ON ip.SupplierID = s.SupplierID
		WHERE 1=1
`
	var args []interface{}
	if importPointID != "" {
		query += ` AND ii.ImportPointID = CAST(@importPointID AS UUID)`
		args = append(args, sql.Named("importPointID", importPointID))
	}
	if supplierID != "" {
		query += ` AND ip.SupplierID = CAST(@supplierID AS UUID)`
		args = append(args, sql.Named("supplierID", supplierID))
	}
	if status != "" {
		query += ` AND ii.ImportStatus = @status`
		args = append(args, sql.Named("status", strings.ToUpper(status)))
	}
	query += fmt.Sprintf(` ORDER BY ii.CreatedAt DESC OFFSET %d LIMIT %d`, offset, limit)

	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения импортов: %v", err))
		return
	}
	defer rows.Close()

	var imports []invoiceImportView
	for rows.Next() {
		var imp invoiceImportView
		var fileSize sql.NullInt64
		var errorMessage, ipName, supplierIDNull, supplierName sql.NullString
		var startedAt, completedAt, createdAt sql.NullTime

		err := rows.Scan(
			&imp.InvoiceImportID,
			&imp.ImportPointID,
			&ipName,
			&supplierIDNull,
			&supplierName,
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

		if ipName.Valid {
			imp.ImportPointName = &ipName.String
		}
		if supplierIDNull.Valid {
			imp.SupplierID = &supplierIDNull.String
		}
		if supplierName.Valid {
			imp.SupplierName = &supplierName.String
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
		imports = []invoiceImportView{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"imports": imports,
		"total":   len(imports),
		"limit":   limit,
		"offset":  offset,
	})
}

func (s *Server) handleDownloadInvoiceImport(w http.ResponseWriter, r *http.Request, importID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var fileName, filePath sql.NullString
	err := s.database.QueryRowContext(ctx, `
		SELECT FileName, FilePath FROM InvoiceImport
		WHERE InvoiceImportID = CAST(@id AS UUID)
	`, sql.Named("id", importID)).Scan(&fileName, &filePath)
	if err == sql.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "Импорт не найден")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка чтения импорта")
		return
	}
	if !filePath.Valid || strings.TrimSpace(filePath.String) == "" {
		s.writeError(w, http.StatusNotFound, "Файл импорта не указан")
		return
	}
	path := filepath.Clean(filePath.String)
	f, err := os.Open(path)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Файл на диске не найден")
		return
	}
	defer f.Close()

	name := "import.bin"
	if fileName.Valid && fileName.String != "" {
		name = filepath.Base(fileName.String)
	} else {
		name = filepath.Base(path)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	http.ServeContent(w, r, name, time.Time{}, f)
}

func (s *Server) handleDeleteInvoiceImport(w http.ResponseWriter, r *http.Request, importID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var filePath sql.NullString
	err := s.database.QueryRowContext(ctx, `
		SELECT FilePath FROM InvoiceImport WHERE InvoiceImportID = CAST(@id AS UUID)
	`, sql.Named("id", importID)).Scan(&filePath)
	if err == sql.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "Импорт не найден")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка чтения импорта")
		return
	}

	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка транзакции")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM SupplierPrice WHERE InvoiceImportID = CAST(@id AS UUID)`, sql.Named("id", importID)); err != nil {
		s.logger.Error("Ошибка удаления SupplierPrice для импорта %s: %v", importID, err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления связанных цен")
		return
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM InvoiceData WHERE InvoiceImportID = CAST(@id AS UUID)`, sql.Named("id", importID)); err != nil {
		s.logger.Error("Ошибка удаления InvoiceData для импорта %s: %v", importID, err)
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления данных импорта")
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM InvoiceImport WHERE InvoiceImportID = CAST(@id AS UUID)`, sql.Named("id", importID))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка удаления импорта")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		s.writeError(w, http.StatusNotFound, "Импорт не найден")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка фиксации удаления")
		return
	}

	if filePath.Valid && strings.TrimSpace(filePath.String) != "" {
		_ = os.Remove(filepath.Clean(filePath.String))
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":            "Импорт удалён",
		"invoice_import_id":  importID,
	})
}

func (s *Server) handleReassignInvoiceImport(w http.ResponseWriter, r *http.Request, importID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req struct {
		ImportPointID string `json:"import_point_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ImportPointID) == "" {
		s.writeError(w, http.StatusBadRequest, "Укажите import_point_id")
		return
	}
	if _, err := uuid.Parse(req.ImportPointID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Недопустимый import_point_id")
		return
	}

	var exists int
	err := s.database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ImportPoint WHERE ImportPointID = CAST(@id AS UUID) AND IsActive = 1
	`, sql.Named("id", req.ImportPointID)).Scan(&exists)
	if err != nil || exists == 0 {
		s.writeError(w, http.StatusBadRequest, "Точка импорта не найдена")
		return
	}

	res, err := s.database.ExecContext(ctx, `
		UPDATE InvoiceImport
		SET ImportPointID = CAST(@pointID AS UUID)
		WHERE InvoiceImportID = CAST(@id AS UUID)
	`, sql.Named("pointID", req.ImportPointID), sql.Named("id", importID))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка переназначения импорта")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		s.writeError(w, http.StatusNotFound, "Импорт не найден")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":            "Импорт переназначен",
		"invoice_import_id":  importID,
		"import_point_id":    req.ImportPointID,
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

	// Используем INSERT ... ON CONFLICT (UPSERT) для обновления или вставки
	// Это предотвращает ошибку дублирования ключа при повторном сохранении
	query := `
		INSERT INTO "DBFFieldMapping" (
			"MappingID", "ImportPointID", "DBFFieldName", "TargetFieldName", "DataType",
			"IsRequired", "DefaultValue", "TransformRule", "DisplayOrder", "CreatedAt", "UpdatedAt"
		) VALUES (
			gen_random_uuid(), CAST(@importPointID AS UUID), @dbfFieldName, @targetFieldName, @dataType,
			@isRequired, @defaultValue, @transformRule, @displayOrder, @createdAt, @updatedAt
		)
		ON CONFLICT ("ImportPointID", "DBFFieldName") DO UPDATE SET
			"TargetFieldName" = EXCLUDED."TargetFieldName",
			"DataType" = EXCLUDED."DataType",
			"IsRequired" = EXCLUDED."IsRequired",
			"DefaultValue" = EXCLUDED."DefaultValue",
			"TransformRule" = EXCLUDED."TransformRule",
			"DisplayOrder" = EXCLUDED."DisplayOrder",
			"UpdatedAt" = EXCLUDED."UpdatedAt"
		RETURNING CAST("MappingID" AS TEXT) AS "MappingID";
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

	_, err = db.ExecRaw(ctx, tx,
		`DELETE FROM DBFFieldMapping WHERE ImportPointID = CAST(? AS UUID)`,
		importPointID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка очистки старых маппингов: %v", err))
		return
	}

	now := time.Now()
	for i, m := range body.Mappings {
		_, err = db.ExecRaw(ctx, tx, `
			INSERT INTO DBFFieldMapping
				(MappingID, ImportPointID, DBFFieldName, TargetFieldName, DataType,
				 IsRequired, DefaultValue, TransformRule, DisplayOrder, CreatedAt, UpdatedAt)
			VALUES
				(gen_random_uuid(), CAST(? AS UUID), ?, ?, ?,
				 ?, ?, ?, ?, ?, ?)`,
			importPointID,
			m.DBFFieldName,
			m.TargetFieldName,
			m.DataType,
			m.IsRequired,
			m.DefaultValue,
			m.TransformRule,
			i,
			now,
			now)
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

// handleImportFile импортирует файл прайса (DBF/Excel)
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

	normalizedFilePath := req.FilePath
	if strings.ToLower(filepath.Ext(normalizedFilePath)) == ".zip" {
		extracted, err := dbfimport.ExtractArchive(normalizedFilePath, s.logger)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось извлечь файл данных из ZIP: %v", err))
			return
		}
		normalizedFilePath = extracted
	}
	if !dbfimport.IsSupportedDataFile(normalizedFilePath) {
		s.writeError(w, http.StatusBadRequest, "Поддерживаются только файлы .dbf, .xlsx, .xlsm, .xls, .xml, .sst и .zip")
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
		    CompletedAt = (NOW() AT TIME ZONE 'utc')
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt <= DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
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
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt > DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
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
				    CompletedAt = (NOW() AT TIME ZONE 'utc')
				WHERE ImportPointID = CAST(@importPointID AS UUID)
				  AND ImportStatus = 'PROCESSING'
				  AND StartedAt <= DATEADD(minute, -5, (NOW() AT TIME ZONE 'utc'))
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
		s.logger.Info("Запуск импорта файла: %s для точки импорта %s", normalizedFilePath, req.ImportPointID)
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
		invoiceImport, err := importer.ImportInvoice(importCtx, req.ImportPointID, normalizedFilePath, mappings)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка импорта файла %s: %v", normalizedFilePath, err)
			}
		} else {
			if s.logger != nil {
				s.logger.Info("Импорт файла %s успешно завершен. InvoiceImportID: %s", normalizedFilePath, invoiceImport.InvoiceImportID)
			}

			// Обновляем LastUpdateAt в связанном PriceList
			updatePLCtx, updatePLCancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, plErr := s.database.ExecContext(updatePLCtx, `
				UPDATE PriceList
				SET LastUpdateAt = (NOW() AT TIME ZONE 'utc'), UpdatedAt = (NOW() AT TIME ZONE 'utc')
				WHERE ImportPointID = CAST(@importPointID AS UUID) AND IsActive = 1
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
						s.scheduleRebuildPriceCacheByImportPoint(req.ImportPointID)
					}
				}()
			}
		}
	}()

	s.writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":    "import_started",
		"message":   "Импорт запущен в фоновом режиме",
		"file_path": normalizedFilePath,
	})
}

// getFieldMappings получает маппинг полей для точки импорта
func (s *Server) getFieldMappings(ctx context.Context, importPointID string) ([]models.DBFFieldMapping, error) {
	query := `
		SELECT CAST(MappingID AS TEXT) AS MappingID, 
		       CAST(ImportPointID AS TEXT) AS ImportPointID, 
		       DBFFieldName, TargetFieldName,
		       DataType, IsRequired, DefaultValue, TransformRule, DisplayOrder
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		ORDER BY DisplayOrder
LIMIT 200
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

// handleAnalyzeImportPoint скачивает файл прайса с FTP или берёт локально и возвращает список полей
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
		WHERE ImportPointID = CAST(@id AS UUID) AND IsActive = 1
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

	if strings.ToLower(filepath.Ext(filePath)) == ".zip" {
		extracted, err := dbfimport.ExtractArchive(filePath, s.logger)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось извлечь файл данных из ZIP: %v", err))
			return
		}
		filePath = extracted
	}

	headers, records, err := dbfimport.ReadTabularFile(filePath)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось открыть файл данных: %v", err))
		return
	}

	rawFields := dbfimport.BuildFieldInfos(headers, records)
	fields := make([]DBFFieldInfo, 0, len(rawFields))
	for _, rawField := range rawFields {
		fields = append(fields, DBFFieldInfo{
			Name:     rawField["name"].(string),
			Type:     rawField["type"].(string),
			Length:   rawField["length"].(int),
			Decimals: rawField["decimals"].(int),
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"file_path":     filePath,
		"records_count": len(records),
		"fields":        fields,
		"field_count":   len(fields),
		"source_type":   sourceType,
		"file_format":   dbfimport.DetectDataFileFormat(filePath),
	})
}

// downloadSampleFromFTP подключается к FTP и скачивает первый поддерживаемый файл прайса.
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
			if dbfimport.IsSupportedDataFile(lower) || strings.HasSuffix(lower, ".zip") {
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
		return "", fmt.Errorf("файлы .dbf/.xlsx/.xlsm/.xls/.xml/.sst/.zip не найдены в %s", remotePath)
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
