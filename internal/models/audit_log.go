package models

import (
	"database/sql"
	"time"
)

// AuditLog представляет запись аудит-лога
type AuditLog struct {
	LogID           int64     `json:"log_id" gorm:"primaryKey;autoIncrement"`
	UserID          *string   `json:"user_id,omitempty" gorm:"type:uniqueidentifier"`
	Username        *string   `json:"username,omitempty"`
	LogLevel        string    `json:"log_level"`
	Category        *string   `json:"category,omitempty"`
	Action          *string   `json:"action,omitempty"`
	Message         string    `json:"message"`
	Details         *string   `json:"details,omitempty"`
	IPAddress       *string   `json:"ip_address,omitempty"`
	UserAgent       *string   `json:"user_agent,omitempty"`
	RequestMethod   *string   `json:"request_method,omitempty"`
	RequestPath     *string   `json:"request_path,omitempty"`
	ResponseStatus  *int      `json:"response_status,omitempty"`
	ExecutionTimeMs *int      `json:"execution_time_ms,omitempty"`
	ErrorMessage    *string   `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
}

// AuditLogRequest представляет запрос на получение логов
type AuditLogRequest struct {
	UserID    *string   `json:"user_id,omitempty"`
	LogLevel  *string   `json:"log_level,omitempty"`
	Category  *string   `json:"category,omitempty"`
	Action    *string   `json:"action,omitempty"`
	StartDate *string   `json:"start_date,omitempty"`
	EndDate   *string   `json:"end_date,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Offset    int       `json:"offset,omitempty"`
}

// ScanAuditLog сканирует AuditLog из результата запроса
func ScanAuditLog(rows *sql.Rows) (*AuditLog, error) {
	var log AuditLog
	var userID, username, category, action, details, ipAddress, userAgent sql.NullString
	var requestMethod, requestPath, errorMessage sql.NullString
	var responseStatus, executionTimeMs sql.NullInt64
	var createdAt time.Time

	err := rows.Scan(
		&log.LogID,
		&userID,
		&username,
		&log.LogLevel,
		&category,
		&action,
		&log.Message,
		&details,
		&ipAddress,
		&userAgent,
		&requestMethod,
		&requestPath,
		&responseStatus,
		&executionTimeMs,
		&errorMessage,
		&createdAt,
	)
	if err != nil {
		return nil, err
	}

	if userID.Valid {
		log.UserID = &userID.String
	}
	if username.Valid {
		log.Username = &username.String
	}
	if category.Valid {
		log.Category = &category.String
	}
	if action.Valid {
		log.Action = &action.String
	}
	if details.Valid {
		log.Details = &details.String
	}
	if ipAddress.Valid {
		log.IPAddress = &ipAddress.String
	}
	if userAgent.Valid {
		log.UserAgent = &userAgent.String
	}
	if requestMethod.Valid {
		log.RequestMethod = &requestMethod.String
	}
	if requestPath.Valid {
		log.RequestPath = &requestPath.String
	}
	if responseStatus.Valid {
		status := int(responseStatus.Int64)
		log.ResponseStatus = &status
	}
	if executionTimeMs.Valid {
		ms := int(executionTimeMs.Int64)
		log.ExecutionTimeMs = &ms
	}
	if errorMessage.Valid {
		log.ErrorMessage = &errorMessage.String
	}
	log.CreatedAt = createdAt

	return &log, nil
}

