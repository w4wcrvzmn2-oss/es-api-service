// @title ES API Service
// @version 1.0
// @description API сервис для экспорта данных из MS SQL Server
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:8080
// @BasePath /
// @schemes http https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

package main

import (
	"es_api_service/internal/config"
	"es_api_service/internal/service"
	"log"
	"os"
	"path/filepath"

	_ "es_api_service/docs" // Swagger docs
)

func main() {
	// Переходим в папку рядом с exe, чтобы статика (ClientWeb и т.д.)
	// находилась и при запуске службой (у служб cwd = System32).
	if exePath, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(exePath))
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	if err := service.RunService(cfg); err != nil {
		log.Fatalf("Ошибка запуска сервиса: %v", err)
	}
}
