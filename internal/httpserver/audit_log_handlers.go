package httpserver

import (
	"context"
	"database/sql"
	"es_api_service/internal/models"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// handleGetAuditLogs возвращает список аудит-логов с фильтрацией
func (s *Server) handleGetAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	userID := r.URL.Query().Get("user_id")
	logLevel := r.URL.Query().Get("log_level")
	category := r.URL.Query().Get("category")
	action := r.URL.Query().Get("action")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	limit := 100
	offset := 0
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	query := `
		SELECT
			LogID, UserID, Username, LogLevel, Category, Action, Message,
			Details, IPAddress, UserAgent, RequestMethod, RequestPath,
			ResponseStatus, ExecutionTimeMs, ErrorMessage, CreatedAt
		FROM AuditLog
		WHERE 1=1
	`
	var args []interface{}

	if userID != "" {
		query += " AND (UserID LIKE @userIDPattern OR Username LIKE @userIDPattern)"
		args = append(args, sql.Named("userIDPattern", "%"+userID+"%"))
	}
	if logLevel != "" {
		query += " AND LogLevel = @logLevel"
		args = append(args, sql.Named("logLevel", logLevel))
	}
	if category != "" {
		query += " AND Category = @category"
		args = append(args, sql.Named("category", category))
	}
	if action != "" {
		query += " AND Action = @action"
		args = append(args, sql.Named("action", action))
	}
	if startDate != "" {
		query += " AND CreatedAt >= @startDate"
		args = append(args, sql.Named("startDate", startDate))
	}
	if endDate != "" {
		query += " AND CreatedAt <= @endDate"
		args = append(args, sql.Named("endDate", endDate))
	}

	query += " ORDER BY CreatedAt DESC"
	query += fmt.Sprintf(" OFFSET %d LIMIT %d", offset, limit)

	var logs []models.AuditLog
	err := s.database.GORMWith(ctx).Raw(query, args...).Scan(&logs).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения аудит-логов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения логов: %v", err))
		return
	}
	if logs == nil {
		logs = []models.AuditLog{}
	}

	// Общее количество записей для пагинации
	countQuery := `SELECT COUNT(*) FROM AuditLog WHERE 1=1`
	if userID != "" {
		countQuery += " AND (UserID LIKE @userIDPattern OR Username LIKE @userIDPattern)"
	}
	if logLevel != "" {
		countQuery += " AND LogLevel = @logLevel"
	}
	if category != "" {
		countQuery += " AND Category = @category"
	}
	if action != "" {
		countQuery += " AND Action = @action"
	}
	if startDate != "" {
		countQuery += " AND CreatedAt >= @startDate"
	}
	if endDate != "" {
		countQuery += " AND CreatedAt <= @endDate"
	}

	var totalCount int
	if err := s.database.GORMWith(ctx).Raw(countQuery, args...).Row().Scan(&totalCount); err != nil && s.logger != nil {
		s.logger.Warn("Ошибка получения общего количества логов: %v", err)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":       logs,
		"total":      totalCount,
		"totalCount": totalCount,
		"limit":      limit,
		"offset":     offset,
		"has_more":   offset+limit < totalCount,
	})
}

// handleGetAuditLogStats возвращает статистику по логам
func (s *Server) handleGetAuditLogStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	type levelStat struct {
		LogLevel string
		Count    int
	}
	var byLevel []levelStat
	err := s.database.GORMWith(ctx).Raw(
		`SELECT LogLevel, COUNT(*) AS Count
		 FROM AuditLog
		 WHERE CreatedAt >= DATEADD(day, -7, (NOW() AT TIME ZONE 'utc'))
		 GROUP BY LogLevel
		 ORDER BY LogLevel`,
	).Scan(&byLevel).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения статистики логов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка получения статистики: %v", err))
		return
	}
	stats := make(map[string]int, len(byLevel))
	for _, v := range byLevel {
		stats[v.LogLevel] = v.Count
	}

	type userStat struct {
		Username string `json:"username"`
		Count    int    `json:"count"`
	}
	var userStats []userStat
	err = s.database.GORMWith(ctx).Raw(
		`SELECT Username, COUNT(*) AS Count
		 FROM AuditLog
		 WHERE CreatedAt >= DATEADD(day, -7, (NOW() AT TIME ZONE 'utc'))
		   AND Username IS NOT NULL
		 GROUP BY Username
		 ORDER BY COUNT(*) DESC
LIMIT 10
`,
	).Scan(&userStats).Error
	if err != nil {
		// Не критично — отдадим только by_level.
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"by_level": stats,
			"by_user":  []interface{}{},
		})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"by_level": stats,
		"by_user":  userStats,
	})
}
