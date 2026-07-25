package sync

import (
	"context"
	"database/sql"
	"es_api_service/internal/db"
	"es_api_service/internal/dbfimport"
	"es_api_service/internal/logger"
	"es_api_service/internal/matching"
	"es_api_service/internal/models"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/robfig/cron/v3"
)

// ImportPointScheduler следит за активными точками импорта и забирает файлы
type ImportPointScheduler struct {
	database *db.Database
	logger   *logger.Logger
	cron     *cron.Cron
	importer *dbfimport.DBFImporter
	matcher  *matching.PriceMatcher
}

// NewImportPointScheduler создает планировщик забора файлов
func NewImportPointScheduler(database *db.Database, logger *logger.Logger, importer *dbfimport.DBFImporter, matcher *matching.PriceMatcher) *ImportPointScheduler {
	cronParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return &ImportPointScheduler{
		database: database,
		logger:   logger,
		cron:     cron.New(cron.WithParser(cronParser)),
		importer: importer,
		matcher:  matcher,
	}
}

// Start запускает планировщик (проверка каждые 5 минут)
func (ips *ImportPointScheduler) Start() {
	if ips.logger != nil {
		ips.logger.Info("Запуск планировщика забора файлов из точек импорта")
	}

	_, err := ips.cron.AddFunc("*/5 * * * *", func() {
		ips.scanImportPoints()
	})
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("Ошибка добавления задачи сканирования точек импорта: %v", err)
		}
		return
	}

	ips.cron.Start()
	if ips.logger != nil {
		ips.logger.Info("Планировщик забора файлов запущен (каждые 5 минут)")
	}
}

// Stop останавливает планировщик
func (ips *ImportPointScheduler) Stop() {
	if ips.cron != nil {
		ips.cron.Stop()
		if ips.logger != nil {
			ips.logger.Info("Планировщик забора файлов остановлен")
		}
	}
}

// scanImportPoints проверяет все активные точки импорта
func (ips *ImportPointScheduler) scanImportPoints() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	query := `
		SELECT 
			CAST(ImportPointID AS TEXT) AS ImportPointID,
			Name,
			SourceType,
			SourceFilePath,
			FtpHost, FtpPort, FtpUser, FtpPassword, FtpRemotePath
		FROM ImportPoint
		WHERE IsActive = 1
		  AND (
			(SourceType = 'local' AND SourceFilePath IS NOT NULL AND SourceFilePath != '')
			OR
			(SourceType = 'ftp' AND FtpHost IS NOT NULL AND FtpHost != '')
		  )
	`

	rows, err := ips.database.GORMWith(ctx).Raw(query).Rows()
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("Ошибка получения точек импорта для сканирования: %v", err)
		}
		return
	}
	defer rows.Close()

	for rows.Next() {
		var pointID, name, sourceType string
		var sourceFilePath, ftpHost, ftpUser, ftpPassword, ftpRemotePath sql.NullString
		var ftpPort sql.NullInt32

		err := rows.Scan(&pointID, &name, &sourceType,
			&sourceFilePath, &ftpHost, &ftpPort, &ftpUser, &ftpPassword, &ftpRemotePath)
		if err != nil {
			if ips.logger != nil {
				ips.logger.Warn("Ошибка сканирования точки импорта: %v", err)
			}
			continue
		}

		if ips.hasActiveImport(ctx, pointID) {
			continue
		}

		if ips.hasCronPriceList(ctx, pointID) {
			continue
		}

		switch sourceType {
		case "local":
			if sourceFilePath.Valid && sourceFilePath.String != "" {
				ips.processLocalSource(ctx, pointID, name, sourceFilePath.String)
			}
		case "ftp":
			if ftpHost.Valid && ftpHost.String != "" {
				port := 21
				if ftpPort.Valid {
					port = int(ftpPort.Int32)
				}
				user := ""
				if ftpUser.Valid {
					user = ftpUser.String
				}
				pass := ""
				if ftpPassword.Valid {
					pass = ftpPassword.String
				}
				remotePath := "/"
				if ftpRemotePath.Valid && ftpRemotePath.String != "" {
					remotePath = ftpRemotePath.String
				}
				ips.processFtpSource(ctx, pointID, name, ftpHost.String, port, user, pass, remotePath)
			}
		}
	}
}

// hasActiveImport проверяет, есть ли активный импорт для точки
func (ips *ImportPointScheduler) hasActiveImport(ctx context.Context, pointID string) bool {
	query := `
		SELECT COUNT(*) FROM InvoiceImport
		WHERE ImportPointID = CAST(@pointID AS UUID)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt > DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
	`
	var count int
	err := ips.database.GORMWith(ctx).Raw(query, sql.Named("pointID", pointID)).Row().Scan(&count)
	if err != nil {
		return true
	}
	return count > 0
}

