package sync

import (
	"context"
	"database/sql"
	"errors"
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

// errFTPNoNewFiles — на FTP пусто, это не сбой расписания.
var errFTPNoNewFiles = errors.New("на FTP нет файлов для импорта")

// PriceListScheduler управляет автоматическим обновлением прайс-листов по расписанию
type PriceListScheduler struct {
	database *db.Database
	logger   *logger.Logger
	cron     *cron.Cron
	importer *dbfimport.DBFImporter
	matcher  *matching.PriceMatcher
}

// NewPriceListScheduler создает новый планировщик прайс-листов
func NewPriceListScheduler(database *db.Database, logger *logger.Logger, importer *dbfimport.DBFImporter, matcher *matching.PriceMatcher) *PriceListScheduler {
	// Используем парсер без секунд для внутреннего планировщика (формат: мин час день месяц день_недели)
	cronParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return &PriceListScheduler{
		database: database,
		logger:   logger,
		cron:     cron.New(cron.WithParser(cronParser)),
		importer: importer,
		matcher:  matcher,
	}
}

// Start запускает планировщик
func (pls *PriceListScheduler) Start() {
	if pls.logger != nil {
		pls.logger.Info("Запуск планировщика обновления прайс-листов")
	}

	_, err := pls.cron.AddFunc("* * * * *", func() {
		pls.checkAndUpdatePriceLists()
	})
	if err != nil {
		if pls.logger != nil {
			pls.logger.Error("Ошибка добавления задачи проверки расписаний: %v", err)
		}
		return
	}

	pls.cron.Start()
	if pls.logger != nil {
		pls.logger.Info("Планировщик обновления прайс-листов запущен (проверка каждую минуту)")
	}

	// Выполняем начальную проверку
	go pls.checkAndUpdatePriceLists()
}

// Stop останавливает планировщик
func (pls *PriceListScheduler) Stop() {
	if pls.cron != nil {
		pls.cron.Stop()
		if pls.logger != nil {
			pls.logger.Info("Планировщик обновления прайс-листов остановлен")
		}
	}
}

// checkAndUpdatePriceLists проверяет все активные прайс-листы и обновляет их по расписанию
func (pls *PriceListScheduler) checkAndUpdatePriceLists() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	query := `
		SELECT 
			CAST(pl.PriceListID AS TEXT) AS PriceListID,
			CAST(pl.ImportPointID AS TEXT) AS ImportPointID,
			pl.ScheduleCron,
			pl.NextUpdateAt,
			pl.LastUpdateAt,
			ip.SourceFilePath,
			ip.DBFFilePath,
			ip.Name AS ImportPointName,
			s.Name AS SupplierName,
			ip.SourceType,
			ip.FtpHost, ip.FtpPort, ip.FtpUser, ip.FtpPassword, ip.FtpRemotePath
		FROM PriceList pl
		INNER JOIN ImportPoint ip ON pl.ImportPointID = ip.ImportPointID
		INNER JOIN Supplier s ON pl.SupplierID = s.SupplierID
		WHERE pl.IsActive = 1
		  AND pl.ScheduleCron IS NOT NULL
		  AND pl.ScheduleCron != ''
		  AND pl.ImportPointID IS NOT NULL
		  AND ip.IsActive = 1
	`

	rows, err := pls.database.GORMWith(ctx).Raw(query).Rows()
	if err != nil {
		if pls.logger != nil {
			pls.logger.Error("Ошибка получения списка прайс-листов для обновления: %v", err)
		}
		return
	}
	defer rows.Close()

	now := time.Now()
	nowUTC := now.UTC()
	updatedCount := 0

	for rows.Next() {
		var priceListID, importPointID, scheduleCron, importPointName, supplierName string
		var sourceFilePath, dbfFilePath sql.NullString
		var nextUpdateAt, lastUpdateAt sql.NullTime
		var sourceType sql.NullString
		var ftpHost, ftpUser, ftpPassword, ftpRemotePath sql.NullString
		var ftpPort sql.NullInt32

		err := rows.Scan(&priceListID, &importPointID, &scheduleCron, &nextUpdateAt, &lastUpdateAt,
			&sourceFilePath, &dbfFilePath, &importPointName, &supplierName,
			&sourceType, &ftpHost, &ftpPort, &ftpUser, &ftpPassword, &ftpRemotePath)
		if err != nil {
			if pls.logger != nil {
				pls.logger.Warn("Ошибка сканирования прайс-листа: %v", err)
			}
			continue
		}

		shouldUpdate := false

		if !nextUpdateAt.Valid {
			nextUpdate := pls.calculateNextUpdate(scheduleCron, now)
			if nextUpdate != nil {
				pls.updateNextUpdateAt(ctx, priceListID, nextUpdate.UTC())
				shouldUpdate = !nextUpdate.After(now)
			}
		} else {
			shouldUpdate = !nextUpdateAt.Time.After(nowUTC)
		}

		if shouldUpdate {
			// Сначала очищаем зависшие импорты (PROCESSING старше 30 минут), помечаем их как FAILED
			cleanupQuery := `
				UPDATE InvoiceImport 
				SET ImportStatus = 'FAILED',
				    ErrorMessage = 'Импорт завис (превышено время ожидания 30 минут)',
				    CompletedAt = (NOW() AT TIME ZONE 'utc')
				WHERE ImportPointID = CAST(@importPointID AS UUID)
				  AND ImportStatus = 'PROCESSING'
				  AND StartedAt <= DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
			`
			res := pls.database.GORMWith(ctx).Exec(cleanupQuery, sql.Named("importPointID", importPointID))
			if res.Error != nil {
				if pls.logger != nil {
					pls.logger.Warn("Ошибка очистки зависших импортов для точки %s: %v", importPointID, res.Error)
				}
			} else if res.RowsAffected > 0 && pls.logger != nil {
				pls.logger.Info("Очищено зависших импортов: %d для точки импорта %s", res.RowsAffected, importPointID)
			}

			// Теперь проверяем активные импорты (PROCESSING), но не старше 30 минут (защита от зависших импортов)
			checkActiveQuery := `
				SELECT COUNT(*) 
				FROM InvoiceImport 
				WHERE ImportPointID = CAST(@importPointID AS UUID)
				  AND ImportStatus = 'PROCESSING'
				  AND StartedAt > DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
			`
			var activeCount int
			err = pls.database.GORMWith(ctx).Raw(checkActiveQuery, sql.Named("importPointID", importPointID)).Row().Scan(&activeCount)
			if err != nil && err != sql.ErrNoRows {
				if pls.logger != nil {
					pls.logger.Error("Ошибка проверки активных импортов для точки %s: %v", importPointID, err)
				}
				continue
			}

			if activeCount > 0 {
				if pls.logger != nil {
					pls.logger.Warn("Пропуск обновления прайс-листа %s: для точки импорта %s уже выполняется импорт (PROCESSING). Запрос отклонен.", priceListID, importPointID)
				}
				nextRetry := nowUTC.Add(5 * time.Minute)
				pls.updateNextUpdateAt(ctx, priceListID, nextRetry)
				continue
			}

			if pls.logger != nil {
				pls.logger.Info("Начало автоматического обновления прайс-листа: %s (Поставщик: %s, Точка: %s, Тип: %s)", priceListID, supplierName, importPointName, sourceType.String)
			}

			var importErr error
			st := ""
			if sourceType.Valid {
				st = sourceType.String
			}

			if st == "ftp" && ftpHost.Valid && ftpHost.String != "" {
				port := 21
				if ftpPort.Valid {
					port = int(ftpPort.Int32)
				}
				user := "anonymous"
				if ftpUser.Valid && ftpUser.String != "" {
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
				importErr = pls.updatePriceListFromFTP(ctx, priceListID, importPointID, importPointName, ftpHost.String, port, user, pass, remotePath)
			} else {
				filePath := ""
				if sourceFilePath.Valid && sourceFilePath.String != "" {
					filePath = sourceFilePath.String
				} else if dbfFilePath.Valid && dbfFilePath.String != "" {
					filePath = dbfFilePath.String
				}
				importErr = pls.updatePriceListFromImportPoint(ctx, priceListID, importPointID, filePath)
			}

			if importErr != nil {
				nextUpdate := pls.calculateNextUpdate(scheduleCron, now)
				if nextUpdate != nil {
					pls.updateNextUpdateAt(ctx, priceListID, nextUpdate.UTC())
				}
				if errors.Is(importErr, errFTPNoNewFiles) {
					if pls.logger != nil {
						pls.logger.Info("Прайс-лист %s: новых файлов на FTP нет, следующее окно: %s",
							priceListID, nextUpdateOr(nextUpdate, "—"))
					}
				} else if pls.logger != nil {
					pls.logger.Error("Ошибка обновления прайс-листа %s: %v", priceListID, importErr)
					if nextUpdate != nil {
						pls.logger.Info("Следующая попытка обновления прайс-листа %s: %s (MSK)", priceListID, nextUpdate.Format("2006-01-02 15:04:05"))
					}
				}
			} else {
				updatedCount++
				nextUpdate := pls.calculateNextUpdate(scheduleCron, now)
				if nextUpdate != nil {
					pls.updateLastAndNextUpdate(ctx, priceListID, nowUTC, nextUpdate.UTC())
					if pls.logger != nil {
						pls.logger.Info("Прайс-лист %s обновлен. Следующее обновление: %s (MSK)", priceListID, nextUpdate.Format("2006-01-02 15:04:05"))
					}
				}
			}
		}
	}

	if updatedCount > 0 && pls.logger != nil {
		pls.logger.Info("Автоматически обновлено прайс-листов: %d", updatedCount)
	}
}

func nextUpdateOr(t *time.Time, fallback string) string {
	if t == nil {
		return fallback
	}
	return t.Format("2006-01-02 15:04:05")
}

// ForceFetchPriceListNow запускает немедленный забор конкретного прайс-листа,
// не дожидаясь минутной проверки планировщика.
func (pls *PriceListScheduler) ForceFetchPriceListNow(ctx context.Context, priceListID string) error {
	query := `
		SELECT 
			CAST(pl.PriceListID AS TEXT) AS PriceListID,
			CAST(pl.ImportPointID AS TEXT) AS ImportPointID,
			pl.ScheduleCron,
			ip.SourceFilePath,
			ip.DBFFilePath,
			ip.Name AS ImportPointName,
			s.Name AS SupplierName,
			ip.SourceType,
			ip.FtpHost, ip.FtpPort, ip.FtpUser, ip.FtpPassword, ip.FtpRemotePath
		FROM PriceList pl
		INNER JOIN ImportPoint ip ON pl.ImportPointID = ip.ImportPointID
		INNER JOIN Supplier s ON pl.SupplierID = s.SupplierID
		WHERE pl.PriceListID = CAST(@priceListID AS UUID)
		  AND pl.IsActive = 1
		  AND pl.ImportPointID IS NOT NULL
		  AND ip.IsActive = 1
		LIMIT 1
	`

	var importPointID, scheduleCron, importPointName, supplierName string
	var sourceFilePath, dbfFilePath sql.NullString
	var sourceType sql.NullString
	var ftpHost, ftpUser, ftpPassword, ftpRemotePath sql.NullString
	var ftpPort sql.NullInt32

	err := pls.database.GORMWith(ctx).Raw(query, sql.Named("priceListID", priceListID)).Row().Scan(
		&priceListID, &importPointID, &scheduleCron,
		&sourceFilePath, &dbfFilePath, &importPointName, &supplierName,
		&sourceType, &ftpHost, &ftpPort, &ftpUser, &ftpPassword, &ftpRemotePath,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("прайс не найден, неактивен или у него нет активной точки импорта")
		}
		return fmt.Errorf("ошибка получения прайс-листа: %w", err)
	}

	checkActiveQuery := `
		SELECT COUNT(*) 
		FROM InvoiceImport 
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		  AND ImportStatus = 'PROCESSING'
		  AND StartedAt > DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc'))
	`
	var activeCount int
	err = pls.database.GORMWith(ctx).Raw(checkActiveQuery, sql.Named("importPointID", importPointID)).Row().Scan(&activeCount)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("ошибка проверки активных импортов: %w", err)
	}
	if activeCount > 0 {
		return fmt.Errorf("для этой точки импорта уже выполняется импорт")
	}

	if pls.logger != nil {
		pls.logger.Info("Запуск ручного обновления прайс-листа: %s (Поставщик: %s, Точка: %s, Тип: %s)",
			priceListID, supplierName, importPointName, sourceType.String)
	}

	var importErr error
	st := ""
	if sourceType.Valid {
		st = sourceType.String
	}

	if st == "ftp" && ftpHost.Valid && ftpHost.String != "" {
		port := 21
		if ftpPort.Valid {
			port = int(ftpPort.Int32)
		}
		user := "anonymous"
		if ftpUser.Valid && ftpUser.String != "" {
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
		importErr = pls.updatePriceListFromFTP(ctx, priceListID, importPointID, importPointName, ftpHost.String, port, user, pass, remotePath)
	} else {
		filePath := ""
		if sourceFilePath.Valid && sourceFilePath.String != "" {
			filePath = sourceFilePath.String
		} else if dbfFilePath.Valid && dbfFilePath.String != "" {
			filePath = dbfFilePath.String
		}
		importErr = pls.updatePriceListFromImportPoint(ctx, priceListID, importPointID, filePath)
	}

	if importErr != nil {
		if pls.logger != nil {
			pls.logger.Error("Ошибка ручного обновления прайс-листа %s: %v", priceListID, importErr)
		}
		return importErr
	}

	nowUTC := time.Now().UTC()
	nextUpdate := pls.calculateNextUpdate(scheduleCron, time.Now())
	if nextUpdate != nil {
		pls.updateLastAndNextUpdate(ctx, priceListID, nowUTC, nextUpdate.UTC())
	} else {
		pls.updateNextUpdateAt(ctx, priceListID, nowUTC)
	}

	return nil
}

