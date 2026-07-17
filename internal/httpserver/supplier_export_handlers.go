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

// handleSCExportConfig — GET/PUT /api/sc/export-config
// Настройки выгрузки заказов поставщика (куда и как отправлять).
func (s *Server) handleSCExportConfig(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		var cfg models.SupplierExportConfig
		err := s.database.GORMWith(ctx).
			Where("SupplierID = ?", db.UUIDParam(sid)).
			Take(&cfg).Error
		if err == gorm.ErrRecordNotFound {
			// Настроек ещё нет — отдаём разумные значения по умолчанию.
			writeJSON(w, http.StatusOK, models.SupplierExportConfig{
				SupplierID: sid,
				Method:     "none",
				Format:     "DBF",
				FtpPort:    21,
				SmtpPort:   587,
				IsActive:   true,
			})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)

	case http.MethodPut:
		var req models.SupplierExportConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный формат запроса"})
			return
		}

		// Нормализация.
		switch req.Method {
		case "ftp", "email", "both", "none":
		default:
			req.Method = "none"
		}
		req.Format = "DBF" // пока поддерживаем только DBF
		if req.FtpPort <= 0 {
			req.FtpPort = 21
		}
		if req.SmtpPort <= 0 {
			req.SmtpPort = 587
		}
		req.SupplierID = sid
		req.UpdatedAt = time.Now().UTC()

		// Одна запись на поставщика: перезаписываем целиком.
		err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.Exec(`DELETE FROM SupplierExportConfig WHERE SupplierID = ?`, db.UUIDParam(sid)).Error; e != nil {
				return e
			}
			req.SupplierExportConfigID = uuid.New().String()
			return tx.Create(&req).Error
		})
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка сохранения настроек выгрузки поставщика %s: %v", sid, err)
			}
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка сохранения настроек"})
			return
		}
		writeJSON(w, http.StatusOK, req)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Метод не поддерживается"})
	}
}