// hasCronPriceList проверяет, привязана ли точка импорта к активному прайс-листу с cron-расписанием
func (ips *ImportPointScheduler) hasCronPriceList(ctx context.Context, pointID string) bool {
	query := `
		SELECT COUNT(*) FROM PriceList
		WHERE ImportPointID = CAST(@pointID AS UUID)
		  AND IsActive = 1
		  AND ScheduleCron IS NOT NULL
		  AND ScheduleCron != ''
	`
	var count int
	err := ips.database.GORMWith(ctx).Raw(query, sql.Named("pointID", pointID)).Row().Scan(&count)
	if err != nil {
		return false
	}
	if count > 0 && ips.logger != nil {
		ips.logger.Info("[ImportPointScheduler] Точка %s привязана к прайс-листу с cron — пропуск (управляется PriceListScheduler)", pointID)
	}
	return count > 0
}

// processLocalSource обрабатывает локальный источник — ищет файлы в папке
func (ips *ImportPointScheduler) processLocalSource(ctx context.Context, pointID, name, dirPath string) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return
	}

	if !info.IsDir() {
		// Путь указывает на конкретный файл
		if isDBFOrArchive(dirPath) {
			ips.importAndCleanup(ctx, pointID, name, dirPath)
		}
		return
	}

	// Сканируем папку — берём первый подходящий файл
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if ips.logger != nil {
			ips.logger.Warn("[%s] Ошибка чтения папки %s: %v", name, dirPath, err)
		}
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dirPath, entry.Name())
		if isDBFOrArchive(fullPath) {
			ips.importAndCleanup(ctx, pointID, name, fullPath)
			return
		}
	}
}

// processFtpSource скачивает файл с FTP и запускает импорт
func (ips *ImportPointScheduler) processFtpSource(ctx context.Context, pointID, name, host string, port int, user, pass, remotePath string) {
	addr := fmt.Sprintf("%s:%d", host, port)

	if ips.logger != nil {
		ips.logger.Info("[%s] Подключение к FTP: %s", name, addr)
	}

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("[%s] Ошибка подключения к FTP %s: %v", name, addr, err)
		}
		return
	}
	defer conn.Quit()

	if user == "" {
		user = "anonymous"
	}
	if err := conn.Login(user, pass); err != nil {
		if ips.logger != nil {
			ips.logger.Error("[%s] Ошибка авторизации FTP: %v", name, err)
		}
		return
	}

	entries, err := conn.List(remotePath)
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("[%s] Ошибка листинга FTP-папки %s: %v", name, remotePath, err)
		}
		return
	}

	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFile && isDBFOrArchive(entry.Name) {
			remoteFile := remotePath
			if !strings.HasSuffix(remoteFile, "/") {
				remoteFile += "/"
			}
			remoteFile += entry.Name

			if ips.logger != nil {
				ips.logger.Info("[%s] Скачивание FTP-файла: %s", name, remoteFile)
			}

			resp, err := conn.Retr(remoteFile)
			if err != nil {
				if ips.logger != nil {
					ips.logger.Error("[%s] Ошибка скачивания %s: %v", name, remoteFile, err)
				}
				continue
			}

			// Сохраняем во временный файл
			tmpDir := filepath.Join(os.TempDir(), "elfapi_ftp")
			if err := os.MkdirAll(tmpDir, 0755); err != nil {
				if ips.logger != nil {
					ips.logger.Error("[%s] Ошибка создания временной папки %s: %v", name, tmpDir, err)
				}
				return
			}
			localPath := filepath.Join(tmpDir, entry.Name)

			outFile, err := os.Create(localPath)
			if err != nil {
				resp.Close()
				if ips.logger != nil {
					ips.logger.Error("[%s] Ошибка создания файла %s: %v", name, localPath, err)
				}
				continue
			}

			_, err = io.Copy(outFile, resp)
			outFile.Close()
			resp.Close()

			if err != nil {
				os.Remove(localPath)
				if ips.logger != nil {
					ips.logger.Error("[%s] Ошибка записи файла %s: %v", name, localPath, err)
				}
				continue
			}

			// Импортируем
			success := ips.importAndCleanup(ctx, pointID, name, localPath)
			if !success {
				_ = os.Remove(localPath)
			}

			// Удаляем файл на FTP после успешного импорта
			if success {
				if err := conn.Delete(remoteFile); err != nil {
					if ips.logger != nil {
						ips.logger.Warn("[%s] Не удалось удалить FTP-файл %s: %v", name, remoteFile, err)
					}
				} else if ips.logger != nil {
					ips.logger.Info("[%s] FTP-файл удалён: %s", name, remoteFile)
				}
			}

			return
		}
	}
}

