package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// handleSupplierMarkupPoliciesRouter роутит запросы к /api/supplier-markup-policies
func (s *Server) handleSupplierMarkupPoliciesRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/supplier-markup-policies/")
	path = strings.TrimRight(path, "/")

	if path != "" && path != r.URL.Path {
		policyID := strings.Split(path, "/")[0]
		if _, err := uuid.Parse(policyID); err != nil {
			s.writeError(w, http.StatusBadRequest, "Неверный формат ID")
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), policyIDCtxKey, policyID))
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			s.handleUpdateSupplierMarkupPolicy(w, r)
		case http.MethodDelete:
			s.handleDeleteSupplierMarkupPolicy(w, r)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetSupplierMarkupPolicies(w, r)
	case http.MethodPost:
		s.handleCreateSupplierMarkupPolicy(w, r)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
	}
}

// policyIDCtxKey — типизированный ключ контекста для policy ID, чтобы избежать
// предупреждения линтера о string-ключе.
type policyIDCtxKeyType struct{}

var policyIDCtxKey = policyIDCtxKeyType{}

// handleGetSupplierMarkupPolicies возвращает список политик наценок для поставщика
func (s *Server) handleGetSupplierMarkupPolicies(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleGetSupplierMarkupPolicies: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	supplierID := r.URL.Query().Get("supplier_id")
	if supplierID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type Policy struct {
		PolicyID      string     `json:"policy_id"`
		SupplierID    string     `json:"supplier_id"`
		SupplierName  string     `json:"supplier_name"`
		RegionID      *string    `json:"region_id,omitempty"`
		RegionName    *string    `json:"region_name,omitempty"`
		MarkupPct     float64    `json:"markup_pct"`
		RoundingStep  *float64   `json:"rounding_step,omitempty"`
		IsActive      bool       `json:"is_active"`
		EffectiveFrom time.Time  `json:"effective_from"`
		EffectiveTo   *time.Time `json:"effective_to,omitempty"`
		CreatedAt     time.Time  `json:"created_at"`
	}

	var policies []Policy
	err := s.database.GORMWith(ctx).Raw(
		`SELECT TOP 100
			CAST(smp.PolicyID AS NVARCHAR(50)) AS PolicyID,
			CAST(smp.SupplierID AS NVARCHAR(50)) AS SupplierID,
			s.Name AS SupplierName,
			CAST(smp.RegionID AS NVARCHAR(50)) AS RegionID,
			r.Name AS RegionName,
			smp.MarkupPct,
			smp.RoundingStep,
			smp.IsActive,
			smp.EffectiveFrom,
			smp.EffectiveTo,
			smp.CreatedAt
		FROM SupplierMarkupPolicy smp
		INNER JOIN Supplier s ON smp.SupplierID = s.SupplierID
		LEFT JOIN Region r ON smp.RegionID = r.RegionID
		WHERE smp.SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER)
		ORDER BY r.Name, smp.CreatedAt DESC`,
		sql.Named("supplierID", supplierID),
	).Scan(&policies).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения политик наценок: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения политик наценок: %v", err))
		return
	}

	if policies == nil {
		policies = []Policy{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"policies": policies,
		"count":    len(policies),
	})
}

