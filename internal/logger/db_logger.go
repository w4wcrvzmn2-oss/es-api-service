package logger

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/db"
)

// DBLogger представляет логгер, который пишет в базу данных
type DBLogger struct {
	database *db.Database
	fileLogger *Logger
	userID     *string
	username   *string
	ipAddress  *string
	userAgent  *string
}

// NewDBLogger создает новый DB логгер
func NewDBLogger(database *db.Database, fileLogger *Logger) *DBLogger {
	return &DBLogger{
		database:   database,
		fileLogger: fileLogger,
	}
}

// SetUserContext устанавливает контекст пользователя для логов
func (dbl *DBLogger) SetUserContext(userID, username, ipAddress, userAgent *string) {
	dbl.userID = userID
	dbl.username = username
	dbl.ipAddress = ipAddress
	dbl.userAgent = userAgent
}

// ClearUserContext очищает контекст пользователя
func (dbl *DBLogger) ClearUserContext() {
	dbl.userID = nil
	dbl.username = nil
	dbl.ipAddress = nil
	dbl.userAgent = nil
}

// Log записывает лог в базу данных
func (dbl *DBLogger) Log(ctx context.Context, level, category, action, message string, details interface{}, requestMethod, requestPath *string, responseStatus, executionTimeMs *int, errorMessage *string) error {
	if dbl.database == nil {
		// Если БД недоступна, пишем только в файл
		if dbl.fileLogger != nil {
			dbl.fileLogger.writeLog(ParseLogLevel(level), "%s: %s", action, message)
		}
		return nil
	}

	// Формируем JSON для details
	var detailsJSON *string
	if details != nil {
		jsonBytes, err := json.Marshal(details)
		if err == nil {
			jsonStr := string(jsonBytes)
			detailsJSON = &jsonStr
		}
	}

	query := `
		INSERT INTO AuditLog (
			UserID, Username, LogLevel, Category, Action, Message, Details,
			IPAddress, UserAgent, RequestMethod, RequestPath, ResponseStatus,
			ExecutionTimeMs, ErrorMessage, CreatedAt
		)
		VALUES (
			@userID, @username, @logLevel, @category, @action, @message, @details,
			@ipAddress, @userAgent, @requestMethod, @requestPath, @responseStatus,
			@executionTimeMs, @errorMessage, GETUTCDATE()
		)
	`

	err := dbl.database.GORMWith(ctx).Exec(query,
		sql.Named("userID", dbl.userID),
		sql.Named("username", dbl.username),
		sql.Named("logLevel", level),
		sql.Named("category", category),
		sql.Named("action", action),
		sql.Named("message", message),
		sql.Named("details", detailsJSON),
		sql.Named("ipAddress", dbl.ipAddress),
		sql.Named("userAgent", dbl.userAgent),
		sql.Named("requestMethod", requestMethod),
		sql.Named("requestPath", requestPath),
		sql.Named("responseStatus", responseStatus),
		sql.Named("executionTimeMs", executionTimeMs),
		sql.Named("errorMessage", errorMessage),
	).Error

	// Всегда пишем в файл тоже
	if dbl.fileLogger != nil {
		dbl.fileLogger.writeLog(ParseLogLevel(level), "[%s] %s: %s", category, action, message)
	}

	return err
}

// Info записывает информационный лог
func (dbl *DBLogger) Info(ctx context.Context, category, action, message string, details interface{}) error {
	return dbl.Log(ctx, "INFO", category, action, message, details, nil, nil, nil, nil, nil)
}

// Warn записывает предупреждение
func (dbl *DBLogger) Warn(ctx context.Context, category, action, message string, details interface{}) error {
	return dbl.Log(ctx, "WARN", category, action, message, details, nil, nil, nil, nil, nil)
}

// Error записывает ошибку
func (dbl *DBLogger) Error(ctx context.Context, category, action, message string, details interface{}, errorMessage *string) error {
	return dbl.Log(ctx, "ERROR", category, action, message, nil, nil, nil, nil, nil, errorMessage)
}

// RequestLog записывает лог HTTP запроса
func (dbl *DBLogger) RequestLog(ctx context.Context, level, category, action, message string, requestMethod, requestPath string, responseStatus, executionTimeMs int, details interface{}) error {
	return dbl.Log(ctx, level, category, action, message, details, 
		&requestMethod, &requestPath, &responseStatus, &executionTimeMs, nil)
}

