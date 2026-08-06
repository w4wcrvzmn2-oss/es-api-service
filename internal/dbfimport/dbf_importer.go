package dbfimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"es_api_service/internal/models"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LindsayBradford/go-dbf/godbf"
	"github.com/google/uuid"
)

// validateDBFStructure проверяет структуру файла данных и соответствие полей маппингу.
func (di *DBFImporter) validateDBFStructure(dbfFieldNames []string, mappings []models.DBFFieldMapping) []string {
	var errors []string

	// Создаем карту полей DBF для быстрого поиска (case-insensitive)
	dbfFieldsMap := make(map[string]bool)
	for _, fieldName := range dbfFieldNames {
		dbfFieldsMap[strings.ToUpper(fieldName)] = true
		dbfFieldsMap[strings.ToLower(fieldName)] = true
		dbfFieldsMap[fieldName] = true
	}

	if len(dbfFieldNames) == 0 {
		errors = append(errors, "DBF файл не содержит полей")
		return errors
	}

	// Проверяем обязательные поля в маппинге
	var missingRequiredFields []string
	var missingOptionalFields []string

	for _, mapping := range mappings {
		fieldFound := false
		// Проверяем различные варианты регистра
		for _, dbfField := range dbfFieldNames {
			if strings.EqualFold(dbfField, mapping.DBFFieldName) {
				fieldFound = true
				break
			}
		}

		if !fieldFound {
			if mapping.IsRequired {
				missingRequiredFields = append(missingRequiredFields,
					fmt.Sprintf("Обязательное поле '%s' (маппинг на '%s') не найдено в DBF файле",
						mapping.DBFFieldName, mapping.TargetFieldName))
			} else {
				missingOptionalFields = append(missingOptionalFields,
					fmt.Sprintf("Поле '%s' (маппинг на '%s') не найдено в DBF файле",
						mapping.DBFFieldName, mapping.TargetFieldName))
			}
		}
	}

	if len(missingRequiredFields) > 0 {
		errors = append(errors, "ОБЯЗАТЕЛЬНЫЕ ПОЛЯ ОТСУТСТВУЮТ:")
		errors = append(errors, missingRequiredFields...)
	}

	if len(missingOptionalFields) > 0 && di.logger != nil {
		di.logger.Warn("Необязательные поля отсутствуют в DBF файле: %v", missingOptionalFields)
	}

	// Проверяем наличие полей в DBF, которые не имеют маппинга (предупреждение)
	var unmappedFields []string
	for _, dbfField := range dbfFieldNames {
		found := false
		for _, mapping := range mappings {
			if strings.EqualFold(dbfField, mapping.DBFFieldName) {
				found = true
				break
			}
		}
		if !found {
			unmappedFields = append(unmappedFields, dbfField)
		}
	}

	if len(unmappedFields) > 0 && di.logger != nil {
		di.logger.Warn("Поля в DBF файле без маппинга (будут пропущены): %v", unmappedFields)
	}

	return errors
}

// DBFImporter обрабатывает импорт файлов прайсов (DBF/Excel).
type DBFImporter struct {
	database *db.Database
	logger   *logger.Logger
}

// NewDBFImporter создает новый импортер файлов прайсов.
func NewDBFImporter(database *db.Database, logger *logger.Logger) *DBFImporter {
	return &DBFImporter{
		database: database,
		logger:   logger,
	}
}

