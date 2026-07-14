package httpserver

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"es_api_service/internal/sync"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "es_api_service/docs" // Swagger docs

	httpSwagger "github.com/swaggo/http-swagger"
)

// Server представляет HTTP сервер
type Server struct {
	server      *http.Server
	server2     *http.Server // Второй сервер для ClienElf2
	config      *config.Config
	database    *db.Database
	authService *AuthService
	logger      *logger.Logger
	fileLogger  *logger.Logger
	dbLogger    *logger.DBLogger
}

// NewServer создаёт новый HTTP сервер
func NewServer(cfg *config.Config, database *db.Database, fileLogger *logger.Logger) *Server {
	authService := NewAuthService(cfg, database, fileLogger)

	dbLogger := logger.NewDBLogger(database, fileLogger)

	server := &Server{
		config:      cfg,
		database:    database,
		authService: authService,
		logger:      fileLogger,
		fileLogger:  fileLogger,
		dbLogger:    dbLogger,
	}

	mux := http.NewServeMux()
	server.setupRoutes(mux)

	server.server = &http.Server{
		Addr:         cfg.GetAddress(),
		Handler:      server.recoveryMiddleware(mux),
		ReadTimeout:  cfg.GetReadTimeout(),
		WriteTimeout: cfg.GetWriteTimeout(),
		IdleTimeout:  cfg.GetIdleTimeout(),
	}

	// Создаем второй HTTP сервер для ClienElf2 если настроен
	if cfg.HasHTTP2() {
		mux2 := http.NewServeMux()
		server.setupRoutes2(mux2)

		server.server2 = &http.Server{
			Addr:         cfg.GetAddress2(),
			Handler:      server.recoveryMiddleware(mux2),
			ReadTimeout:  cfg.GetReadTimeout(),
			WriteTimeout: cfg.GetWriteTimeout(),
			IdleTimeout:  cfg.GetIdleTimeout(),
		}
	}

	return server
}

// setupRoutes настраивает маршруты для основного сервера (ClientWeb)
func (s *Server) setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)
	mux.HandleFunc("/auth/login", s.corsMiddleware(s.loggingMiddleware(s.authService.Login)))
	mux.HandleFunc("/health", s.corsMiddleware(s.loggingMiddleware(s.healthCheck)))

	s.setupAPIRoutes(mux)
	s.setupStaticFiles(mux)
}

