package service

import (
	"context"
	"es_api_service/internal/config"
	"es_api_service/internal/db"
	"es_api_service/internal/dbfimport"
	"es_api_service/internal/httpserver"
	"es_api_service/internal/logger"
	"es_api_service/internal/matching"
	"es_api_service/internal/sync"
	"os"
	"time"

	"github.com/kardianos/service"
)

// ESAPIService представляет Windows-сервис
type ESAPIService struct {
	config                *config.Config
	httpServer            *httpserver.Server
	database              *db.Database
	sourceDB              *db.Database
	universalSync         *sync.UniversalSync
	priceListScheduler    *sync.PriceListScheduler
	importPointScheduler  *sync.ImportPointScheduler
	logger                service.Logger
	fileLogger            *logger.Logger
}

// NewESAPIService создаёт новый экземпляр сервиса
func NewESAPIService(cfg *config.Config) *ESAPIService {
	return &ESAPIService{
		config: cfg,
	}
}

// Start запускает сервис
func (s *ESAPIService) Start(srv service.Service) error {
	if s.logger == nil {
		s.logger, _ = srv.Logger(nil)
	}

	// Инициализация файлового логирования
	if s.config.Logging.Enabled {
		var err error
		s.fileLogger, err = logger.NewLogger("es_api_service", s.config.Logging.LogDir, s.config.Logging.Level)
		if err != nil {
			s.logger.Errorf("Ошибка инициализации файлового логирования: %v", err)
			return err
		}
		s.fileLogger.Info("Файловое логирование инициализировано в директории: %s", s.config.Logging.LogDir)
	}

	s.logger.Info("Запуск ES API Service...")
	if s.fileLogger != nil {
		s.fileLogger.Info("Запуск ES API Service...")
	}

	// Инициализация базы данных
	var err error
	s.database, err = db.NewDatabase(s.config)
	if err != nil {
		s.logger.Errorf("Ошибка подключения к базе данных: %v", err)
		if s.fileLogger != nil {
			s.fileLogger.Error("Ошибка подключения к базе данных: %v", err)
		}
		s.logger.Info("Запуск без подключения к БД для тестирования HTTP сервера")
		if s.fileLogger != nil {
			s.fileLogger.Info("Запуск без подключения к БД для тестирования HTTP сервера")
		}
	} else {
		// Проверка соединения с БД
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.database.Ping(ctx); err != nil {
			s.logger.Errorf("Не удалось подключиться к базе данных: %v", err)
			if s.fileLogger != nil {
				s.fileLogger.Error("Не удалось подключиться к базе данных: %v", err)
			}
			s.logger.Info("Запуск без подключения к БД для тестирования HTTP сервера")
			if s.fileLogger != nil {
				s.fileLogger.Info("Запуск без подключения к БД для тестирования HTTP сервера")
			}
			s.database = nil // Сбрасываем соединение
		} else {
			s.logger.Info("Подключение к базе данных успешно установлено")
			if s.fileLogger != nil {
				s.fileLogger.Info("Подключение к базе данных успешно установлено")
			}
		}
	}

	// Инициализация подключения к базе eplus_work для синхронизации
	s.sourceDB, err = db.NewSourceDatabase(s.config)
	if err != nil {
		s.logger.Errorf("Ошибка подключения к базе eplus_work: %v", err)
		if s.fileLogger != nil {
			s.fileLogger.Error("Ошибка подключения к базе eplus_work: %v", err)
		}
		s.logger.Info("Синхронизация справочника препаратов отключена")
		if s.fileLogger != nil {
			s.fileLogger.Info("Синхронизация справочника препаратов отключена")
		}
	} else {
		// Проверка соединения с source БД
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := s.sourceDB.Ping(ctx); err != nil {
			s.logger.Errorf("Не удалось подключиться к базе eplus_work: %v", err)
			if s.fileLogger != nil {
				s.fileLogger.Error("Не удалось подключиться к базе eplus_work: %v", err)
			}
			s.logger.Info("Синхронизация справочника препаратов отключена")
			if s.fileLogger != nil {
				s.fileLogger.Info("Синхронизация справочника препаратов отключена")
			}
			s.sourceDB = nil
		} else {
			s.logger.Info("Подключение к базе eplus_work успешно установлено")
			if s.fileLogger != nil {
				s.fileLogger.Info("Подключение к базе eplus_work успешно установлено")
			}

			// Инициализация универсальной синхронизации всех таблиц es_*
			if s.database != nil {
				s.universalSync = sync.NewUniversalSync(s.sourceDB, s.database, s.fileLogger, s.config)
				s.universalSync.StartScheduler()
				s.logger.Info("Планировщик универсальной синхронизации таблиц es_* запущен")
				if s.fileLogger != nil {
					s.fileLogger.Info("Планировщик универсальной синхронизации таблиц es_* запущен")
				}
			}
		}
	}

	// Инициализация HTTP сервера
	s.httpServer = httpserver.NewServer(s.config, s.database, s.fileLogger)

	// Очистка зависших импортов при старте сервиса
	if s.database != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cleanupQuery := `
			UPDATE InvoiceImport 
			SET ImportStatus = 'FAILED',
			    ErrorMessage = 'Импорт прерван из-за перезапуска сервиса',
			    CompletedAt = GETUTCDATE()
			WHERE ImportStatus = 'PROCESSING'
		`
		result, err := s.database.ExecContext(ctx, cleanupQuery)
		if err != nil {
			if s.fileLogger != nil {
				s.fileLogger.Warn("Ошибка очистки зависших импортов при старте: %v", err)
			}
		} else {
			rowsAffected, _ := result.RowsAffected()
			if rowsAffected > 0 {
				if s.fileLogger != nil {
					s.fileLogger.Info("Очищено зависших импортов при старте сервиса: %d", rowsAffected)
				}
			}
		}
	}

	// Инициализация планировщика обновления прайс-листов
	if s.database != nil {
		// Создаем импортер и сопоставитель для планировщика
		importer := dbfimport.NewDBFImporter(s.database, s.fileLogger)
		matcher := matching.NewPriceMatcher(s.database, s.fileLogger)

		s.priceListScheduler = sync.NewPriceListScheduler(s.database, s.fileLogger, importer, matcher)
		s.priceListScheduler.Start()
		s.logger.Info("Планировщик обновления прайс-листов запущен")
		if s.fileLogger != nil {
			s.fileLogger.Info("Планировщик обновления прайс-листов запущен")
		}

		s.importPointScheduler = sync.NewImportPointScheduler(s.database, s.fileLogger, importer, matcher)
		s.importPointScheduler.Start()
		s.logger.Info("Планировщик забора файлов из точек импорта запущен")
		if s.fileLogger != nil {
			s.fileLogger.Info("Планировщик забора файлов из точек импорта запущен")
		}
	}

	// Запуск HTTP сервера в отдельной горутине
	go func() {
		if err := s.httpServer.Start(); err != nil {
			s.logger.Errorf("Ошибка HTTP сервера: %v", err)
			if s.fileLogger != nil {
				s.fileLogger.Error("Ошибка HTTP сервера: %v", err)
			}
		}
	}()

	// Запуск второго HTTP сервера (ClienElf2) если настроен
	if s.httpServer.HasServer2() {
		go func() {
			if err := s.httpServer.Start2(); err != nil {
				s.logger.Errorf("Ошибка HTTP сервера 2 (ClienElf2): %v", err)
				if s.fileLogger != nil {
					s.fileLogger.Error("Ошибка HTTP сервера 2 (ClienElf2): %v", err)
				}
			}
		}()
		s.logger.Infof("HTTP сервер 2 (ClienElf2) запущен на %s", s.httpServer.GetServer2Addr())
		if s.fileLogger != nil {
			s.fileLogger.Info("HTTP сервер 2 (ClienElf2) запущен на %s", s.httpServer.GetServer2Addr())
		}
	}

	s.logger.Infof("ES API Service запущен на %s", s.config.GetAddress())
	if s.fileLogger != nil {
		s.fileLogger.Info("ES API Service запущен на %s", s.config.GetAddress())
	}
	return nil
}