// ImportInvoice импортирует накладную из файла прайса.
func (di *DBFImporter) ImportInvoice(ctx context.Context, importPointID, filePath string, mappings []models.DBFFieldMapping) (*models.InvoiceImport, error) {
	if di.logger != nil {
		di.logger.Info("Начало импорта файла прайса: ImportPointID=%s, FilePath=%s", importPointID, filePath)
	}

	// Проверяем существование файла
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if di.logger != nil {
			di.logger.Error("Файл не найден: %s, ошибка: %v", filePath, err)
		}
		return nil, fmt.Errorf("файл не найден: %w", err)
	}

	if di.logger != nil {
		di.logger.Info("Файл найден: размер=%d байт, путь=%s", fileInfo.Size(), filePath)
	}

	// Создаем запись об импорте
	invoiceImport := &models.InvoiceImport{
		InvoiceImportID:  generateGUID(),
		ImportPointID:    importPointID,
		FileName:         filepath.Base(filePath),
		FilePath:         filePath,
		FileSize:         func() *int64 { s := fileInfo.Size(); return &s }(),
		ImportStatus:     "PROCESSING",
		StartedAt:        func() *time.Time { t := time.Now(); return &t }(),
		RecordsTotal:     0,
		RecordsProcessed: 0,
		RecordsSkipped:   0,
		RecordsError:     0,
		CreatedAt:        time.Now(),
	}

	// Сохраняем запись об импорте
	if err := di.saveInvoiceImport(ctx, invoiceImport); err != nil {
		if di.logger != nil {
			di.logger.Error("Ошибка сохранения записи об импорте: InvoiceImportID=%s, ошибка: %v", invoiceImport.InvoiceImportID, err)
		}
		return nil, fmt.Errorf("не удалось сохранить запись об импорте: %w", err)
	}

	if di.logger != nil {
		di.logger.Info("Запись об импорте создана: InvoiceImportID=%s", invoiceImport.InvoiceImportID)
	}

	// DBF (Katren ~75MB): не грузим весь файл в []map — иначе OOM и рестарт службы каждые ~40с.
	if DetectDataFileFormat(filePath) == "dbf" {
		return di.importInvoiceDBFStream(ctx, invoiceImport, importPointID, filePath, mappings)
	}

	// Excel и прочее — прежний путь (файлы обычно меньше).
	if di.logger != nil {
		di.logger.Info("Чтение файла прайса: %s", filePath)
	}
	fieldNames, records, err := ReadTabularFile(filePath)
	if err != nil {
		errMsg := fmt.Sprintf("не удалось открыть файл данных: %v", err)
		if di.logger != nil {
			di.logger.Error("Ошибка чтения файла данных: %s, ошибка: %v", filePath, err)
		}
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf("не удалось открыть файл данных: %w", err)
	}

	invoiceImport.RecordsTotal = len(records)
	if di.logger != nil {
		di.logger.Info("Файл прайса прочитан успешно: формат=%s, записей=%d", DetectDataFileFormat(filePath), invoiceImport.RecordsTotal)
	}

	// Получаем маппинг полей
	fieldMap := make(map[string]models.DBFFieldMapping)
	for _, mapping := range mappings {
		fieldMap[mapping.DBFFieldName] = mapping
		if di.logger != nil {
			di.logger.Info("Маппинг поля: DBF=%s -> Target=%s, Type=%s, Required=%v",
				mapping.DBFFieldName, mapping.TargetFieldName, mapping.DataType, mapping.IsRequired)
		}
	}

	if len(mappings) == 0 {
		errMsg := "маппинг полей не настроен для точки импорта"
		if di.logger != nil {
			di.logger.Error("Ошибка: %s, ImportPointID=%s", errMsg, importPointID)
		}
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf(errMsg)
	}

	// Получаем SupplierID из ImportPoint
	if di.logger != nil {
		di.logger.Info("Получение SupplierID для точки импорта: ImportPointID=%s", importPointID)
	}
	supplierID, err := di.getSupplierIDFromImportPoint(ctx, importPointID)
	if err != nil {
		errMsg := fmt.Sprintf("не удалось получить SupplierID: %v", err)
		if di.logger != nil {
			di.logger.Error("Ошибка получения SupplierID: ImportPointID=%s, ошибка: %v", importPointID, err)
		}
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, err
	}

	if di.logger != nil {
		di.logger.Info("SupplierID получен: %s", supplierID)
	}

	if di.logger != nil {
		di.logger.Info("Поля в файле данных (%d): %v", len(fieldNames), fieldNames)
	}

	// Проверяем структуру и соответствие полей перед импортом.
	validationErrors := di.validateDBFStructure(fieldNames, mappings)
	if len(validationErrors) > 0 {
		errMsg := fmt.Sprintf("Ошибки валидации структуры файла данных:\n%s", strings.Join(validationErrors, "\n"))
		if di.logger != nil {
			di.logger.Error("Ошибки валидации файла данных %s:\n%s", filePath, errMsg)
		}
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf("ошибки валидации структуры файла данных: %s", strings.Join(validationErrors, "; "))
	}

	if di.logger != nil {
		di.logger.Info("Валидация структуры файла данных пройдена успешно. Начинаем импорт записей...")
	}

	totalRecords := len(records)
	if totalRecords == 0 {
		errMsg := "Не удалось прочитать ни одной записи из файла данных"
		if di.logger != nil {
			di.logger.Error("Ошибка: %s", errMsg)
		}
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf(errMsg)
	}

	if di.logger != nil {
		di.logger.Info("Начинаем параллельную обработку %d записей...", totalRecords)
	}

	// Обновляем счетчик в invoiceImport на фактическое количество
	invoiceImport.RecordsTotal = totalRecords

	// Количество воркеров ограничиваем консервативно:
	// массовый импорт и последующий матчинг не должны выбивать локальный PostgreSQL по соединениям.
	numWorkers := 8
	if totalRecords < 100 {
		numWorkers = 2
	} else if totalRecords < 1000 {
		numWorkers = 4
	}
	if totalRecords < numWorkers {
		numWorkers = totalRecords
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	if di.logger != nil {
		di.logger.Info("Запуск %d воркеров для параллельной обработки", numWorkers)
	}

	// Канал для передачи задач воркерам
	recordChan := make(chan int, numWorkers*2)

	// Счетчики с атомарными операциями для потокобезопасности
	var processedCount int64
	var errorCount int64

	// WaitGroup для ожидания завершения всех воркеров
	var wg sync.WaitGroup

	// Запускаем воркеры
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Обработка паник для предотвращения остановки всего процесса
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&errorCount, 1)
					if di.logger != nil {
						di.logger.Error("ПАНИКА в воркере %d: %v", workerID, r)
						di.logger.Error("Stack trace: %s", fmt.Sprintf("%+v", r))
					}
				}
			}()

			processedByWorker := 0
			// Batch insert для ускорения - накапливаем записи и вставляем батчами
			batchSize := 100
			batch := make([]*models.InvoiceData, 0, batchSize)

			for recordIndex := range recordChan {
				// Проверяем контекст на отмену
				select {
				case <-ctx.Done():
					// Сохраняем оставшиеся записи в батче перед выходом
					if len(batch) > 0 {
						di.saveInvoiceDataBatch(ctx, batch)
						atomic.AddInt64(&processedCount, int64(len(batch)))
					}
					if di.logger != nil {
						di.logger.Warn("Воркер %d остановлен: контекст отменен", workerID)
					}
					return
				default:
				}

				record := records[recordIndex]

				// Преобразуем запись в структуру InvoiceData
				invoiceData, err := di.mapDBFRecordToInvoiceData(ctx, record, fieldMap, supplierID, invoiceImport.InvoiceImportID)
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					if di.logger != nil {
						// Логируем только первые 10 ошибок для каждого воркера
						if atomic.LoadInt64(&errorCount) <= 10 || processedByWorker < 10 {
							di.logger.Error("Ошибка обработки записи %d/%d (воркер %d): %v", recordIndex+1, totalRecords, workerID, err)
						}
					}
					continue
				}

				// Добавляем в батч
				batch = append(batch, invoiceData)
				processedByWorker++

				// Когда батч заполнен, сохраняем его
				if len(batch) >= batchSize {
					if err := di.saveInvoiceDataBatch(ctx, batch); err != nil {
						atomic.AddInt64(&errorCount, int64(len(batch)))
						if di.logger != nil {
							di.logger.Error("Ошибка batch сохранения %d записей (воркер %d): %v", len(batch), workerID, err)
						}
					} else {
						atomic.AddInt64(&processedCount, int64(len(batch)))
					}
					batch = batch[:0] // Очищаем батч, сохраняя capacity
				}

				// Логирование прогресса убрано - слишком много сообщений
				// Прогресс отслеживается через периодический ticker
			}

			// Сохраняем оставшиеся записи в батче
			if len(batch) > 0 {
				if err := di.saveInvoiceDataBatch(ctx, batch); err != nil {
					atomic.AddInt64(&errorCount, int64(len(batch)))
					if di.logger != nil {
						di.logger.Error("Ошибка batch сохранения последних %d записей (воркер %d): %v", len(batch), workerID, err)
					}
				} else {
					atomic.AddInt64(&processedCount, int64(len(batch)))
				}
			}

			if di.logger != nil && processedByWorker > 0 {
				di.logger.Debug("Воркер %d завершил работу: обработано %d записей", workerID, processedByWorker)
			}
		}(w)
	}

	// Отправляем задачи в канал
	go func() {
		defer close(recordChan)
		sentCount := 0
		for i := 0; i < totalRecords; i++ {
			select {
			case <-ctx.Done():
				if di.logger != nil {
					di.logger.Warn("Отправка задач прервана: контекст отменен. Отправлено %d из %d", sentCount, totalRecords)
				}
				return
			case recordChan <- i:
				sentCount++
				if sentCount%1000 == 0 && di.logger != nil {
					di.logger.Debug("Отправлено задач в канал: %d/%d", sentCount, totalRecords)
				}
			}
		}
		if di.logger != nil {
			di.logger.Info("Все задачи отправлены в канал: %d записей", sentCount)
		}
	}()

	// Ждем завершения всех воркеров
	if di.logger != nil {
		di.logger.Info("Ожидание завершения всех воркеров... (всего записей: %d)", totalRecords)
	}

	// Периодически логируем прогресс во время ожидания (реже, чтобы не засорять логи)
	progressTicker := time.NewTicker(60 * time.Second)
	defer progressTicker.Stop()

	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	for {
		select {
		case <-done:
			if di.logger != nil {
				di.logger.Info("Все воркеры завершили работу")
			}
			goto finished
		case <-progressTicker.C:
			currentProcessed := atomic.LoadInt64(&processedCount)
			currentErrors := atomic.LoadInt64(&errorCount)
			if di.logger != nil {
				totalProcessed := currentProcessed + currentErrors
				remaining := int64(totalRecords) - totalProcessed
				di.logger.Info("Прогресс импорта: обработано %d/%d записей (успешно: %d, ошибок: %d, осталось: %d)",
					totalProcessed, totalRecords, currentProcessed, currentErrors, remaining)
			}
		case <-ctx.Done():
			if di.logger != nil {
				di.logger.Error("ИМПОРТ ПРЕРВАН: контекст отменен. Обработано: %d/%d",
					atomic.LoadInt64(&processedCount)+atomic.LoadInt64(&errorCount), totalRecords)
			}
			goto finished
		}
	}