// setupStaticFiles настраивает раздачу статических файлов из папки ClientWeb
func (s *Server) setupStaticFiles(mux *http.ServeMux) {
	staticDir := "./ClientWeb"

	// Обработчик для статических файлов
	staticHandler := func(w http.ResponseWriter, r *http.Request) {
		// НЕ обрабатываем API запросы в статическом обработчике
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if s.fileLogger != nil {
				s.fileLogger.Warn("Статический обработчик получил API запрос: %s %s - это не должно происходить!", r.Method, r.URL.Path)
			}
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		// Применяем CORS
		origin := r.Header.Get("Origin")
		if origin == "" || origin == "null" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		// Определяем путь к файлу
		requestPath := r.URL.Path

		// Если запрашивается корень - используем index.html
		if requestPath == "/" || requestPath == "" {
			requestPath = "/index.html"
		}

		// Если путь не содержит расширение (SPA роутинг) - используем index.html
		if !strings.Contains(filepath.Base(requestPath), ".") {
			requestPath = "/index.html"
		}

		// Полный путь к файлу
		filePath := filepath.Join(staticDir, requestPath)

		// Проверяем существование файла
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Если файл не найден, пробуем index.html (для SPA)
				if requestPath != "/index.html" {
					filePath = filepath.Join(staticDir, "index.html")
					fileInfo, err = os.Stat(filePath)
					if err != nil {
						http.Error(w, "File not found", http.StatusNotFound)
						return
					}
					requestPath = "/index.html"
				} else {
					http.Error(w, "File not found", http.StatusNotFound)
					return
				}
			} else {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		// Проверяем, что это не директория
		if fileInfo.IsDir() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Настраиваем заголовки кеширования — no-cache для всех файлов (dev)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// Определяем Content-Type
		ext := strings.ToLower(filepath.Ext(requestPath))
		switch ext {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case ".json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".gif":
			w.Header().Set("Content-Type", "image/gif")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		// Открываем и отдаем файл
		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer file.Close()

		// Устанавливаем размер файла
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Копируем содержимое файла в ответ
		io.Copy(w, file)
	}

	// Регистрируем обработчик для всех путей (должен быть последним)
	mux.HandleFunc("/", staticHandler)
}

// setupRoutes2 настраивает маршруты для второго сервера (ClienElf2)
func (s *Server) setupRoutes2(mux *http.ServeMux) {
	// Swagger документация
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	// Публичные маршруты
	mux.HandleFunc("/auth/login", s.corsMiddleware(s.loggingMiddleware(s.authService.Login)))
	mux.HandleFunc("/health", s.corsMiddleware(s.loggingMiddleware(s.healthCheck)))

	// Копируем все API маршруты из основного сервера
	s.setupAPIRoutes(mux)

	// Маршруты кабинета поставщика
	s.setupSupplierRoutes(mux)

	// Статические файлы из ClienElf2
	s.setupStaticFiles2(mux)
}

// setupAPIRoutes настраивает API маршруты (общие для обоих серверов)
func (s *Server) setupAPIRoutes(mux *http.ServeMux) {
	tableRoute := func(path, table string) {
		mux.HandleFunc(path, s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.handleTableData(w, r, table)
		})).ServeHTTP)))
	}

	tableRoute("/api/region", "Region")
	tableRoute("/api/es_atc", "es_atc")
	tableRoute("/api/es_country", "es_country")
	tableRoute("/api/es_ef2", "es_ef2")
	tableRoute("/api/es_farmgroup", "es_farmgroup")
	tableRoute("/api/es_firma", "es_firma")
	tableRoute("/api/es_goods_classifier", "es_goods_classifier")
	tableRoute("/api/es_group", "es_group")
	tableRoute("/api/es_instruction", "es_instruction")
	tableRoute("/api/es_mnn", "es_mnn")
	tableRoute("/api/es_phgroup", "es_phgroup")
	tableRoute("/api/es_pku_list", "es_pku_list")
	tableRoute("/api/es_producer", "es_producer")
	tableRoute("/api/es_store_condition", "es_store_condition")
	tableRoute("/api/es_supplier", "es_supplier")

	// Информация о полях таблиц
	mux.HandleFunc("/api/fields/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleFieldsInfo)).ServeHTTP)))

	// Синхронизация справочника препаратов
	mux.HandleFunc("/api/sync/drugs", s.corsMiddleware(s.loggingMiddleware(
		s.authService.JWTMiddleware(
			s.authService.RequireRole("admin")(http.HandlerFunc(s.handleDrugSync)),
		).ServeHTTP,
	)))

	// Поставщики
	mux.HandleFunc("/api/suppliers", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetSuppliers)).ServeHTTP)))
	mux.HandleFunc("/api/suppliers/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSuppliersRouter)).ServeHTTP)))
	mux.HandleFunc("/api/suppliers/create", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleCreateSupplier)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-markup-policies", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSupplierMarkupPoliciesRouter)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-markup-policies/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSupplierMarkupPoliciesRouter)).ServeHTTP)))

	// Точки импорта
	mux.HandleFunc("/api/import-points", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetImportPoints)).ServeHTTP)))
	mux.HandleFunc("/api/import-points/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleImportPointsRouter)).ServeHTTP)))
	mux.HandleFunc("/api/import-points/create", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleCreateImportPoint)).ServeHTTP)))

	// Маппинги полей
	mux.HandleFunc("/api/field-mappings", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetFieldMappings)).ServeHTTP)))
	mux.HandleFunc("/api/field-mappings/save", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSaveFieldMapping)).ServeHTTP)))
	mux.HandleFunc("/api/field-mappings/save-all", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSaveAllFieldMappings)).ServeHTTP)))

	// DBF анализ и загрузка
	mux.HandleFunc("/api/dbf/analyze", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleAnalyzeDBF)).ServeHTTP)))
	mux.HandleFunc("/api/dbf/upload", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleUploadDBF)).ServeHTTP)))
	mux.HandleFunc("/api/target-fields", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetTargetFields)).ServeHTTP)))

	// Импорт файлов
	mux.HandleFunc("/api/import/file", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleImportFile)).ServeHTTP)))
	mux.HandleFunc("/api/invoice-imports", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetInvoiceImports)).ServeHTTP)))

	// Сопоставление прайсов
	mux.HandleFunc("/api/match/invoice-data", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleMatchInvoiceData)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-prices", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSupplierPricesRouter)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-prices/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSupplierPricesRouter)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-prices/summary", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetSupplierPriceSummary)).ServeHTTP)))
	mux.HandleFunc("/api/supplier-prices/create", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleCreateSupplierPrice)).ServeHTTP)))

	// Глобальная статистика
	mux.HandleFunc("/api/stats/global", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGlobalStats)).ServeHTTP)))

	// Поиск препаратов
	mux.HandleFunc("/api/drugs/search", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleSearchDrugs)).ServeHTTP)))

	// Аналоги и синонимы препарата
	mux.HandleFunc("/api/drug/analogs-synonyms", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetDrugAnalogsAndSynonyms)).ServeHTTP)))

	// Инструкции по применению
	mux.HandleFunc("/api/instruction", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetInstruction)).ServeHTTP)))

	// Прайс-листы
	mux.HandleFunc("/api/price-lists", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handlePriceListsRouter)).ServeHTTP)))
	mux.HandleFunc("/api/price-lists/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handlePriceListsRouter)).ServeHTTP)))
	mux.HandleFunc("/api/price-lists/create", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleCreatePriceList)).ServeHTTP)))

	// Логи аудита
	mux.HandleFunc("/api/audit-logs", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetAuditLogs)).ServeHTTP)))
	mux.HandleFunc("/api/audit-logs/stats", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetAuditLogStats)).ServeHTTP)))

	// Заказы
	mux.HandleFunc("/api/orders", s.corsMiddleware(s.loggingMiddleware(
		s.authService.JWTMiddleware(
			s.authService.RequireRole("admin")(http.HandlerFunc(s.handleOrdersRouter)),
		).ServeHTTP,
	)))
	mux.HandleFunc("/api/orders/", s.corsMiddleware(s.loggingMiddleware(
		s.authService.JWTMiddleware(
			s.authService.RequireRole("admin")(http.HandlerFunc(s.handleOrdersRouter)),
		).ServeHTTP,
	)))
	mux.HandleFunc("/api/order-statuses", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleGetOrderStatuses)).ServeHTTP)))

	// Покупатели
	mux.HandleFunc("/api/buyers", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyersRouter)).ServeHTTP)))
	mux.HandleFunc("/api/buyers/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyersRouter)).ServeHTTP)))
	mux.HandleFunc("/api/buyer-users", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyerUsersRouter)).ServeHTTP)))
	mux.HandleFunc("/api/buyer-users/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyerUsersRouter)).ServeHTTP)))
	mux.HandleFunc("/api/buyer-locations", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyerLocationsRouter)).ServeHTTP)))
	mux.HandleFunc("/api/buyer-locations/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyerLocationsRouter)).ServeHTTP)))

	// Внешний API для покупателей
	mux.HandleFunc("/api/buyer/", s.corsMiddleware(s.loggingMiddleware(s.authService.JWTMiddleware(http.HandlerFunc(s.handleBuyerOrdersRouter)).ServeHTTP)))
}