// updatePriceListFromFTP скачивает файл с FTP и запускает импорт для прайс-листа
func (pls *PriceListScheduler) updatePriceListFromFTP(ctx context.Context, priceListID, importPointID, pointName, host string, port int, user, pass, remotePath string) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	if pls.logger != nil {
		pls.logger.Info("[PLS/%s] Подключение к FTP: %s", pointName, addr)
	}

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return fmt.Errorf("ошибка подключения к FTP %s: %w", addr, err)
	}
	defer conn.Quit()

	if err := conn.Login(user, pass); err != nil {
		return fmt.Errorf("ошибка авторизации FTP: %w", err)
	}

	entries, err := conn.List(remotePath)
	if err != nil {
		return fmt.Errorf("ошибка листинга FTP-папки %s: %w", remotePath, err)
	}

	for _, entry := range entries {
		if entry.Type != ftp.EntryTypeFile || !isDBFOrArchive(entry.Name) {
			continue
		}

		remoteFile := remotePath
		if !strings.HasSuffix(remoteFile, "/") {
			remoteFile += "/"
		}
		remoteFile += entry.Name

		if pls.logger != nil {
			pls.logger.Info("[PLS/%s] Скачивание FTP-файла: %s", pointName, remoteFile)
		}
		tmpDir := filepath.Join(os.TempDir(), "elfapi_ftp")
		os.MkdirAll(tmpDir, 0755)
		localPath := filepath.Join(tmpDir, entry.Name)

		if err := pls.downloadFTPFile(conn, pointName, remoteFile, localPath, entry.Size); err != nil {
			if pls.logger != nil {
				pls.logger.Error("[PLS/%s] Ошибка скачивания %s: %v", pointName, remoteFile, err)
			}
			continue
		}

		importErr := pls.updatePriceListFromImportPoint(ctx, priceListID, importPointID, localPath)

		os.Remove(localPath)

		// Не удаляем файл на FTP: поставщики часто кладут один и тот же priceK.dbf,
		// который перезаписывается. Удаление ломало повторный забор каждые N минут.
		if importErr == nil && pls.logger != nil {
			pls.logger.Info("[PLS/%s] Импорт FTP-файла завершён: %s (удалён только локальный временный файл)", pointName, remoteFile)
		}

		return importErr
	}

	if pls.logger != nil {
		pls.logger.Info("[PLS/%s] На FTP %s нет файлов для импорта в %s — пропуск до следующего окна", pointName, addr, remotePath)
	}
	return errFTPNoNewFiles
}