finished:
	if di.logger != nil {
		di.logger.Info("Параллельная обработка завершена. Успешно: %d, ошибок: %d, всего должно быть: %d",
			atomic.LoadInt64(&processedCount), atomic.LoadInt64(&errorCount), totalRecords)

		// Проверяем, обработаны ли все записи
		totalProcessed := atomic.LoadInt64(&processedCount) + atomic.LoadInt64(&errorCount)
		if totalProcessed < int64(totalRecords) {
			di.logger.Warn("ВНИМАНИЕ: Обработано только %d из %d записей. Возможно, процесс был прерван или произошла ошибка.",
				totalProcessed, totalRecords)
		}
	}

	// Обновляем счетчики
	invoiceImport.RecordsProcessed = int(atomic.LoadInt64(&processedCount))
	invoiceImport.RecordsError = int(atomic.LoadInt64(&errorCount))

	// Обновляем статус импорта
	completedAt := time.Now()
	invoiceImport.CompletedAt = &completedAt

	// Определяем финальный статус
	if invoiceImport.RecordsError > 0 && invoiceImport.RecordsProcessed == 0 {
		// Если все записи с ошибками
		invoiceImport.ImportStatus = "FAILED"
		errMsg := fmt.Sprintf("Все записи (%d) обработаны с ошибками", invoiceImport.RecordsTotal)
		invoiceImport.ErrorMessage = &errMsg
	} else if invoiceImport.RecordsError > 0 {
		// Если есть ошибки, но часть записей обработана
		invoiceImport.ImportStatus = "COMPLETED"
		if di.logger != nil {
			di.logger.Warn("Импорт завершен с ошибками: обработано %d/%d записей, ошибок: %d",
				invoiceImport.RecordsProcessed, invoiceImport.RecordsTotal, invoiceImport.RecordsError)
		}
	} else {
		// Все успешно
		invoiceImport.ImportStatus = "COMPLETED"
	}

	if err := di.updateInvoiceImport(ctx, invoiceImport); err != nil {
		if di.logger != nil {
			di.logger.Error("Ошибка обновления статуса импорта: InvoiceImportID=%s, ошибка: %v",
				invoiceImport.InvoiceImportID, err)
		}
	}

	if di.logger != nil {
		di.logger.Info("Импорт завершен: InvoiceImportID=%s, Статус=%s, Всего=%d, Обработано=%d, Ошибок=%d, Пропущено=%d",
			invoiceImport.InvoiceImportID, invoiceImport.ImportStatus,
			invoiceImport.RecordsTotal, invoiceImport.RecordsProcessed,
			invoiceImport.RecordsError, invoiceImport.RecordsSkipped)
	}

	return invoiceImport, nil
}