// setupStaticFiles2 настраивает раздачу статических файлов из папки ClienElf2
func (s *Server) setupStaticFiles2(mux *http.ServeMux) {
	staticDir := s.config.HTTP2.StaticDir
	if staticDir == "" {
		staticDir = "./ClienElf2"
	}

	// Обработчик для статических файлов
	staticHandler := func(w http.ResponseWriter, r *http.Request) {
		// НЕ обрабатываем API запросы в статическом обработчике
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if s.fileLogger != nil {
				s.fileLogger.Warn("Статический обработчик получил API запрос: %s %s - это не должно происходить!", r.Method, r.URL.Path)
			}
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		// Применяем CORS
		origin := r.Header.Get("Origin")
		if origin == "" || origin == "null" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		// Определяем путь к файлу
		requestPath := r.URL.Path

		// Если запрашивается корень - используем index.html
		if requestPath == "/" || requestPath == "" {
			requestPath = "/index.html"
		}

		// Если путь не содержит расширение (SPA роутинг) - используем index.html
		if !strings.Contains(filepath.Base(requestPath), ".") {
			requestPath = "/index.html"
		}

		// Полный путь к файлу
		filePath := filepath.Join(staticDir, requestPath)

		// Проверяем существование файла
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Если файл не найден, пробуем index.html (для SPA)
				if requestPath != "/index.html" {
					filePath = filepath.Join(staticDir, "index.html")
					fileInfo, err = os.Stat(filePath)
					if err != nil {
						http.Error(w, "File not found", http.StatusNotFound)
						return
					}
					requestPath = "/index.html"
				} else {
					http.Error(w, "File not found", http.StatusNotFound)
					return
				}
			} else {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		// Проверяем, что это не директория
		if fileInfo.IsDir() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Настраиваем заголовки кеширования — no-cache для всех файлов (dev)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		// Определяем Content-Type
		ext := strings.ToLower(filepath.Ext(requestPath))
		switch ext {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case ".json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".gif":
			w.Header().Set("Content-Type", "image/gif")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		// Открываем и отдаем файл
		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer file.Close()

		// Устанавливаем размер файла
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Копируем содержимое файла в ответ
		io.Copy(w, file)
	}

	// Обработчик для статических файлов кабинета поставщика (ClienSupplier)
	supplierDir := "./ClienSupplier"
	supplierHandler := func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || origin == "null" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}

		reqPath := strings.TrimPrefix(r.URL.Path, "/supplier")
		if reqPath == "" || reqPath == "/" {
			reqPath = "/index.html"
		}
		if !strings.Contains(filepath.Base(reqPath), ".") {
			reqPath = "/index.html"
		}

		filePath := filepath.Join(supplierDir, reqPath)
		fileInfo, err := os.Stat(filePath)
		if err != nil || fileInfo.IsDir() {
			filePath = filepath.Join(supplierDir, "index.html")
			fileInfo, err = os.Stat(filePath)
			if err != nil {
				http.Error(w, "File not found", http.StatusNotFound)
				return
			}
			reqPath = "/index.html"
		}

		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")

		ext := strings.ToLower(filepath.Ext(reqPath))
		switch ext {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case ".json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer file.Close()
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
		io.Copy(w, file)
	}
	mux.HandleFunc("/supplier/", supplierHandler)

	// Регистрируем обработчик для всех путей (должен быть последним)
	mux.HandleFunc("/", staticHandler)
}

