package logger

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/db"
	"time"
)

// DBLogger представляет логгер, который пишет в базу данных
type DBLogger struct {
	database   *db.Database
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

// snapshotUser копирует поля пользователя на момент вызова (безопасно для async).
func (dbl *DBLogger) snapshotUser() (userID, username, ip, ua *string) {
	if dbl.userID != nil {
		v := *dbl.userID
		userID = &v
	}
	if dbl.username != nil {
		v := *dbl.username
		username = &v
	}
	if dbl.ipAddress != nil {
		v := *dbl.ipAddress
		ip = &v
	}
	if dbl.userAgent != nil {
		v := *dbl.userAgent
		ua = &v
	}
	return
}

// Log записывает лог в базу данных (синхронно — для ошибок).
func (dbl *DBLogger) Log(ctx context.Context, level, category, action, message string, details interface{}, requestMethod, requestPath *string, responseStatus, executionTimeMs *int, errorMessage *string) error {
	return dbl.logWithUser(ctx, dbl.userID, dbl.username, dbl.ipAddress, dbl.userAgent,
		level, category, action, message, details, requestMethod, requestPath, responseStatus, executionTimeMs, errorMessage)
}

func (dbl *DBLogger) logWithUser(
	ctx context.Context,
	userID, username, ipAddress, userAgent *string,
	level, category, action, message string,
	details interface{},
	requestMethod, requestPath *string,
	responseStatus, executionTimeMs *int,
	errorMessage *string,
) error {
	if dbl.database == nil {
		if dbl.fileLogger != nil {
			dbl.fileLogger.writeLog(ParseLogLevel(level), "%s: %s", action, message)
		}
		return nil
	}

	var detailsJSON *string
	if details != nil {
		if jsonBytes, err := json.Marshal(details); err == nil {
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
			@executionTimeMs, @errorMessage, (NOW() AT TIME ZONE 'utc')
		)
	`

	err := dbl.database.GORMWith(ctx).Exec(query,
		sql.Named("userID", userID),
		sql.Named("username", username),
		sql.Named("logLevel", level),
		sql.Named("category", category),
		sql.Named("action", action),
		sql.Named("message", message),
		sql.Named("details", detailsJSON),
		sql.Named("ipAddress", ipAddress),
		sql.Named("userAgent", userAgent),
		sql.Named("requestMethod", requestMethod),
		sql.Named("requestPath", requestPath),
		sql.Named("responseStatus", responseStatus),
		sql.Named("executionTimeMs", executionTimeMs),
		sql.Named("errorMessage", errorMessage),
	).Error

	if dbl.fileLogger != nil && (level == "WARN" || level == "ERROR") {
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

// RequestLogFast — аудит HTTP без блокировки ответа клиенту.
// Успешные GET не пишем в БД (основной тормоз списков).
func (dbl *DBLogger) RequestLogFast(level, category, action, message, requestMethod, requestPath string, responseStatus, executionTimeMs int) {
	if dbl == nil {
		return
	}
	// Не засоряем AuditLog успешными чтениями — это делало UI «вязким».
	if responseStatus < 400 && (requestMethod == "GET" || requestMethod == "HEAD" || requestMethod == "OPTIONS") {
		return
	}

	userID, username, ip, ua := dbl.snapshotUser()
	rm, rp := requestMethod, requestPath
	rs, et := responseStatus, executionTimeMs

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = dbl.logWithUser(ctx, userID, username, ip, ua,
			level, category, action, message, nil, &rm, &rp, &rs, &et, nil)
	}()
}

// RequestLog записывает лог HTTP запроса (совместимость; теперь тоже async).
func (dbl *DBLogger) RequestLog(ctx context.Context, level, category, action, message string, requestMethod, requestPath string, responseStatus, executionTimeMs int, details interface{}) error {
	dbl.RequestLogFast(level, category, action, message, requestMethod, requestPath, responseStatus, executionTimeMs)
	return nil
}