// importInvoiceDBFStream читает DBF построчно и пишет батчами — без загрузки всего файла в RAM.
func (di *DBFImporter) importInvoiceDBFStream(ctx context.Context, invoiceImport *models.InvoiceImport, importPointID, filePath string, mappings []models.DBFFieldMapping) (result *models.InvoiceImport, err error) {
	defer func() {
		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("panic при потоковом импорте DBF: %v", r)
			if di.logger != nil {
				di.logger.Error("%s file=%s processed_before_panic", errMsg, filePath)
			}
			invoiceImport.ImportStatus = "FAILED"
			invoiceImport.ErrorMessage = &errMsg
			now := time.Now()
			invoiceImport.CompletedAt = &now
			_ = di.updateInvoiceImport(ctx, invoiceImport)
			result = nil
			err = fmt.Errorf("%s", errMsg)
		}
	}()

	if di.logger != nil {
		di.logger.Info("Потоковое чтение DBF: %s", filePath)
	}

	dbfTable, errOpen := godbf.NewFromFile(filePath, "CP866")
	if errOpen != nil {
		errMsg := fmt.Sprintf("не удалось открыть DBF файл: %v", errOpen)
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf("%s", errMsg)
	}

	fieldNames := dbfTable.FieldNames()
	headerCount := dbfTable.NumberOfRecords()
	if headerCount < 0 {
		headerCount = 0
	}
	invoiceImport.RecordsTotal = headerCount

	if len(mappings) == 0 {
		errMsg := "маппинг полей не настроен для точки импорта"
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf("%s", errMsg)
	}

	fieldMap := make(map[string]models.DBFFieldMapping, len(mappings))
	for _, mapping := range mappings {
		fieldMap[mapping.DBFFieldName] = mapping
	}

	if errs := di.validateDBFStructure(fieldNames, mappings); len(errs) > 0 {
		errMsg := fmt.Sprintf("Ошибки валидации структуры файла данных:\n%s", strings.Join(errs, "\n"))
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, fmt.Errorf("%s", errMsg)
	}

	supplierID, errSup := di.getSupplierIDFromImportPoint(ctx, importPointID)
	if errSup != nil {
		errMsg := fmt.Sprintf("не удалось получить SupplierID: %v", errSup)
		invoiceImport.ImportStatus = "FAILED"
		invoiceImport.ErrorMessage = &errMsg
		di.updateInvoiceImport(ctx, invoiceImport)
		return nil, errSup
	}

	if di.logger != nil {
		di.logger.Info("DBF потоковый импорт: полей=%d, записей(header)=%d, SupplierID=%s", len(fieldNames), headerCount, supplierID)
	}

	const batchSize = 200
	batch := make([]*models.InvoiceData, 0, batchSize)
	processed := 0
	errorsCount := 0

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if saveErr := di.saveInvoiceDataBatch(ctx, batch); saveErr != nil {
			errorsCount += len(batch)
			if di.logger != nil {
				di.logger.Error("Ошибка batch сохранения %d записей: %v", len(batch), saveErr)
			}
		} else {
			processed += len(batch)
		}
		batch = batch[:0]
	}

	// Строго в пределах NumberOfRecords — выход за header у godbf часто даёт panic → 7031 и рестарт службы.
	for i := 0; i < headerCount; i++ {
		select {
		case <-ctx.Done():
			flush()
			errMsg := "импорт прерван: контекст отменен"
			invoiceImport.ImportStatus = "FAILED"
			invoiceImport.ErrorMessage = &errMsg
			invoiceImport.RecordsProcessed = processed
			invoiceImport.RecordsError = errorsCount
			now := time.Now()
			invoiceImport.CompletedAt = &now
			di.updateInvoiceImport(ctx, invoiceImport)
			return nil, ctx.Err()
		default:
		}

		record, rowOK := readDBFRecordSafe(dbfTable, fieldNames, i)
		if !rowOK {
			errorsCount++
			continue
		}

		invoiceData, mapErr := di.mapDBFRecordToInvoiceData(ctx, record, fieldMap, supplierID, invoiceImport.InvoiceImportID)
		if mapErr != nil {
			errorsCount++
			continue
		}
		batch = append(batch, invoiceData)
		if len(batch) >= batchSize {
			flush()
			if processed > 0 && processed%2000 == 0 && di.logger != nil {
				di.logger.Info("Прогресс потокового импорта DBF: %d/%d успешно, ошибок=%d", processed, headerCount, errorsCount)
			}
		}
	}

	flush()

	now := time.Now()
	invoiceImport.CompletedAt = &now
	invoiceImport.RecordsTotal = headerCount
	invoiceImport.RecordsProcessed = processed
	invoiceImport.RecordsError = errorsCount

	if processed == 0 {
		invoiceImport.ImportStatus = "FAILED"
		errMsg := fmt.Sprintf("Не удалось импортировать ни одной записи (ошибок: %d)", errorsCount)
		invoiceImport.ErrorMessage = &errMsg
	} else {
		invoiceImport.ImportStatus = "COMPLETED"
	}

	if updErr := di.updateInvoiceImport(ctx, invoiceImport); updErr != nil && di.logger != nil {
		di.logger.Error("Ошибка обновления статуса импорта: %v", updErr)
	}
	if di.logger != nil {
		di.logger.Info("Потоковый импорт DBF завершен: InvoiceImportID=%s Status=%s Processed=%d Errors=%d",
			invoiceImport.InvoiceImportID, invoiceImport.ImportStatus, processed, errorsCount)
	}
	if processed == 0 {
		return nil, fmt.Errorf("не удалось импортировать ни одной записи")
	}
	return invoiceImport, nil
}