// Stop останавливает сервис
func (s *ESAPIService) Stop(srv service.Service) error {
	s.logger.Info("Остановка ES API Service...")
	if s.fileLogger != nil {
		s.fileLogger.Info("Остановка ES API Service...")
	}

	// Остановка планировщиков
	if s.importPointScheduler != nil {
		s.importPointScheduler.Stop()
	}
	if s.priceListScheduler != nil {
		s.priceListScheduler.Stop()
	}
	if s.universalSync != nil {
		s.universalSync.Stop()
	}
	if s.logger != nil {
		s.logger.Info("Планировщики остановлены")
	}
	if s.fileLogger != nil {
		s.fileLogger.Info("Планировщики остановлены")
	}

	// Остановка HTTP серверов
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Остановка второго сервера
		if s.httpServer.HasServer2() {
			if err := s.httpServer.Stop2(ctx); err != nil {
				s.logger.Errorf("Ошибка при остановке HTTP сервера 2 (ClienElf2): %v", err)
				if s.fileLogger != nil {
					s.fileLogger.Error("Ошибка при остановке HTTP сервера 2 (ClienElf2): %v", err)
				}
			}
		}

		if err := s.httpServer.Stop(ctx); err != nil {
			s.logger.Errorf("Ошибка при остановке HTTP сервера: %v", err)
			if s.fileLogger != nil {
				s.fileLogger.Error("Ошибка при остановке HTTP сервера: %v", err)
			}
		}
	}

	// Закрытие соединений с БД
	if s.sourceDB != nil {
		if err := s.sourceDB.Close(); err != nil {
			s.logger.Errorf("Ошибка при закрытии sourceDB: %v", err)
		}
	}
	if s.database != nil {
		if err := s.database.Close(); err != nil {
			s.logger.Errorf("Ошибка при закрытии соединения с БД: %v", err)
			if s.fileLogger != nil {
				s.fileLogger.Error("Ошибка при закрытии соединения с БД: %v", err)
			}
		}
	}

	// Закрытие файлового логирования
	if s.fileLogger != nil {
		s.fileLogger.Close()
	}

	s.logger.Info("ES API Service остановлен")
	return nil
}

// RunService запускает сервис как Windows-службу или в режиме отладки
func RunService(cfg *config.Config) error {
	svcConfig := &service.Config{
		Name:        "ESAPIService",
		DisplayName: "ES API Service",
		Description: "HTTP API сервис для экспорта данных из MS SQL Server с BZip2 сжатием",
	}

	esService := NewESAPIService(cfg)

	srv, err := service.New(esService, svcConfig)
	if err != nil {
		return err
	}

	logger, err := srv.Logger(nil)
	if err != nil {
		return err
	}
	esService.logger = logger

	// Проверяем аргументы командной строки для управления сервисом
	if len(os.Args) > 1 && os.Args[1] == "-service" {
		if len(os.Args) < 3 {
			logger.Info("Использование: программа -service [install|uninstall|start|stop]")
			return nil
		}

		switch os.Args[2] {
		case "install":
			return srv.Install()
		case "uninstall":
			return srv.Uninstall()
		case "start":
			return srv.Start()
		case "stop":
			return srv.Stop()
		default:
			logger.Infof("Неизвестная команда: %s", os.Args[2])
			return nil
		}
	}

	// Запуск сервиса
	return srv.Run()
}