// handleCreateSupplierMarkupPolicy создает новую политику наценок
func (s *Server) handleCreateSupplierMarkupPolicy(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleCreateSupplierMarkupPolicy: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	var req struct {
		SupplierID   string   `json:"supplier_id"`
		RegionID     *string  `json:"region_id,omitempty"`
		MarkupPct    float64  `json:"markup_pct"`
		RoundingStep *float64 `json:"rounding_step,omitempty"`
		IsActive     *bool    `json:"is_active,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.SupplierID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	policyID := uuid.New().String()

	query := `
		INSERT INTO SupplierMarkupPolicy (
			PolicyID, SupplierID, RegionID, MarkupPct, RoundingStep, IsActive, EffectiveFrom, CreatedAt
		) VALUES (
			CAST(@policyID AS UNIQUEIDENTIFIER),
			CAST(@supplierID AS UNIQUEIDENTIFIER),
			CASE WHEN @regionID IS NULL OR @regionID = '' THEN NULL ELSE CAST(@regionID AS UNIQUEIDENTIFIER) END,
			@markupPct,
			@roundingStep,
			ISNULL(@isActive, 1),
			GETUTCDATE(),
			GETUTCDATE()
		)
	`

	args := []interface{}{
		sql.Named("policyID", policyID),
		sql.Named("supplierID", req.SupplierID),
		sql.Named("markupPct", req.MarkupPct),
	}
	if req.RegionID != nil && *req.RegionID != "" {
		args = append(args, sql.Named("regionID", *req.RegionID))
	} else {
		args = append(args, sql.Named("regionID", nil))
	}
	if req.RoundingStep != nil {
		args = append(args, sql.Named("roundingStep", *req.RoundingStep))
	} else {
		args = append(args, sql.Named("roundingStep", nil))
	}
	if req.IsActive != nil {
		args = append(args, sql.Named("isActive", *req.IsActive))
	} else {
		args = append(args, sql.Named("isActive", true))
	}

	err := s.database.GORMWith(ctx).Exec(query, args...).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания политики наценок: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания политики наценок: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"message":   "Политика наценок создана успешно",
		"policy_id": policyID,
	})
}

// handleUpdateSupplierMarkupPolicy обновляет существующую политику наценок
func (s *Server) handleUpdateSupplierMarkupPolicy(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleUpdateSupplierMarkupPolicy: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	policyID, ok := r.Context().Value(policyIDCtxKey).(string)
	if !ok || policyID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан policy_id")
		return
	}

	var req struct {
		RegionID     *string  `json:"region_id,omitempty"`
		MarkupPct    *float64 `json:"markup_pct,omitempty"`
		RoundingStep *float64 `json:"rounding_step,omitempty"`
		IsActive     *bool    `json:"is_active,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var updates []string
	var args []interface{}
	args = append(args, sql.Named("policyID", policyID))

	if req.RegionID != nil {
		if *req.RegionID == "" {
			updates = append(updates, "RegionID = NULL")
		} else {
			updates = append(updates, "RegionID = CAST(@regionID AS UNIQUEIDENTIFIER)")
			args = append(args, sql.Named("regionID", *req.RegionID))
		}
	}
	if req.MarkupPct != nil {
		updates = append(updates, "MarkupPct = @markupPct")
		args = append(args, sql.Named("markupPct", *req.MarkupPct))
	}
	if req.RoundingStep != nil {
		updates = append(updates, "RoundingStep = @roundingStep")
		args = append(args, sql.Named("roundingStep", *req.RoundingStep))
	}
	if req.IsActive != nil {
		updates = append(updates, "IsActive = @isActive")
		args = append(args, sql.Named("isActive", *req.IsActive))
	}

	if len(updates) == 0 {
		s.writeError(w, http.StatusBadRequest, "Не указаны поля для обновления")
		return
	}

	query := fmt.Sprintf(`
		UPDATE SupplierMarkupPolicy
		SET %s
		WHERE PolicyID = CAST(@policyID AS UNIQUEIDENTIFIER)
	`, strings.Join(updates, ", "))

	err := s.database.GORMWith(ctx).Exec(query, args...).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка обновления политики наценок: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления политики наценок: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Политика наценок обновлена успешно",
	})
}

// handleDeleteSupplierMarkupPolicy удаляет политику наценок
func (s *Server) handleDeleteSupplierMarkupPolicy(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.logger != nil {
				s.logger.Error("Паника в handleDeleteSupplierMarkupPolicy: %v", rec)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Внутренняя ошибка сервера: %v", rec))
		}
	}()

	if s.database == nil {
		s.writeError(w, http.StatusServiceUnavailable, "База данных недоступна")
		return
	}

	policyID, ok := r.Context().Value(policyIDCtxKey).(string)
	if !ok || policyID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан policy_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.database.GORMWith(ctx).Exec(
		`DELETE FROM SupplierMarkupPolicy WHERE PolicyID = CAST(@policyID AS UNIQUEIDENTIFIER)`,
		sql.Named("policyID", policyID),
	).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка удаления политики наценок: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка удаления политики наценок: %v", err))
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Политика наценок удалена успешно",
	})
}