// readDBFRecordSafe читает одну строку DBF; panic в godbf не роняет процесс.
func readDBFRecordSafe(dbfTable *godbf.DbfTable, fieldNames []string, row int) (record map[string]interface{}, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			record = nil
		}
	}()
	record = make(map[string]interface{}, len(fieldNames))
	for _, fieldName := range fieldNames {
		fieldValue, ferr := dbfTable.FieldValueByName(row, fieldName)
		if ferr != nil {
			record[fieldName] = ""
			continue
		}
		record[fieldName] = fieldValue
	}
	return record, true
}

// min возвращает минимальное из двух чисел
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// mapDBFRecordToInvoiceData преобразует запись DBF в InvoiceData
func (di *DBFImporter) mapDBFRecordToInvoiceData(ctx context.Context, record map[string]interface{}, fieldMap map[string]models.DBFFieldMapping, supplierID, invoiceImportID string) (*models.InvoiceData, error) {
	invoiceData := &models.InvoiceData{
		InvoiceDataID:   generateGUID(),
		InvoiceImportID: invoiceImportID,
		SupplierID:      supplierID,
		IsProcessed:     false,
		CreatedAt:       time.Now(),
	}

	// Сохраняем исходные данные в JSON
	rawDataJSON, _ := json.Marshal(record)
	rawDataStr := string(rawDataJSON)
	invoiceData.RawData = &rawDataStr

	// Применяем маппинг
	for dbfFieldName, value := range record {
		mapping, exists := fieldMap[dbfFieldName]
		if !exists {
			continue
		}

		// Преобразуем значение согласно типу данных
		convertedValue, err := di.convertValue(value, mapping.DataType)
		if err != nil {
			if di.logger != nil {
				di.logger.Warn("Ошибка преобразования значения поля '%s' (DBF='%s', Target='%s', Type='%s'): %v, значение: %v",
					dbfFieldName, mapping.DBFFieldName, mapping.TargetFieldName, mapping.DataType, err, value)
			}
			// Если поле обязательное, возвращаем ошибку
			if mapping.IsRequired {
				return nil, fmt.Errorf("не удалось преобразовать обязательное поле '%s' (тип %s): %w",
					mapping.DBFFieldName, mapping.DataType, err)
			}
			continue
		}

		// Присваиваем значение целевому полю
		switch strings.ToLower(mapping.TargetFieldName) {
		case "invoice_number":
			if str, ok := convertedValue.(string); ok {
				invoiceData.InvoiceNumber = &str
			}
		case "invoice_date":
			if t, ok := convertedValue.(*time.Time); ok {
				invoiceData.InvoiceDate = t
			}
		case "item_code":
			// ItemCode может быть строкой или числом (INT)
			var itemCodeStr string
			if str, ok := convertedValue.(string); ok {
				itemCodeStr = strings.TrimSpace(str)
			} else if num, ok := convertedValue.(int); ok {
				itemCodeStr = fmt.Sprintf("%d", num)
			} else if num, ok := convertedValue.(int64); ok {
				itemCodeStr = fmt.Sprintf("%d", num)
			} else if num, ok := convertedValue.(float64); ok {
				// Если число без дробной части, форматируем как целое
				if num == float64(int64(num)) {
					itemCodeStr = fmt.Sprintf("%.0f", num)
				} else {
					itemCodeStr = fmt.Sprintf("%v", num)
				}
			} else {
				// Пробуем преобразовать в строку любым способом
				itemCodeStr = strings.TrimSpace(fmt.Sprintf("%v", convertedValue))
			}

			if itemCodeStr != "" {
				invoiceData.ItemCode = &itemCodeStr
			} else {
				if di.logger != nil {
					di.logger.Warn("ItemCode из DBF пустой после преобразования: исходный тип=%T, значение=%v", convertedValue, convertedValue)
				}
			}
		case "item_name":
			if str, ok := convertedValue.(string); ok {
				invoiceData.ItemName = &str
			}
		case "quantity":
			var qty float64
			if f, ok := convertedValue.(float64); ok {
				qty = f
			} else if i, ok := convertedValue.(int); ok {
				qty = float64(i)
			} else if i, ok := convertedValue.(int64); ok {
				qty = float64(i)
			} else if str, ok := convertedValue.(string); ok {
				// Пробуем распарсить строку как число
				str = strings.TrimSpace(str)
				if str == "" {
					continue
				}
				var parsed float64
				if _, err := fmt.Sscanf(str, "%f", &parsed); err == nil {
					qty = parsed
				} else {
					if di.logger != nil {
						di.logger.Debug("Quantity не удалось преобразовать из строки '%s': %v", str, err)
					}
					continue
				}
			} else {
				if di.logger != nil {
					di.logger.Debug("Quantity не удалось преобразовать: тип=%T, значение=%v", convertedValue, convertedValue)
				}
				continue
			}
			// Сохраняем даже если 0 (это валидное значение)
			invoiceData.Quantity = &qty
			if di.logger != nil {
				di.logger.Debug("Quantity импортирован: %.3f (тип исходного значения: %T)", qty, convertedValue)
			}
		case "price":
			if f, ok := convertedValue.(float64); ok {
				invoiceData.Price = &f
			}
		case "batch_number":
			if str, ok := convertedValue.(string); ok {
				invoiceData.BatchNumber = &str
			}
		case "expiry_date":
			if t, ok := convertedValue.(*time.Time); ok {
				invoiceData.ExpiryDate = t
			}
		case "manufacturer":
			if str, ok := convertedValue.(string); ok {
				invoiceData.Manufacturer = &str
			}
		case "country":
			if str, ok := convertedValue.(string); ok {
				invoiceData.Country = &str
			}
		case "barcode":
			if str, ok := convertedValue.(string); ok {
				// Нормализуем штрихкод: убираем пробелы
				normalized := strings.TrimSpace(str)
				if normalized != "" {
					invoiceData.Barcode = &normalized
				}
			} else if num, ok := convertedValue.(float64); ok {
				// Если штрихкод пришел как число (часто бывает в DBF для EAN13)
				// Преобразуем в строку без десятичной части и научной нотации
				// Важно для EAN13 - всегда целое число, может быть очень большим (13 цифр)
				barcodeStr := ""
				if num == float64(int64(num)) {
					// Для целых чисел используем fmt.Sprintf("%d", ...) чтобы избежать научной нотации
					barcodeStr = fmt.Sprintf("%d", int64(num))
				} else {
					// Если есть дробная часть, используем %.0f
					barcodeStr = fmt.Sprintf("%.0f", num)
				}
				// Убираем возможные пробелы и точки
				barcodeStr = strings.TrimSpace(barcodeStr)
				invoiceData.Barcode = &barcodeStr
			} else if num, ok := convertedValue.(int); ok {
				// Если штрихкод пришел как целое число
				barcodeStr := fmt.Sprintf("%d", num)
				invoiceData.Barcode = &barcodeStr
			} else if num, ok := convertedValue.(int64); ok {
				// Если штрихкод пришел как int64
				barcodeStr := fmt.Sprintf("%d", num)
				invoiceData.Barcode = &barcodeStr
			}
		}
	}

	return invoiceData, nil
}