// healthCheck обрабатывает проверку здоровья сервиса
// @Summary Проверка состояния сервиса
// @Description Возвращает статус работы сервиса и подключения к БД
// @Tags system
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "Сервис работает нормально"
// @Failure 500 {object} map[string]interface{} "Ошибка сервиса"
// @Router /health [get]
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Проверяем соединение с БД если оно есть
	if s.database != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		if err := s.database.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "Нет соединения с базой данных",
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// GzipResponseWriter обёртка для gzip сжатия ответов
type GzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

// Write записывает данные через gzip компрессор
func (w *GzipResponseWriter) Write(data []byte) (int, error) {
	return w.writer.Write(data)
}

// NewGzipResponseWriter создаёт ResponseWriter с gzip сжатием
func NewGzipResponseWriter(w http.ResponseWriter) *GzipResponseWriter {
	gzWriter := gzip.NewWriter(w)

	return &GzipResponseWriter{
		ResponseWriter: w,
		writer:         gzWriter,
	}
}

// Close закрывает gzip writer
func (w *GzipResponseWriter) Close() error {
	return w.writer.Close()
}

// corsMiddleware добавляет CORS заголовки
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "null" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Обрабатываем preflight запросы - возвращаем ответ сразу, не вызывая next
		if r.Method == http.MethodOptions {
			if s.logger != nil {
				s.logger.Info("CORS preflight запрос для %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// recoveryMiddleware перехватывает панику в любом handler'е
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.logger != nil {
					s.logger.Error("PANIC RECOVERY [%s %s]: %v", r.Method, r.URL.Path, rec)
				}
				http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// timeoutMiddleware добавляет таймаут контекста ко всем API-запросам
func (s *Server) timeoutMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// loggingMiddleware логирует все HTTP запросы
func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		if s.logger != nil {
			s.logger.Info("HTTP запрос: %s %s от %s", r.Method, r.URL.Path, r.RemoteAddr)
		}

		// Извлекаем информацию о пользователе из JWT токена, если есть
		var userID, username *string
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			// Используем ValidateToken из authService
			if claims, err := s.authService.ValidateToken(token); err == nil {
				if claims.Username != "" {
					username = &claims.Username
					userID = &claims.Username
				}
			}
		}

		// Устанавливаем контекст пользователя для dbLogger
		ipAddr := r.RemoteAddr
		userAgent := r.Header.Get("User-Agent")
		if s.dbLogger != nil {
			s.dbLogger.SetUserContext(userID, username, &ipAddr, &userAgent)
		}

		// Создаём ResponseWriter для отслеживания статуса ответа
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		if s.logger != nil {
			s.logger.Info("HTTP ответ: %s %s -> %d (%v)", r.Method, r.URL.Path, wrapped.statusCode, duration)
		}

		// Записываем лог в базу данных
		if s.dbLogger != nil {
			ctx := r.Context()
			executionTimeMs := int(duration.Milliseconds())
			requestPath := r.URL.Path
			requestMethod := r.Method

			// Определяем категорию и действие на основе пути запроса
			category := "HTTP_REQUEST"
			action := requestMethod + "_" + strings.TrimPrefix(requestPath, "/api/")
			if strings.HasPrefix(requestPath, "/api/") {
				// Убираем /api/ и берем первую часть как действие
				pathParts := strings.Split(strings.TrimPrefix(requestPath, "/api/"), "/")
				if len(pathParts) > 0 && pathParts[0] != "" {
					action = requestMethod + "_" + pathParts[0]
				}
			} else {
				action = requestMethod + "_" + requestPath
			}

			message := fmt.Sprintf("%s %s", requestMethod, requestPath)
			level := "INFO"
			if wrapped.statusCode >= 400 && wrapped.statusCode < 500 {
				level = "WARN"
			} else if wrapped.statusCode >= 500 {
				level = "ERROR"
			}

			var errorMsg *string
			if wrapped.statusCode >= 400 {
				errMsg := fmt.Sprintf("HTTP %d", wrapped.statusCode)
				errorMsg = &errMsg
			}

			_ = s.dbLogger.RequestLog(ctx, level, category, action, message,
				requestMethod, requestPath, wrapped.statusCode, executionTimeMs, nil)
			if errorMsg != nil && s.dbLogger != nil {
				// Дополнительно логируем ошибки
				_ = s.dbLogger.Error(ctx, category, action+"_ERROR", message, nil, errorMsg)
			}
		}

		// Очищаем контекст пользователя
		if s.dbLogger != nil {
			s.dbLogger.ClearUserContext()
		}
	}
}