func (pls *PriceListScheduler) downloadFTPFile(conn *ftp.ServerConn, pointName, remoteFile, localPath string, expectedSize uint64) error {
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := conn.Retr(remoteFile)
		if err != nil {
			if attempt == maxAttempts {
				return err
			}
			continue
		}

		outFile, err := os.Create(localPath)
		if err != nil {
			resp.Close()
			return fmt.Errorf("ошибка создания файла %s: %w", localPath, err)
		}

		written, copyErr := io.Copy(outFile, resp)
		closeErr := outFile.Close()
		resp.Close()
		if copyErr != nil {
			_ = os.Remove(localPath)
			if attempt == maxAttempts {
				return fmt.Errorf("ошибка записи файла %s: %w", localPath, copyErr)
			}
			continue
		}
		if closeErr != nil {
			_ = os.Remove(localPath)
			if attempt == maxAttempts {
				return fmt.Errorf("ошибка закрытия файла %s: %w", localPath, closeErr)
			}
			continue
		}

		if expectedSize > 0 && uint64(written) != expectedSize {
			_ = os.Remove(localPath)
			if pls.logger != nil {
				pls.logger.Warn("[PLS/%s] FTP-файл скачан не полностью (%d из %d байт), попытка %d/%d",
					pointName, written, expectedSize, attempt, maxAttempts)
			}
			if attempt == maxAttempts {
				return fmt.Errorf("неполная загрузка FTP-файла: %d из %d байт", written, expectedSize)
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("не удалось скачать FTP-файл %s", remoteFile)
}

// calculateNextUpdate вычисляет следующее время обновления на основе CRON выражения
func (pls *PriceListScheduler) calculateNextUpdate(cronExpr string, from time.Time) *time.Time {
	// Пробуем сначала с секундами (расширенный формат) + @every
	parserWithSeconds := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	schedule, err := parserWithSeconds.Parse(cronExpr)
	if err != nil {
		parserStandard := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		schedule, err = parserStandard.Parse(cronExpr)
		if err != nil {
			if pls.logger != nil {
				pls.logger.Warn("Некорректное CRON выражение '%s': %v", cronExpr, err)
			}
			return nil
		}
	}

	nextTime := schedule.Next(from)
	return &nextTime
}

// updatePriceListFromImportPoint обновляет прайс-лист из точки импорта
func (pls *PriceListScheduler) updatePriceListFromImportPoint(ctx context.Context, priceListID, importPointID, filePath string) error {
	// Если файл не указан, пытаемся найти последний файл для этой точки импорта
	if filePath == "" {
		query := `
			SELECT FilePath
			FROM InvoiceImport
			WHERE ImportPointID = CAST(@importPointID AS UUID)
			  AND ImportStatus = 'COMPLETED'
			ORDER BY CompletedAt DESC
LIMIT 1
`
		var lastFilePath sql.NullString
		err := pls.database.GORMWith(ctx).Raw(query, sql.Named("importPointID", importPointID)).Row().Scan(&lastFilePath)
		if err != nil && err != sql.ErrNoRows {
			if pls.logger != nil {
				pls.logger.Warn("Не удалось найти последний файл для точки импорта %s: %v", importPointID, err)
			}
		} else if lastFilePath.Valid {
			filePath = lastFilePath.String
		}
	}

	// Проверяем существование файла - если файл не найден, не запускаем импорт
	if filePath == "" {
		if pls.logger != nil {
			pls.logger.Warn("Не указан файл для автоматического обновления прайс-листа %s (точка импорта %s). Пропускаем обновление.", priceListID, importPointID)
		}
		return fmt.Errorf("файл для импорта не указан")
	}

	// Проверяем существование файла
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if pls.logger != nil {
			pls.logger.Error("Файл %s не найден для автоматического обновления прайс-листа %s (точка импорта %s). Пропускаем обновление.", filePath, priceListID, importPointID)
		}
		return fmt.Errorf("файл не найден: %s: %w", filePath, err)
	}

	// Если файл является директорией, это ошибка
	if fileInfo.IsDir() {
		if pls.logger != nil {
			pls.logger.Error("Указан путь к директории вместо файла: %s. Пропускаем обновление.", filePath)
		}
		return fmt.Errorf("указан путь к директории вместо файла: %s", filePath)
	}

	// Проверяем, является ли файл архивом
	extractedFilePath := filePath
	isArchiveFile := false

	ext := strings.ToLower(filepath.Ext(filePath))
	archiveExts := []string{".zip", ".rar", ".7z", ".tar", ".gz", ".bz2"}
	for _, archiveExt := range archiveExts {
		if ext == archiveExt {
			isArchiveFile = true
			break
		}
	}

	// Если это архив, распаковываем его
	if isArchiveFile {
		if pls.logger != nil {
			pls.logger.Info("Обнаружен архив %s. Начинаем распаковку...", filePath)
		}

		extractedPath, err := dbfimport.ExtractArchive(filePath, pls.logger)
		if err != nil {
			if pls.logger != nil {
				pls.logger.Error("Ошибка распаковки архива %s: %v. Пропускаем обновление.", filePath, err)
			}
			return fmt.Errorf("ошибка распаковки архива: %w", err)
		}

		extractedFilePath = extractedPath

		// Устанавливаем флаг для очистки временных файлов после импорта
		defer func() {
			if extractedPath != filePath {
				dbfimport.CleanupExtractedFiles(extractedPath)
				if pls.logger != nil {
					pls.logger.Info("Временные файлы распаковки удалены: %s", extractedPath)
				}
			}
		}()
	}

	// Получаем маппинг полей для точки импорта
	mappings, err := pls.getFieldMappings(ctx, importPointID)
	if err != nil {
		return fmt.Errorf("ошибка получения маппинга полей: %w", err)
	}

	// Импортируем файл (используем распакованный файл, если был архив)
	if pls.logger != nil {
		pls.logger.Info("Импорт файла %s для прайс-листа %s", extractedFilePath, priceListID)
	}

	invoiceImport, err := pls.importer.ImportInvoice(ctx, importPointID, extractedFilePath, mappings)
	if err != nil {
		return fmt.Errorf("ошибка импорта файла: %w", err)
	}

	// Автоматически запускаем сопоставление после успешного импорта
	if invoiceImport != nil && invoiceImport.ImportStatus == "COMPLETED" {
		if pls.logger != nil {
			pls.logger.Info("Автоматический запуск сопоставления для импорта %s (прайс-лист %s)", invoiceImport.InvoiceImportID, priceListID)
		}

		// Используем контекст без таймаута для сопоставления больших файлов (может занять много времени)
		// Для очень больших файлов сопоставление может занимать несколько часов
		matchCtx, matchCancel := context.WithCancel(context.Background()) // Без таймаута для больших файлов
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if pls.logger != nil {
						pls.logger.Error("[PriceListScheduler] Паника в горутине: %v", r)
					}
				}
			}()
			defer matchCancel()
			err := pls.matcher.MatchInvoiceData(matchCtx, invoiceImport.InvoiceImportID)
			if err != nil {
				if pls.logger != nil {
					pls.logger.Error("Ошибка сопоставления данных импорта %s: %v", invoiceImport.InvoiceImportID, err)
				}
			} else {
				if pls.logger != nil {
					pls.logger.Info("Сопоставление данных импорта %s успешно завершено", invoiceImport.InvoiceImportID)
				}
			}
		}()
	}

	return nil
}