// convertValue преобразует значение согласно типу данных
func (di *DBFImporter) convertValue(value interface{}, dataType string) (interface{}, error) {
	switch strings.ToUpper(dataType) {
	case "NVARCHAR", "VARCHAR", "STRING":
		if str, ok := value.(string); ok {
			return strings.TrimSpace(str), nil
		}
		// Для чисел преобразуем в строку, сохраняя формат
		if num, ok := value.(float64); ok {
			// Для целых чисел (например, штрихкодов) форматируем без десятичной части
			if num == float64(int64(num)) {
				return fmt.Sprintf("%.0f", num), nil
			}
			return fmt.Sprintf("%v", value), nil
		}
		return fmt.Sprintf("%v", value), nil
	case "DECIMAL", "FLOAT", "NUMERIC":
		switch v := value.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case string:
			var f float64
			if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
				return f, nil
			}
		}
		return 0.0, nil
	case "INT", "INTEGER":
		switch v := value.(type) {
		case int:
			return v, nil
		case float64:
			return int(v), nil
		case string:
			var i int
			if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
				return i, nil
			}
		}
		return 0, nil
	case "DATE", "DATETIME":
		switch v := value.(type) {
		case time.Time:
			return &v, nil
		case string:
			// Убираем пробелы и пробуем разные форматы
			v = strings.TrimSpace(v)
			if v == "" {
				return nil, nil
			}
			// Расширенный список форматов дат для DBF
			layouts := []string{
				"2006-01-02",          // ISO
				"02.01.2006",          // DD.MM.YYYY
				"02/01/2006",          // DD/MM/YYYY
				"2006-01-02T15:04:05", // ISO с временем
				time.RFC3339,          // RFC3339
				"02.01.06",            // DD.MM.YY
				"02/01/06",            // DD/MM/YY
				"20060102",            // YYYYMMDD
				"020106",              // DDMMYY
			}
			for _, layout := range layouts {
				if t, err := time.Parse(layout, v); err == nil {
					return &t, nil
				}
			}
			// Если не удалось распарсить и значение не пустое, логируем только для нестандартных случаев
			if di.logger != nil && v != "" && v != ".." && v != "--" && v != "//" {
				// Логируем только если значение выглядит как дата, но не распарсилось
				if len(v) >= 4 && len(v) <= 20 {
					di.logger.Debug("Не удалось распарсить дату из DBF: '%s' (тип: %T)", v, value)
				}
			}
		case int, int64:
			// DBF может хранить дату как число (количество дней с 1900-01-01)
			var days int64
			if i, ok := v.(int); ok {
				days = int64(i)
			} else {
				days = v.(int64)
			}
			// Базовая дата для DBF: 1900-01-01
			baseDate := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
			t := baseDate.AddDate(0, 0, int(days))
			return &t, nil
		case float64:
			// DBF может хранить дату как число с плавающей точкой
			days := int64(v)
			baseDate := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
			t := baseDate.AddDate(0, 0, int(days))
			return &t, nil
		}
		return nil, nil
	default:
		return value, nil
	}
}