// responseWriter обёртка для отслеживания статуса ответа
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// handleTableData обрабатывает запросы к таблицам с потоковой выдачей
// @Summary Получение данных из таблицы
// @Description Возвращает данные из указанной таблицы с поддержкой пагинации и фильтрации
// @Tags data
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param tableName path string true "Имя таблицы" Enums(Region)
// @Param limit query int false "Количество записей (по умолчанию 100)" default(100)
// @Param offset query int false "Смещение (по умолчанию 0)" default(0)
// @Param updated_after query string false "Фильтр по дате обновления (RFC3339)"
// @Param id_gt query int false "Фильтр по ID больше указанного"
// @Param columns query string false "Список колонок через запятую"
// @Success 200 {array} map[string]interface{} "Массив записей из таблицы"
// @Failure 400 {object} map[string]interface{} "Некорректный запрос"
// @Failure 401 {object} map[string]interface{} "Не авторизован"
// @Failure 500 {object} map[string]interface{} "Внутренняя ошибка сервера"
// @Router /api/{tableName} [get]
func (s *Server) handleTableData(w http.ResponseWriter, r *http.Request, tableName string) {
	if r.Method != http.MethodGet {
		if s.logger != nil {
			s.logger.Warn("Неподдерживаемый метод %s для %s от %s", r.Method, r.URL.Path, r.RemoteAddr)
		}
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Проверяем наличие подключения к БД
	if s.database == nil {
		if s.logger != nil {
			s.logger.Error("Нет подключения к базе данных для обработки запроса %s", tableName)
		}
		s.sendJSONError(w, http.StatusServiceUnavailable, "Сервис базы данных недоступен")
		return
	}

	// Проверяем параметр count
	countOnly := r.URL.Query().Get("count") == "true"

	// Парсинг параметров запроса
	columnsParam := r.URL.Query().Get("columns")

	// Если columns не указан и включено использование полей по умолчанию
	if columnsParam == "" && s.config.FieldsConfig != nil && s.config.FieldsConfig.ShouldUseDefaultFields() {
		defaultFields := s.config.FieldsConfig.GetDefaultFields(tableName)
		if s.logger != nil {
			s.logger.Debug("Конфигурация полей: FieldsConfig=%v, ShouldUseDefaultFields=%v, defaultFields=%v",
				s.config.FieldsConfig != nil,
				s.config.FieldsConfig.ShouldUseDefaultFields(),
				defaultFields)
		}
		if len(defaultFields) > 0 {
			columnsParam = strings.Join(defaultFields, ",")
			if s.logger != nil {
				s.logger.Info("Использование полей по умолчанию для таблицы %s: %s", tableName, columnsParam)
			}
		}
	}

	params, err := db.ParseQueryParams(
		r.URL.Query().Get("limit"),
		r.URL.Query().Get("offset"),
		r.URL.Query().Get("updated_after"),
		r.URL.Query().Get("id_gt"),
		columnsParam,
	)
	if err != nil {
		s.sendJSONError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка параметров: %v", err))
		return
	}

	// Создаём контекст с таймаутом
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Проверяем существование таблицы
	exists, err := s.database.TableExists(ctx, tableName)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка проверки таблицы %s: %v", tableName, err)
		}
		s.sendJSONError(w, http.StatusInternalServerError, "Ошибка базы данных")
		return
	}
	if !exists {
		if s.logger != nil {
			s.logger.Warn("Таблица %s не найдена", tableName)
		}
		s.sendJSONError(w, http.StatusNotFound, fmt.Sprintf("Таблица %s не найдена", tableName))
		return
	}

	// Получаем колонки таблицы
	tableColumns, err := s.database.GetTableColumns(ctx, tableName)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения колонок таблицы %s: %v", tableName, err)
		}
		s.sendJSONError(w, http.StatusInternalServerError, "Ошибка базы данных")
		return
	}

	// Если запрашивается только количество записей
	if countOnly {
		countQuery, countArgs, err := db.BuildCountQuery(tableName, params, tableColumns)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка построения запроса count для %s: %v", tableName, err)
			}
			s.sendJSONError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка запроса count: %v", err))
			return
		}

		if s.logger != nil {
			s.logger.Info("Выполнение запроса count для таблицы %s", tableName)
			s.logger.Debug("SQL запрос count: %s", countQuery)
			s.logger.Debug("Аргументы запроса count: %v", countArgs)
		}

		var count int
		err = s.database.QueryRowContext(ctx, countQuery, countArgs...).Scan(&count)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка выполнения запроса count к %s: %v", tableName, err)
				s.logger.Error("Проблемный SQL: %s", countQuery)
			}
			s.sendJSONError(w, http.StatusInternalServerError, "Ошибка выполнения запроса count")
			return
		}

		// Возвращаем только количество
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Total-Count", fmt.Sprintf("%d", count))
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count": count,
			"table": tableName,
		})
		return
	}

	// Получаем детальную информацию о колонках для правильной обработки типов
	_, err = s.database.GetTableColumnsWithTypes(ctx, tableName)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Не удалось получить типы колонок для %s, используем базовую обработку: %v", tableName, err)
		}
	}

	// Строим SQL запрос
	query, args, err := db.BuildSelectQuery(tableName, params, tableColumns)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка построения запроса для %s: %v", tableName, err)
		}
		s.sendJSONError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка запроса: %v", err))
		return
	}

	if s.logger != nil {
		s.logger.Info("Выполнение запроса к таблице %s с параметрами: limit=%d, offset=%d", tableName, params.Limit, params.Offset)
		s.logger.Debug("SQL запрос: %s", query)
		s.logger.Debug("Аргументы запроса: %v", args)
	}

	// Выполняем запрос
	rows, err := s.database.QueryRowsContext(ctx, query, args...)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выполнения запроса к %s: %v", tableName, err)
			s.logger.Error("Проблемный SQL: %s", query)
			s.logger.Error("Аргументы: %v", args)
		}
		s.sendJSONError(w, http.StatusInternalServerError, "Ошибка выполнения запроса")
		return
	}
	defer rows.Close()

	// Отправляем данные с HTTP сжатием
	s.streamTableDataWithCompression(w, r, rows)
}

