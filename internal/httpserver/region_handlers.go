package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type regionRequest struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	FederalDistrict string `json:"federal_district"`
	Capital         string `json:"capital"`
	IsActive        *bool  `json:"is_active"`
}

// handleCreateRegion создает новый регион в справочнике Region.
func (s *Server) handleCreateRegion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var req regionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Ошибка парсинга запроса")
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.FederalDistrict = strings.TrimSpace(req.FederalDistrict)
	req.Capital = strings.TrimSpace(req.Capital)
	if req.Code == "" || req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Код и название региона обязательны")
		return
	}
	if req.FederalDistrict == "" {
		req.FederalDistrict = "-"
	}
	if req.Capital == "" {
		req.Capital = "-"
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	var exists int
	err := s.database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM "Region"
		WHERE lower("Code") = lower(@code) OR lower("Name") = lower(@name)
	`, sql.Named("code", req.Code), sql.Named("name", req.Name)).Scan(&exists)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка проверки дубликата региона: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка проверки региона")
		return
	}
	if exists > 0 {
		s.writeError(w, http.StatusConflict, "Регион с таким кодом или названием уже существует")
		return
	}

	regionID := uuid.New().String()
	_, err = s.database.ExecContext(ctx, `
		INSERT INTO "Region" (
			"RegionID", "Code", "Name", "FederalDistrict", "Capital",
			"IsActive", "CreatedAt", "UpdatedAt"
		) VALUES (
			CAST(@id AS UUID), @code, @name, @fd, @capital,
			@active, (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc')
		)
	`,
		sql.Named("id", regionID),
		sql.Named("code", req.Code),
		sql.Named("name", req.Name),
		sql.Named("fd", req.FederalDistrict),
		sql.Named("capital", req.Capital),
		sql.Named("active", isActive),
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания региона: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания региона")
		return
	}

	if s.logger != nil {
		s.logger.Info("Регион создан: %s (%s) id=%s", req.Name, req.Code, regionID)
	}
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"region_id": regionID,
		"code":      req.Code,
		"name":      req.Name,
	})
}

// handleUpdateRegion обновляет существующий регион.
func (s *Server) handleUpdateRegion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/region/")
	regionID := strings.Trim(path, "/")
	if _, err := uuid.Parse(regionID); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный ID региона")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var req regionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Ошибка парсинга запроса")
		return
	}

	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	req.FederalDistrict = strings.TrimSpace(req.FederalDistrict)
	req.Capital = strings.TrimSpace(req.Capital)
	if req.Code == "" || req.Name == "" {
		s.writeError(w, http.StatusBadRequest, "Код и название региона обязательны")
		return
	}
	if req.FederalDistrict == "" {
		req.FederalDistrict = "-"
	}
	if req.Capital == "" {
		req.Capital = "-"
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	var exists int
	err := s.database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM "Region"
		WHERE ("Code" = @code OR "Name" = @name)
		  AND "RegionID" <> CAST(@id AS UUID)
	`, sql.Named("code", req.Code), sql.Named("name", req.Name), sql.Named("id", regionID)).Scan(&exists)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка проверки региона")
		return
	}
	if exists > 0 {
		s.writeError(w, http.StatusConflict, "Регион с таким кодом или названием уже существует")
		return
	}

	res, err := s.database.ExecContext(ctx, `
		UPDATE "Region" SET
			"Code" = @code,
			"Name" = @name,
			"FederalDistrict" = @fd,
			"Capital" = @capital,
			"IsActive" = @active,
			"UpdatedAt" = (NOW() AT TIME ZONE 'utc')
		WHERE "RegionID" = CAST(@id AS UUID)
	`,
		sql.Named("id", regionID),
		sql.Named("code", req.Code),
		sql.Named("name", req.Name),
		sql.Named("fd", req.FederalDistrict),
		sql.Named("capital", req.Capital),
		sql.Named("active", isActive),
	)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка обновления региона %s: %v", regionID, err)
		}
		s.writeError(w, http.StatusInternalServerError, "Ошибка обновления региона")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		s.writeError(w, http.StatusNotFound, "Регион не найден")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"region_id": regionID,
		"code":      req.Code,
		"name":      req.Name,
	})
}

// handleRegionRouter — PUT/PATCH /api/region/{id}
func (s *Server) handleRegionRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/region/")
	if path == "" || path == "create" {
		if path == "create" {
			s.handleCreateRegion(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	if strings.Contains(path, "/") {
		s.writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		s.handleUpdateRegion(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}