// Helper функции для работы с БД
func (di *DBFImporter) getSupplierIDFromImportPoint(ctx context.Context, importPointID string) (string, error) {
	// Сначала пробуем из ImportPoint напрямую
	query := `SELECT CAST(SupplierID AS TEXT) AS SupplierID FROM ImportPoint WHERE ImportPointID = CAST(@importPointID AS UUID) AND SupplierID IS NOT NULL`
	var supplierID string
	err := di.database.QueryRowContext(ctx, query, sql.Named("importPointID", importPointID)).Scan(&supplierID)
	if err == nil {
		return supplierID, nil
	}

	// Если SupplierID не задан в ImportPoint, берём из связанного PriceList
	queryPL := `SELECT CAST(pl.SupplierID AS TEXT) FROM PriceList pl WHERE pl.ImportPointID = CAST(@importPointID AS UUID) AND pl.IsActive = 1
LIMIT 1
`
	err = di.database.QueryRowContext(ctx, queryPL, sql.Named("importPointID", importPointID)).Scan(&supplierID)
	if err != nil {
		return "", fmt.Errorf("SupplierID не найден ни в ImportPoint, ни в связанных PriceList для ImportPointID=%s", importPointID)
	}
	return supplierID, nil
}

func (di *DBFImporter) saveInvoiceImport(ctx context.Context, invoiceImport *models.InvoiceImport) error {
	query := `
		INSERT INTO InvoiceImport 
		(InvoiceImportID, ImportPointID, FileName, FilePath, FileSize, RecordsTotal, 
		 RecordsProcessed, RecordsSkipped, RecordsError, ImportStatus, ErrorMessage, 
		 StartedAt, CompletedAt, CreatedAt, CreatedBy)
		VALUES 
		(CAST(@invoiceImportID AS UUID), CAST(@importPointID AS UUID), 
		 @fileName, @filePath, @fileSize, @recordsTotal, @recordsProcessed, @recordsSkipped, 
		 @recordsError, @importStatus, @errorMessage, @startedAt, @completedAt, @createdAt, @createdBy)
	`

	_, err := di.database.ExecContext(ctx, query,
		sql.Named("invoiceImportID", invoiceImport.InvoiceImportID),
		sql.Named("importPointID", invoiceImport.ImportPointID),
		sql.Named("fileName", invoiceImport.FileName),
		sql.Named("filePath", invoiceImport.FilePath),
		sql.Named("fileSize", invoiceImport.FileSize),
		sql.Named("recordsTotal", invoiceImport.RecordsTotal),
		sql.Named("recordsProcessed", invoiceImport.RecordsProcessed),
		sql.Named("recordsSkipped", invoiceImport.RecordsSkipped),
		sql.Named("recordsError", invoiceImport.RecordsError),
		sql.Named("importStatus", invoiceImport.ImportStatus),
		sql.Named("errorMessage", invoiceImport.ErrorMessage),
		sql.Named("startedAt", invoiceImport.StartedAt),
		sql.Named("completedAt", invoiceImport.CompletedAt),
		sql.Named("createdAt", invoiceImport.CreatedAt),
		sql.Named("createdBy", invoiceImport.CreatedBy))

	return err
}