// streamTableDataWithCompression отправляет данные с HTTP сжатием
func (s *Server) streamTableDataWithCompression(w http.ResponseWriter, r *http.Request, rows *sql.Rows) {
	// Устанавливаем заголовки
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	var writer io.Writer = w
	var closer io.Closer

	// Проверяем поддержку сжатия клиентом
	acceptEncoding := r.Header.Get("Accept-Encoding")
	if strings.Contains(acceptEncoding, "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gzWriter := NewGzipResponseWriter(w)
		writer = gzWriter
		closer = gzWriter
		defer closer.Close()
	}

	// Создаём JSON streamer
	streamer := NewJSONStreamer(writer)

	// Записываем начало массива
	if err := streamer.WriteArrayStart(); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка записи начала массива: %v", err)
		}
		return
	}

	rowCount := 0
	// Потоково записываем строки
	for rows.Next() {
		if err := streamer.WriteRowAsJSON(rows, nil); err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка записи строки: %v", err)
			}
			return
		}
		rowCount++
	}

	// Проверяем ошибки после итерации
	if err := rows.Err(); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка при итерации строк: %v", err)
		}
		return
	}

	// Записываем конец массива
	if err := streamer.WriteArrayEnd(); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка записи конца массива: %v", err)
		}
		return
	}

	if s.logger != nil {
		s.logger.Info("Успешно отправлено %d записей", rowCount)
	}
}