// importAndCleanup запускает импорт файла и удаляет его после успешной обработки. Возвращает true при успехе.
func (ips *ImportPointScheduler) importAndCleanup(ctx context.Context, pointID, name, filePath string) bool {
	if ips.logger != nil {
		ips.logger.Info("[%s] Обнаружен файл: %s — запуск импорта", name, filePath)
	}

	// Распаковка архива если нужно
	actualPath := filePath
	isArchive := false
	ext := strings.ToLower(filepath.Ext(filePath))
	archiveExts := []string{".zip", ".rar", ".7z", ".tar", ".gz", ".bz2"}
	for _, ae := range archiveExts {
		if ext == ae {
			isArchive = true
			break
		}
	}

	if isArchive {
		extracted, err := dbfimport.ExtractArchive(filePath, ips.logger)
		if err != nil {
			if ips.logger != nil {
				ips.logger.Error("[%s] Ошибка распаковки %s: %v", name, filePath, err)
			}
			return false
		}
		actualPath = extracted
		defer dbfimport.CleanupExtractedFiles(extracted)
	}

	// Получаем маппинг полей
	mappings, err := ips.getFieldMappings(ctx, pointID)
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("[%s] Ошибка получения маппинга: %v", name, err)
		}
		return false
	}

	if len(mappings) == 0 {
		if ips.logger != nil {
			ips.logger.Warn("[%s] Маппинг полей не настроен — пропуск", name)
		}
		return false
	}

	// Импорт
	invoiceImport, err := ips.importer.ImportInvoice(ctx, pointID, actualPath, mappings)
	if err != nil {
		if ips.logger != nil {
			ips.logger.Error("[%s] Ошибка импорта %s: %v", name, actualPath, err)
		}
		return false
	}

	success := invoiceImport != nil && invoiceImport.ImportStatus == "COMPLETED"

	// Сопоставление
	if success {
		if ips.logger != nil {
			ips.logger.Info("[%s] Импорт завершён, запуск сопоставления: %s", name, invoiceImport.InvoiceImportID)
		}

		matchCtx, matchCancel := context.WithCancel(context.Background())
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if ips.logger != nil {
						ips.logger.Error("[ImportPointScheduler] Паника в горутине: %v", r)
					}
				}
			}()
			defer matchCancel()
			if err := ips.matcher.MatchInvoiceData(matchCtx, invoiceImport.InvoiceImportID); err != nil {
				if ips.logger != nil {
					ips.logger.Error("[%s] Ошибка сопоставления: %v", name, err)
				}
			} else if ips.logger != nil {
				ips.logger.Info("[%s] Сопоставление завершено: %s", name, invoiceImport.InvoiceImportID)
			}
		}()
	}

	// Удаляем исходный файл после успешного импорта
	if success {
		if err := os.Remove(filePath); err != nil {
			if ips.logger != nil {
				ips.logger.Warn("[%s] Не удалось удалить файл %s: %v", name, filePath, err)
			}
		} else if ips.logger != nil {
			ips.logger.Info("[%s] Файл удалён после импорта: %s", name, filePath)
		}
	}

	return success
}

// getFieldMappings получает маппинг полей для точки импорта
func (ips *ImportPointScheduler) getFieldMappings(ctx context.Context, importPointID string) ([]models.DBFFieldMapping, error) {
	query := `
		SELECT CAST(MappingID AS TEXT), CAST(ImportPointID AS TEXT),
		       DBFFieldName, TargetFieldName, DataType, IsRequired,
		       DefaultValue, TransformRule, DisplayOrder, CreatedAt, UpdatedAt
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		ORDER BY DisplayOrder
	`
	rows, err := ips.database.GORMWith(ctx).Raw(query, sql.Named("importPointID", importPointID)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mappings []models.DBFFieldMapping
	for rows.Next() {
		var m models.DBFFieldMapping
		var defaultValue, transformRule sql.NullString
		err := rows.Scan(&m.MappingID, &m.ImportPointID, &m.DBFFieldName, &m.TargetFieldName,
			&m.DataType, &m.IsRequired, &defaultValue, &transformRule, &m.DisplayOrder,
			&m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			continue
		}
		if defaultValue.Valid {
			m.DefaultValue = &defaultValue.String
		}
		if transformRule.Valid {
			m.TransformRule = &transformRule.String
		}
		mappings = append(mappings, m)
	}
	return mappings, nil
}

func isDBFOrArchive(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".dbf", ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2":
		return true
	}
	return false
}