func (di *DBFImporter) updateInvoiceImport(ctx context.Context, invoiceImport *models.InvoiceImport) error {
	query := `
		UPDATE InvoiceImport 
		SET RecordsProcessed = @recordsProcessed, RecordsSkipped = @recordsSkipped, RecordsError = @recordsError,
		    ImportStatus = @importStatus, ErrorMessage = @errorMessage, StartedAt = @startedAt, CompletedAt = @completedAt
		WHERE InvoiceImportID = CAST(@invoiceImportID AS UUID)
	`

	_, err := di.database.ExecContext(ctx, query,
		sql.Named("recordsProcessed", invoiceImport.RecordsProcessed),
		sql.Named("recordsSkipped", invoiceImport.RecordsSkipped),
		sql.Named("recordsError", invoiceImport.RecordsError),
		sql.Named("importStatus", invoiceImport.ImportStatus),
		sql.Named("errorMessage", invoiceImport.ErrorMessage),
		sql.Named("startedAt", invoiceImport.StartedAt),
		sql.Named("completedAt", invoiceImport.CompletedAt),
		sql.Named("invoiceImportID", invoiceImport.InvoiceImportID))

	return err
}

func (di *DBFImporter) saveInvoiceData(ctx context.Context, invoiceData *models.InvoiceData) error {
	query := `
		INSERT INTO InvoiceData 
		(InvoiceDataID, InvoiceImportID, SupplierID, InvoiceNumber, InvoiceDate,
		 ItemCode, ItemName, Quantity, Price, BatchNumber, ExpiryDate,
		 Manufacturer, Country,
		 Barcode, RawData, IsProcessed, CreatedAt)
		VALUES 
		(CAST(@invoiceDataID AS UUID), CAST(@invoiceImportID AS UUID), 
		 CAST(@supplierID AS UUID), @invoiceNumber, @invoiceDate,
		 @itemCode, @itemName, @quantity, @price, @batchNumber, @expiryDate,
		 @manufacturer, @country,
		 @barcode, @rawData, @isProcessed, @createdAt)
	`

	_, err := di.database.ExecContext(ctx, query,
		sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
		sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
		sql.Named("supplierID", invoiceData.SupplierID),
		sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
		sql.Named("invoiceDate", invoiceData.InvoiceDate),
		sql.Named("itemCode", invoiceData.ItemCode),
		sql.Named("itemName", invoiceData.ItemName),
		sql.Named("quantity", invoiceData.Quantity),
		sql.Named("price", invoiceData.Price),
		sql.Named("batchNumber", invoiceData.BatchNumber),
		sql.Named("expiryDate", invoiceData.ExpiryDate),
		sql.Named("manufacturer", invoiceData.Manufacturer),
		sql.Named("country", invoiceData.Country),
		sql.Named("barcode", invoiceData.Barcode),
		sql.Named("rawData", invoiceData.RawData),
		sql.Named("isProcessed", invoiceData.IsProcessed),
		sql.Named("createdAt", invoiceData.CreatedAt))

	return err
}

// saveInvoiceDataBatch выполняет batch insert для ускорения импорта
func (di *DBFImporter) saveInvoiceDataBatch(ctx context.Context, batch []*models.InvoiceData) error {
	if len(batch) == 0 {
		return nil
	}

	// Начинаем транзакцию для batch insert
	tx, err := di.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ошибка начала транзакции: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO InvoiceData 
		(InvoiceDataID, InvoiceImportID, SupplierID, InvoiceNumber, InvoiceDate,
		 ItemCode, ItemName, Quantity, Price, BatchNumber, ExpiryDate,
		 Manufacturer, Country,
		 Barcode, RawData, IsProcessed, CreatedAt)
		VALUES 
		(CAST(? AS UUID), CAST(? AS UUID), 
		 CAST(? AS UUID), ?, ?,
		 ?, ?, ?, ?, ?, ?,
		 ?, ?,
		 ?, ?, ?, ?)
	`

	for _, invoiceData := range batch {
		_, err := db.ExecRaw(ctx, tx, query,
			invoiceData.InvoiceDataID,
			invoiceData.InvoiceImportID,
			invoiceData.SupplierID,
			invoiceData.InvoiceNumber,
			invoiceData.InvoiceDate,
			invoiceData.ItemCode,
			invoiceData.ItemName,
			invoiceData.Quantity,
			invoiceData.Price,
			invoiceData.BatchNumber,
			invoiceData.ExpiryDate,
			invoiceData.Manufacturer,
			invoiceData.Country,
			invoiceData.Barcode,
			invoiceData.RawData,
			invoiceData.IsProcessed,
			invoiceData.CreatedAt)
		if err != nil {
			return fmt.Errorf("ошибка вставки записи в батч: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	return nil
}

func generateGUID() string {
	return uuid.New().String()
}