// sendJSONError отправляет ошибку в формате JSON (без сжатия)
func (s *Server) sendJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

// handleFieldsInfo обрабатывает запросы информации о полях таблиц
// @Summary Информация о полях таблицы
// @Description Возвращает информацию о доступных полях для указанной таблицы
// @Tags fields
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param tableName path string true "Имя таблицы"
// @Success 200 {object} map[string]interface{} "Информация о полях таблицы"
// @Failure 400 {object} ErrorResponse "Некорректный запрос"
// @Failure 401 {object} ErrorResponse "Не авторизован"
// @Failure 404 {object} ErrorResponse "Таблица не найдена"
// @Router /api/fields/{tableName} [get]
func (s *Server) handleFieldsInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Извлекаем имя таблицы из URL
	path := strings.TrimPrefix(r.URL.Path, "/api/fields/")
	if path == "" {
		s.sendJSONError(w, http.StatusBadRequest, "Не указано имя таблицы")
		return
	}

	// Проверяем наличие конфигурации полей
	if s.config.FieldsConfig == nil {
		s.sendJSONError(w, http.StatusInternalServerError, "Конфигурация полей недоступна")
		return
	}

	// Получаем конфигурацию для таблицы
	tableConfig, exists := s.config.FieldsConfig.GetTableConfig(path)
	if !exists {
		s.sendJSONError(w, http.StatusNotFound, fmt.Sprintf("Таблица %s не найдена в конфигурации", path))
		return
	}

	// Формируем ответ
	response := map[string]interface{}{
		"table_name":         path,
		"description":        tableConfig.Description,
		"default_fields":     tableConfig.DefaultFields,
		"all_fields":         tableConfig.AllFields,
		"field_descriptions": tableConfig.FieldDescriptions,
	}

	// Отправляем ответ
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDrugSync обрабатывает запросы синхронизации всех таблиц es_*
// @Summary Синхронизация всех таблиц es_*
// @Description Запускает синхронизацию всех таблиц, начинающихся с es_, из базы eplus_work
// @Tags sync
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Синхронизация запущена"
// @Failure 400 {object} ErrorResponse "Некорректный запрос"
// @Failure 401 {object} ErrorResponse "Не авторизован"
// @Failure 500 {object} ErrorResponse "Внутренняя ошибка сервера"
// @Router /api/sync/drugs [post]
func (s *Server) handleDrugSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	// Проверяем наличие подключения к БД
	if s.database == nil {
		s.sendJSONError(w, http.StatusServiceUnavailable, "Сервис базы данных недоступен")
		return
	}

	// Запускаем синхронизацию в отдельной горутине
	go func() {
		if s.logger != nil {
			s.logger.Info("Запуск ручной синхронизации всех таблиц es_*")
		}

		// Создаем новый экземпляр UniversalSync для ручной синхронизации
		// Получаем подключения к базам данных
		sourceDB, err := db.NewSourceDatabase(s.config)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка подключения к sourceDB для синхронизации: %v", err)
			}
			return
		}
		defer sourceDB.Close()

		// Создаем UniversalSync и запускаем синхронизацию
		universalSync := sync.NewUniversalSync(sourceDB, s.database, s.fileLogger, s.config)
		// Используем контекст без таймаута для синхронизации ES_EF2 (может занять много времени)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = universalSync.SyncAllTables(ctx)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка синхронизации: %v", err)
			}
		} else {
			if s.logger != nil {
				s.logger.Info("Ручная синхронизация завершена успешно")
			}
		}
	}()

	// Отправляем ответ о запуске синхронизации
	response := map[string]interface{}{
		"message": "Синхронизация всех таблиц es_* запущена",
		"status":  "started",
		"time":    time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Start запускает HTTP сервер
func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

// Start2 запускает второй HTTP сервер (для ClienElf2)
func (s *Server) Start2() error {
	if s.server2 == nil {
		return nil
	}
	return s.server2.ListenAndServe()
}

// HasServer2 возвращает true если настроен второй сервер
func (s *Server) HasServer2() bool {
	return s.server2 != nil
}

// GetServer2Addr возвращает адрес второго сервера
func (s *Server) GetServer2Addr() string {
	if s.server2 == nil {
		return ""
	}
	return s.server2.Addr
}

// Stop останавливает HTTP сервер
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Stop2 останавливает второй HTTP сервер
func (s *Server) Stop2(ctx context.Context) error {
	if s.server2 == nil {
		return nil
	}
	return s.server2.Shutdown(ctx)
}