// getFieldMappings получает маппинг полей для точки импорта
func (pls *PriceListScheduler) getFieldMappings(ctx context.Context, importPointID string) ([]models.DBFFieldMapping, error) {
	query := `
		SELECT 
			CAST(MappingID AS TEXT) AS MappingID,
			CAST(ImportPointID AS TEXT) AS ImportPointID,
			DBFFieldName, 
			TargetFieldName, 
			DataType, 
			IsRequired, 
			DefaultValue, 
			TransformRule,
			DisplayOrder,
			CreatedAt,
			UpdatedAt
		FROM DBFFieldMapping
		WHERE ImportPointID = CAST(@importPointID AS UUID)
		ORDER BY DisplayOrder
	`

	rows, err := pls.database.GORMWith(ctx).Raw(query, sql.Named("importPointID", importPointID)).Rows()
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

// updateNextUpdateAt обновляет NextUpdateAt для прайс-листа
func (pls *PriceListScheduler) updateNextUpdateAt(ctx context.Context, priceListID string, nextUpdate time.Time) {
	query := `
		UPDATE PriceList
		SET NextUpdateAt = @nextUpdate,
		    UpdatedAt = (NOW() AT TIME ZONE 'utc')
		WHERE PriceListID = CAST(@priceListID AS UUID)
	`

	err := pls.database.GORMWith(ctx).Exec(query, sql.Named("priceListID", priceListID), sql.Named("nextUpdate", nextUpdate)).Error
	if err != nil && pls.logger != nil {
		pls.logger.Warn("Ошибка обновления NextUpdateAt для прайс-листа %s: %v", priceListID, err)
	}
}

// updateLastAndNextUpdate обновляет LastUpdateAt и NextUpdateAt
func (pls *PriceListScheduler) updateLastAndNextUpdate(ctx context.Context, priceListID string, lastUpdate, nextUpdate time.Time) {
	query := `
		UPDATE PriceList
		SET LastUpdateAt = @lastUpdate,
		    NextUpdateAt = @nextUpdate,
		    UpdatedAt = (NOW() AT TIME ZONE 'utc')
		WHERE PriceListID = CAST(@priceListID AS UUID)
	`

	err := pls.database.GORMWith(ctx).Exec(query,
		sql.Named("priceListID", priceListID),
		sql.Named("lastUpdate", lastUpdate),
		sql.Named("nextUpdate", nextUpdate)).Error
	if err != nil && pls.logger != nil {
		pls.logger.Warn("Ошибка обновления времени обновления для прайс-листа %s: %v", priceListID, err)
	}
}
