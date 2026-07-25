package matching

import (
	"context"
	"database/sql"
	"es_api_service/internal/db"
	"es_api_service/internal/logger"
	"es_api_service/internal/models"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// PriceMatcher обрабатывает сопоставление импортированных данных с справочником
type PriceMatcher struct {
	database *db.Database
	logger   *logger.Logger
}

// NewPriceMatcher создает новый сопоставитель прайсов
func NewPriceMatcher(database *db.Database, logger *logger.Logger) *PriceMatcher {
	return &PriceMatcher{
		database: database,
		logger:   logger,
	}
}

// MatchInvoiceData сопоставляет данные из InvoiceData с справочником es_ef2
func (pm *PriceMatcher) MatchInvoiceData(ctx context.Context, invoiceImportID string) error {
	if pm.logger != nil {
		pm.logger.Info("Начало сопоставления данных импорта %s", invoiceImportID)
	}

	// Проверяем валидность invoiceImportID
	_, err := uuid.Parse(invoiceImportID)
	if err != nil {
		return fmt.Errorf("неправильный формат invoiceImportID: %w", err)
	}

	// Кэшируем PriceListID один раз в начале обработки, чтобы не делать запрос для каждой записи
	var cachedPriceListID sql.NullString
	queryPriceList := `
		SELECT CAST(pl.PriceListID AS TEXT)
		FROM InvoiceImport ii
		INNER JOIN ImportPoint ip ON ii.ImportPointID = ip.ImportPointID
		LEFT JOIN PriceList pl ON ip.ImportPointID = pl.ImportPointID AND pl.IsActive = 1
		WHERE ii.InvoiceImportID = CAST(@invoiceImportID AS UUID)
		  AND pl.PriceListID IS NOT NULL
LIMIT 1
`
	// Используем короткий таймаут для кэширования, чтобы не блокировать весь процесс
	cacheCtx, cacheCancel := context.WithTimeout(ctx, 45*time.Second)
	err = pm.database.QueryRowContext(cacheCtx, queryPriceList, sql.Named("invoiceImportID", invoiceImportID)).Scan(&cachedPriceListID)
	cacheCancel()
	if err != nil && err != sql.ErrNoRows {
		if pm.logger != nil {
			pm.logger.Warn("Ошибка получения PriceListID для кэширования: %v, продолжаем без PriceListID", err)
		}
	} else if cachedPriceListID.Valid && pm.logger != nil {
		pm.logger.Info("PriceListID кэширован для импорта %s: %s", invoiceImportID, cachedPriceListID.String)
	}

	// Получаем все неподтвержденные записи из InvoiceData для этого импорта
	query := `
		SELECT CAST(InvoiceDataID AS TEXT) AS InvoiceDataID,
		       CAST(SupplierID AS TEXT) AS SupplierID,
		       CAST(InvoiceImportID AS TEXT) AS InvoiceImportID,
		       ItemCode, ItemName, Barcode, Price, Quantity,
		       InvoiceNumber, InvoiceDate, BatchNumber, ExpiryDate,
		       Manufacturer, Country
		FROM InvoiceData
		WHERE InvoiceImportID = CAST(@invoiceImportID AS UUID)
		  AND IsProcessed = 0
	`

	rows, err := pm.database.QueryContext(ctx, query, sql.Named("invoiceImportID", invoiceImportID))
	if err != nil {
		return fmt.Errorf("ошибка получения данных для сопоставления: %w", err)
	}
	defer rows.Close()

	// Загружаем все записи в память для параллельной обработки
	var invoiceDataList []models.InvoiceData
	for rows.Next() {
		var invoiceData models.InvoiceData
		var itemCode, itemName, barcode, invoiceNumber, batchNumber sql.NullString
		var manufacturer, country sql.NullString
		var invoiceDate, expiryDate sql.NullTime
		var quantity sql.NullFloat64
		var price sql.NullFloat64
		var supplierID, invoiceDataID, invoiceImportIDFromDB sql.NullString

		err := rows.Scan(
			&invoiceDataID,
			&supplierID,
			&invoiceImportIDFromDB,
			&itemCode,
			&itemName,
			&barcode,
			&price,
			&quantity,
			&invoiceNumber,
			&invoiceDate,
			&batchNumber,
			&expiryDate,
			&manufacturer,
			&country,
		)
		if err != nil {
			if pm.logger != nil {
				pm.logger.Warn("Ошибка сканирования InvoiceData: %v", err)
			}
			continue
		}

		// Обрабатываем обязательные поля
		if !invoiceDataID.Valid || invoiceDataID.String == "" {
			if pm.logger != nil {
				pm.logger.Warn("InvoiceDataID пустой, пропускаем запись")
			}
			continue
		}
		invoiceData.InvoiceDataID = invoiceDataID.String

		if !supplierID.Valid || supplierID.String == "" {
			if pm.logger != nil {
				pm.logger.Warn("SupplierID пустой, пропускаем запись")
			}
			continue
		}
		invoiceData.SupplierID = supplierID.String

		if invoiceImportIDFromDB.Valid && invoiceImportIDFromDB.String != "" {
			invoiceData.InvoiceImportID = invoiceImportIDFromDB.String
		} else {
			invoiceData.InvoiceImportID = invoiceImportID
		}

		// Проверяем цену (обязательное поле)
		if !price.Valid {
			if pm.logger != nil {
				pm.logger.Debug("Запись без цены, пропускаем")
			}
			continue
		}
		invoiceData.Price = &price.Float64

		// Обрабатываем опциональные поля
		if itemCode.Valid {
			invoiceData.ItemCode = &itemCode.String
		}
		if itemName.Valid {
			invoiceData.ItemName = &itemName.String
		}
		if barcode.Valid {
			invoiceData.Barcode = &barcode.String
		}
		if quantity.Valid {
			invoiceData.Quantity = &quantity.Float64
		}
		if invoiceNumber.Valid {
			invoiceData.InvoiceNumber = &invoiceNumber.String
		}
		if invoiceDate.Valid {
			invoiceData.InvoiceDate = &invoiceDate.Time
		}
		if batchNumber.Valid {
			invoiceData.BatchNumber = &batchNumber.String
		}
		if expiryDate.Valid {
			invoiceData.ExpiryDate = &expiryDate.Time
		}
		if manufacturer.Valid {
			invoiceData.Manufacturer = &manufacturer.String
		}
		if country.Valid {
			invoiceData.Country = &country.String
		}

		invoiceDataList = append(invoiceDataList, invoiceData)
	}

	totalRecords := len(invoiceDataList)
	if pm.logger != nil {
		pm.logger.Info("Загружено %d записей для сопоставления. Запускаем параллельную обработку...", totalRecords)
	}

	if totalRecords == 0 {
		if pm.logger != nil {
			pm.logger.Info("Нет записей для сопоставления")
		}
		return nil
	}

	// Определяем SupplierID для предзагрузки кэша
	var supplierIDForCache string
	if len(invoiceDataList) > 0 {
		supplierIDForCache = invoiceDataList[0].SupplierID
	}

	// Загружаем весь кэш SupplierItemMapping для этого поставщика в память
	// Это значительно ускорит обработку повторяющихся ItemCode
	mappingCache := make(map[string]*MatchResult) // key: itemCode
	cacheMutex := &sync.RWMutex{}                 // Для thread-safe доступа к кэшу

	if supplierIDForCache != "" {
		if pm.logger != nil {
			pm.logger.Info("Предзагрузка кэша SupplierItemMapping для SupplierID=%s...", supplierIDForCache)
		}
		cacheQuery := `
			SELECT 
				ItemCode,
				CAST(GUID_ES AS TEXT) AS GUID_ES,
				MatchMethod,
				MatchConfidence
			FROM SupplierItemMapping
			WHERE SupplierID = CAST(@supplierID AS UUID)
			ORDER BY UseCount DESC, LastUsedAt DESC
		`
		cacheRows, err := pm.database.QueryContext(ctx, cacheQuery, sql.Named("supplierID", supplierIDForCache))
		if err != nil {
			if pm.logger != nil {
				pm.logger.Warn("Ошибка предзагрузки кэша SupplierItemMapping: %v, продолжаем без предзагрузки", err)
			}
		} else {
			cacheCount := 0
			for cacheRows.Next() {
				var itemCode, guidES, matchMethod sql.NullString
				var matchConfidence sql.NullFloat64

				err := cacheRows.Scan(&itemCode, &guidES, &matchMethod, &matchConfidence)
				if err != nil {
					continue
				}

				if itemCode.Valid && guidES.Valid && guidES.String != "" {
					// Валидация будет выполнена после загрузки in-memory индекса
					// Пока сохраняем в кэш, валидация произойдет при использовании через drugIndexes
					method := "CACHED"
					if matchMethod.Valid {
						method = matchMethod.String
					}
					confidence := 100.0
					if matchConfidence.Valid {
						confidence = matchConfidence.Float64
					}
					cacheMutex.Lock()
					mappingCache[itemCode.String] = &MatchResult{
						GUID_ES:         guidES.String,
						MatchMethod:     method,
						MatchConfidence: confidence,
					}
					cacheMutex.Unlock()
					cacheCount++
				}
			}
			cacheRows.Close()
			if pm.logger != nil {
				pm.logger.Info("✅ Предзагружено %d записей в кэш SupplierItemMapping для SupplierID=%s", cacheCount, supplierIDForCache)
			}
		}
	}

	// Загружаем весь справочник es_ef2 в память для быстрого сопоставления
	// Это In-Memory таблица, которая освободится после завершения сопоставления
	if pm.logger != nil {
		pm.logger.Info("Загрузка справочника es_ef2 в память для быстрого сопоставления...")
	}
	drugIndexes := pm.loadDrugIndexes(ctx)
	if pm.logger != nil {
		barcodeCount := len(drugIndexes.ByBarcode)
		codeCount := len(drugIndexes.ByCode)
		nameCount := len(drugIndexes.ByName)
		pm.logger.Info("✅ Справочник загружен в память: штрихкодов=%d, кодов=%d, наименований=%d", barcodeCount, codeCount, nameCount)
	}

	// Валидируем кэш SupplierItemMapping используя in-memory индекс ByGUID
	// Удаляем записи с невалидными GUID_ES
	cacheMutex.Lock()
	totalCacheCount := len(mappingCache)
	if pm.logger != nil && totalCacheCount > 0 {
		pm.logger.Info("Валидация кэша SupplierItemMapping используя in-memory индекс...")
	}
	validCacheCount := 0
	drugIndexes.mutex.RLock()
	for itemCode, cachedMatch := range mappingCache {
		// Быстрая проверка через индекс ByGUID (O(1))
		if drugIndexes.ByGUID[cachedMatch.GUID_ES] {
			validCacheCount++
		} else {
			// Удаляем невалидную запись из кэша
			delete(mappingCache, itemCode)
		}
	}
	drugIndexes.mutex.RUnlock()
	cacheMutex.Unlock()
	if pm.logger != nil && totalCacheCount > 0 {
		pm.logger.Info("✅ Валидировано кэша: %d валидных записей из %d", validCacheCount, totalCacheCount)
	}

	// Параллельная обработка через воркеры
	// Увеличено для ускорения сопоставления
	maxWorkers := 128 // Количество параллельных горутин для больших файлов
	if totalRecords < 100 {
		maxWorkers = 8 // Для маленьких файлов меньше потоков
	} else if totalRecords < 1000 {
		maxWorkers = 32
	} else if totalRecords < 10000 {
		maxWorkers = 64
	} else if totalRecords < 50000 {
		maxWorkers = 128
	} else {
		maxWorkers = 256 // Для очень больших файлов максимум потоков
	}

	// Канал для передачи задач воркерам
	taskChan := make(chan models.InvoiceData, maxWorkers*2)

	// Счетчики с мьютексом для thread-safe доступа
	var matchedCount int64
	var unmatchedCount int64
	var mu sync.Mutex

	// Запускаем воркеры
	var wg sync.WaitGroup
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			processed := 0
			// Batch для ускорения - накапливаем записи и сохраняем батчами
			batchSize := 50
			matchedBatch := make([]struct {
				invoiceData models.InvoiceData
				match       *MatchResult
			}, 0, batchSize)
			unmatchedBatch := make([]models.InvoiceData, 0, batchSize)
			processedIDs := make([]string, 0, batchSize*2)

			for invoiceData := range taskChan {
				// Проверяем контекст на отмену
				select {
				case <-ctx.Done():
					// Сохраняем оставшиеся записи перед выходом
					if len(matchedBatch) > 0 {
						pm.saveSupplierPriceBatch(ctx, matchedBatch)
						for _, item := range matchedBatch {
							processedIDs = append(processedIDs, item.invoiceData.InvoiceDataID)
						}
						mu.Lock()
						matchedCount += int64(len(matchedBatch))
						mu.Unlock()
					}
					if len(unmatchedBatch) > 0 {
						pm.saveSupplierPriceWithoutMatchBatch(ctx, unmatchedBatch, cachedPriceListID)
						for _, item := range unmatchedBatch {
							processedIDs = append(processedIDs, item.InvoiceDataID)
						}
						mu.Lock()
						unmatchedCount += int64(len(unmatchedBatch))
						mu.Unlock()
					}
					if len(processedIDs) > 0 {
						pm.markAsProcessedBatch(ctx, processedIDs)
					}
					if pm.logger != nil {
						pm.logger.Warn("Воркер %d: контекст отменен, прерываем обработку", workerID)
					}
					return
				default:
				}

				// Пытаемся сопоставить с справочником (используем предзагруженный кэш и in-memory индексы)
				matchResult := pm.findMatchWithCacheAndIndexes(ctx, &invoiceData, mappingCache, cacheMutex, drugIndexes)
				if matchResult != nil {
					matchedBatch = append(matchedBatch, struct {
						invoiceData models.InvoiceData
						match       *MatchResult
					}{invoiceData, matchResult})
				} else {
					unmatchedBatch = append(unmatchedBatch, invoiceData)
				}

				processed++

				// Когда батч заполнен, сохраняем его
				if len(matchedBatch)+len(unmatchedBatch) >= batchSize {
					if len(matchedBatch) > 0 {
						if err := pm.saveSupplierPriceBatch(ctx, matchedBatch); err != nil {
							mu.Lock()
							unmatchedCount += int64(len(matchedBatch))
							mu.Unlock()
							if pm.logger != nil && processed < 10 {
								pm.logger.Error("Воркер %d: Ошибка batch сохранения сопоставленных: %v", workerID, err)
							}
						} else {
							mu.Lock()
							matchedCount += int64(len(matchedBatch))
							mu.Unlock()
							for _, item := range matchedBatch {
								processedIDs = append(processedIDs, item.invoiceData.InvoiceDataID)
							}
						}
						matchedBatch = matchedBatch[:0]
					}
					if len(unmatchedBatch) > 0 {
						if err := pm.saveSupplierPriceWithoutMatchBatch(ctx, unmatchedBatch, cachedPriceListID); err != nil {
							mu.Lock()
							unmatchedCount += int64(len(unmatchedBatch))
							mu.Unlock()
							if pm.logger != nil && processed < 10 {
								pm.logger.Error("Воркер %d: Ошибка batch сохранения несопоставленных: %v", workerID, err)
							}
						} else {
							mu.Lock()
							unmatchedCount += int64(len(unmatchedBatch))
							mu.Unlock()
							for _, item := range unmatchedBatch {
								processedIDs = append(processedIDs, item.InvoiceDataID)
							}
						}
						unmatchedBatch = unmatchedBatch[:0]
					}
					// Помечаем обработанные батчем
					if len(processedIDs) > 0 {
						pm.markAsProcessedBatch(ctx, processedIDs)
						processedIDs = processedIDs[:0]
					}
				}

				// Логируем прогресс каждые 1000 записей (реже для скорости)
				if processed%1000 == 0 && pm.logger != nil {
					pm.logger.Debug("Воркер %d: обработано %d записей", workerID, processed)
				}
			}

			// Сохраняем оставшиеся записи
			if len(matchedBatch) > 0 {
				if err := pm.saveSupplierPriceBatch(ctx, matchedBatch); err == nil {
					mu.Lock()
					matchedCount += int64(len(matchedBatch))
					mu.Unlock()
					for _, item := range matchedBatch {
						processedIDs = append(processedIDs, item.invoiceData.InvoiceDataID)
					}
				}
			}
			if len(unmatchedBatch) > 0 {
				if err := pm.saveSupplierPriceWithoutMatchBatch(ctx, unmatchedBatch, cachedPriceListID); err == nil {
					mu.Lock()
					unmatchedCount += int64(len(unmatchedBatch))
					mu.Unlock()
					for _, item := range unmatchedBatch {
						processedIDs = append(processedIDs, item.InvoiceDataID)
					}
				}
			}
			if len(processedIDs) > 0 {
				pm.markAsProcessedBatch(ctx, processedIDs)
			}

			if pm.logger != nil && processed > 0 {
				pm.logger.Debug("Воркер %d: завершен, обработано %d записей", workerID, processed)
			}
		}(w)
	}

	// Отправляем задачи в канал
	go func() {
		defer close(taskChan)
		for i := range invoiceDataList {
			select {
			case <-ctx.Done():
				if pm.logger != nil {
					pm.logger.Warn("Отправка задач прервана: контекст отменен")
				}
				return
			case taskChan <- invoiceDataList[i]:
			}
		}
	}()

	// Ждем завершения всех воркеров
	wg.Wait()

	if pm.logger != nil {
		pm.logger.Info("Сопоставление завершено. Всего: %d, Сопоставлено: %d, Не сопоставлено: %d",
			totalRecords, matchedCount, unmatchedCount)
	}

	// Освобождаем память: очищаем in-memory структуры
	if pm.logger != nil {
		pm.logger.Info("Освобождение памяти: очистка in-memory кэшей и индексов...")
	}

	// Очищаем mappingCache
	cacheMutex.Lock()
	for k := range mappingCache {
		delete(mappingCache, k)
	}
	mappingCache = nil
	cacheMutex.Unlock()

	// Очищаем drugIndexes
	if drugIndexes != nil {
		drugIndexes.mutex.Lock()
		for k := range drugIndexes.ByBarcode {
			delete(drugIndexes.ByBarcode, k)
		}
		for k := range drugIndexes.ByCode {
			delete(drugIndexes.ByCode, k)
		}
		for k := range drugIndexes.ByName {
			delete(drugIndexes.ByName, k)
		}
		for k := range drugIndexes.ByGUID {
			delete(drugIndexes.ByGUID, k)
		}
		drugIndexes.ByBarcode = nil
		drugIndexes.ByCode = nil
		drugIndexes.ByName = nil
		drugIndexes.ByGUID = nil
		drugIndexes.mutex.Unlock()
		drugIndexes = nil
	}

	// Очищаем invoiceDataList
	invoiceDataList = nil

	// Принудительно запускаем GC для освобождения памяти
	runtime.GC()

	// Даем GC время на работу
	runtime.Gosched()

	// Принудительно освобождаем память обратно ОС (более агрессивная очистка)
	debug.FreeOSMemory()

	// Получаем статистику памяти для логирования
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if pm.logger != nil {
		pm.logger.Info("✅ Память освобождена: in-memory структуры очищены")
		pm.logger.Info("📊 Статистика памяти: Alloc=%.2f MB, Sys=%.2f MB, NumGC=%d",
			float64(m.Alloc)/1024/1024, float64(m.Sys)/1024/1024, m.NumGC)
	}

	return nil
}

// MatchResult представляет результат сопоставления
type MatchResult struct {
	GUID_ES         string
	MatchMethod     string
	MatchConfidence float64
	SupplierName    string // Наименование из справочника для сравнения
}

// DrugRecord представляет запись препарата в памяти для быстрого поиска
type DrugRecord struct {
	GUID_ES           string
	Name              string
	Barcode           string
	KOD_ES            string
	NormalizedBarcode string // Нормализованный штрихкод
	NormalizedName    string // Нормализованное наименование
}

// DrugIndexes содержит индексы для быстрого поиска препаратов в памяти
type DrugIndexes struct {
	ByBarcode map[string][]*DrugRecord // key: нормализованный штрихкод
	ByCode    map[string][]*DrugRecord // key: KOD_ES
	ByName    map[string][]*DrugRecord // key: нормализованное наименование
	ByGUID    map[string]bool          // key: GUID_ES для быстрой проверки существования
	mutex     *sync.RWMutex
}

// loadDrugIndexes загружает весь справочник es_ef2 в память и строит индексы для быстрого поиска
func (pm *PriceMatcher) loadDrugIndexes(ctx context.Context) *DrugIndexes {
	indexes := &DrugIndexes{
		ByBarcode: make(map[string][]*DrugRecord),
		ByCode:    make(map[string][]*DrugRecord),
		ByName:    make(map[string][]*DrugRecord),
		ByGUID:    make(map[string]bool),
		mutex:     &sync.RWMutex{},
	}

	query := `
		SELECT 
			CAST(GUID_ES AS TEXT) AS GUID_ES,
			COALESCE(NAME, '') AS NAME,
			COALESCE(BARCODE, '') AS BARCODE,
			CAST(COALESCE(KOD_ES, '') AS TEXT) AS KOD_ES
		FROM es_ef2
		WHERE is_active = 1
		  AND DELETED IS NULL
	`

	rows, err := pm.database.QueryContext(ctx, query)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Error("Ошибка загрузки справочника es_ef2 в память: %v", err)
		}
		return indexes
	}
	defer rows.Close()

	recordCount := 0
	for rows.Next() {
		var guidES, name, barcode, kodES sql.NullString

		err := rows.Scan(&guidES, &name, &barcode, &kodES)
		if err != nil {
			continue
		}

		if !guidES.Valid || guidES.String == "" {
			continue
		}

		record := &DrugRecord{
			GUID_ES: guidES.String,
		}

		// Добавляем GUID_ES в индекс для быстрой проверки существования
		indexes.mutex.Lock()
		indexes.ByGUID[guidES.String] = true
		indexes.mutex.Unlock()

		if name.Valid {
			record.Name = name.String
			record.NormalizedName = strings.ToUpper(strings.TrimSpace(name.String))
			// Индекс по наименованию
			if record.NormalizedName != "" {
				indexes.mutex.Lock()
				indexes.ByName[record.NormalizedName] = append(indexes.ByName[record.NormalizedName], record)
				indexes.mutex.Unlock()
			}
		}

		if barcode.Valid && barcode.String != "" {
			record.Barcode = barcode.String
			normalizedBarcode := normalizeBarcode(barcode.String)
			if normalizedBarcode != "" {
				record.NormalizedBarcode = normalizedBarcode
				// Индекс по штрихкоду (несколько записей могут иметь один штрихкод)
				indexes.mutex.Lock()
				indexes.ByBarcode[normalizedBarcode] = append(indexes.ByBarcode[normalizedBarcode], record)
				// Также добавляем оригинальный штрихкод (нормализованный)
				if strings.TrimSpace(barcode.String) != normalizedBarcode {
					origNormalized := normalizeBarcode(strings.TrimSpace(barcode.String))
					if origNormalized != "" && origNormalized != normalizedBarcode {
						indexes.ByBarcode[origNormalized] = append(indexes.ByBarcode[origNormalized], record)
					}
				}
				indexes.mutex.Unlock()
			}
		}

		if kodES.Valid && kodES.String != "" {
			record.KOD_ES = kodES.String
			// Индекс по коду товара
			indexes.mutex.Lock()
			indexes.ByCode[kodES.String] = append(indexes.ByCode[kodES.String], record)
			indexes.mutex.Unlock()
		}

		recordCount++
	}

	if pm.logger != nil {
		pm.logger.Info("Загружено %d записей из справочника es_ef2 в память", recordCount)
	}

	return indexes
}

// findMatchWithCacheAndIndexes пытается найти соответствие, используя предзагруженный кэш и in-memory индексы
func (pm *PriceMatcher) findMatchWithCacheAndIndexes(ctx context.Context, invoiceData *models.InvoiceData, mappingCache map[string]*MatchResult, cacheMutex *sync.RWMutex, drugIndexes *DrugIndexes) *MatchResult {
	// Приоритет 0: Проверяем предзагруженный кэш в памяти
	if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
		itemCode := strings.TrimSpace(*invoiceData.ItemCode)
		cacheMutex.RLock()
		cachedMatch, found := mappingCache[itemCode]
		cacheMutex.RUnlock()
		if found && cachedMatch != nil {
			// Увеличиваем счетчик использования (асинхронно, не блокируем обработку)
			go pm.incrementMappingUseCount(ctx, invoiceData.SupplierID, itemCode)
			return cachedMatch
		}
	}
	// Если в кэше не найдено, используем in-memory поиск
	return pm.findMatchInMemory(ctx, invoiceData, drugIndexes)
}

// findMatchInMemory пытается найти соответствие используя in-memory индексы (без запросов к БД)
func (pm *PriceMatcher) findMatchInMemory(ctx context.Context, invoiceData *models.InvoiceData, drugIndexes *DrugIndexes) *MatchResult {
	// Приоритет 1: По штрихкоду
	if invoiceData.Barcode != nil && strings.TrimSpace(*invoiceData.Barcode) != "" {
		barcode := strings.TrimSpace(*invoiceData.Barcode)
		match := pm.matchByBarcodeInMemory(barcode, drugIndexes)
		if match != nil {
			// Сохраняем сопоставление в кеш, если есть код товара
			if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
				itemCode := strings.TrimSpace(*invoiceData.ItemCode)
				pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			}
			return match
		}
	}

	// Приоритет 2: По коду товара
	if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
		itemCode := strings.TrimSpace(*invoiceData.ItemCode)
		match := pm.matchByCodeInMemory(itemCode, drugIndexes)
		if match != nil {
			// Сохраняем сопоставление в кеш
			pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			return match
		}
	}

	// Приоритет 3: По наименованию
	if invoiceData.ItemName != nil && strings.TrimSpace(*invoiceData.ItemName) != "" {
		itemName := strings.TrimSpace(*invoiceData.ItemName)
		match := pm.matchByNameInMemory(itemName, drugIndexes)
		if match != nil {
			// Сохраняем сопоставление в кеш, если есть код товара
			if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
				itemCode := strings.TrimSpace(*invoiceData.ItemCode)
				pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			}
			return match
		}
	}

	return nil
}

// findMatch пытается найти соответствие в справочнике es_ef2 (fallback для случаев без in-memory индексов)
func (pm *PriceMatcher) findMatch(ctx context.Context, invoiceData *models.InvoiceData) *MatchResult {
	// Приоритет 0: Проверяем сохраненное сопоставление по коду поставщика (кеш из БД, если не загружен в память)
	if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
		itemCode := strings.TrimSpace(*invoiceData.ItemCode)
		cachedMatch := pm.getCachedMatch(ctx, invoiceData.SupplierID, itemCode)
		if cachedMatch != nil {
			// Увеличиваем счетчик использования
			pm.incrementMappingUseCount(ctx, invoiceData.SupplierID, itemCode)
			return cachedMatch
		}
	}

	// Приоритет 1: По штрихкоду
	if invoiceData.Barcode != nil && strings.TrimSpace(*invoiceData.Barcode) != "" {
		barcode := strings.TrimSpace(*invoiceData.Barcode)
		match := pm.matchByBarcode(ctx, barcode)
		if match != nil {
			// Сохраняем сопоставление в кеш, если есть код товара
			if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
				itemCode := strings.TrimSpace(*invoiceData.ItemCode)
				pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			} else {
				if pm.logger != nil {
					pm.logger.Warn("⚠️ Сопоставление по штрихкоду найдено, но ItemCode отсутствует или пустой - не сохраняем в кэш. SupplierID=%s, GUID_ES=%s",
						invoiceData.SupplierID, match.GUID_ES)
				}
			}
			return match
		}
		if pm.logger != nil {
			pm.logger.Debug("Сопоставление по штрихкоду не найдено: '%s'", barcode)
		}
	}

	// Приоритет 2: По коду товара (ItemCode -> KOD_ES или ID_ES)
	if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
		itemCode := strings.TrimSpace(*invoiceData.ItemCode)
		match := pm.matchByCode(ctx, itemCode)
		if match != nil {
			if pm.logger != nil {
				pm.logger.Info("✅ Сопоставление по коду товара найдено: ItemCode='%s' -> GUID_ES=%s", itemCode, match.GUID_ES)
				pm.logger.Info("💾 Сохранение сопоставления в кэш: SupplierID=%s, ItemCode='%s', GUID_ES=%s",
					invoiceData.SupplierID, itemCode, match.GUID_ES)
			}
			// Сохраняем сопоставление в кеш
			pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			return match
		}
	}

	// Приоритет 3: По наименованию (нечеткое совпадение)
	if invoiceData.ItemName != nil && strings.TrimSpace(*invoiceData.ItemName) != "" {
		itemName := strings.TrimSpace(*invoiceData.ItemName)
		match := pm.matchByName(ctx, itemName)
		if match != nil {
			// Сохраняем сопоставление в кеш, если есть код товара
			if invoiceData.ItemCode != nil && strings.TrimSpace(*invoiceData.ItemCode) != "" {
				itemCode := strings.TrimSpace(*invoiceData.ItemCode)
				pm.saveMapping(ctx, invoiceData.SupplierID, itemCode, match)
			} else {
				if pm.logger != nil {
					pm.logger.Warn("⚠️ Сопоставление по наименованию найдено, но ItemCode отсутствует - не сохраняем в кэш")
				}
			}
			return match
		}
	}

	return nil
}

// normalizeBarcode нормализует штрихкод для сравнения (убирает пробелы, лидирующие нули)
func normalizeBarcode(barcode string) string {
	// Убираем пробелы
	barcode = strings.TrimSpace(barcode)
	// Убираем все нецифровые символы
	result := ""
	for _, r := range barcode {
		if r >= '0' && r <= '9' {
			result += string(r)
		}
	}
	return result
}

// matchByBarcodeInMemory сопоставляет по штрихкоду используя in-memory индекс
func (pm *PriceMatcher) matchByBarcodeInMemory(barcode string, drugIndexes *DrugIndexes) *MatchResult {
	normalizedBarcode := normalizeBarcode(barcode)
	if normalizedBarcode == "" {
		return nil
	}

	drugIndexes.mutex.RLock()
	records, found := drugIndexes.ByBarcode[normalizedBarcode]
	drugIndexes.mutex.RUnlock()

	if !found || len(records) == 0 {
		return nil
	}

	// Берем первую запись
	record := records[0]
	return &MatchResult{
		GUID_ES:         record.GUID_ES,
		MatchMethod:     "BARCODE",
		MatchConfidence: 100.0,
		SupplierName:    record.Name,
	}
}

// matchByBarcode сопоставляет по штрихкоду (fallback для случаев без in-memory индексов)
func (pm *PriceMatcher) matchByBarcode(ctx context.Context, barcode string) *MatchResult {
	// Нормализуем штрихкод
	normalizedBarcode := normalizeBarcode(barcode)
	if normalizedBarcode == "" {
		return nil
	}

	// Оптимизированный запрос - один запрос вместо двух, используем для быстрого результата
	// Используем вычисляемое поле для нормализации прямо в запросе, но сначала пробуем точное совпадение
	query := `
		SELECT CAST(GUID_ES AS TEXT) AS GUID_ES, 
			NAME, 
			BARCODE
		FROM es_ef2
		WHERE is_active = 1
		  AND DELETED IS NULL
		  AND (
			-- Точное совпадение с исходным штрихкодом
			LTRIM(RTRIM(COALESCE(BARCODE, ''))) = @barcode 
			-- Точное совпадение с нормализованным штрихкодом
			OR LTRIM(RTRIM(COALESCE(BARCODE, ''))) = @normalizedBarcode
			-- Нормализованное сравнение (убираем все нецифровые символы)
			OR (
				LEN(BARCODE) > 0 
				AND LTRIM(RTRIM(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(COALESCE(BARCODE, ''), ' ', ''), '-', ''), '.', ''), '_', ''), ' ', ''))) = @normalizedBarcode
			)
		)
		ORDER BY 
			-- Приоритет: сначала точное совпадение, потом нормализованное
			CASE 
				WHEN LTRIM(RTRIM(COALESCE(BARCODE, ''))) = @barcode THEN 1
				WHEN LTRIM(RTRIM(COALESCE(BARCODE, ''))) = @normalizedBarcode THEN 2
				ELSE 3
			END
LIMIT 1
`

	var guidES, name, dbBarcode string
	err := pm.database.QueryRowContext(ctx, query,
		sql.Named("barcode", barcode),
		sql.Named("normalizedBarcode", normalizedBarcode)).Scan(&guidES, &name, &dbBarcode)

	if err == nil {
		// Определяем confidence на основе типа совпадения
		confidence := 95.0
		if dbBarcode == barcode || dbBarcode == normalizedBarcode {
			confidence = 100.0
		}

		if pm.logger != nil {
			pm.logger.Debug("Сопоставление по штрихкоду успешно: исходный='%s', нормализованный='%s', найден в БД='%s', GUID_ES=%s",
				barcode, normalizedBarcode, dbBarcode, guidES)
		}
		return &MatchResult{
			GUID_ES:         guidES,
			MatchMethod:     "BARCODE",
			MatchConfidence: confidence,
			SupplierName:    name,
		}
	}

	return nil
}

// matchByCode сопоставляет по коду товара
// matchByCodeInMemory сопоставляет по коду товара используя in-memory индекс
func (pm *PriceMatcher) matchByCodeInMemory(itemCode string, drugIndexes *DrugIndexes) *MatchResult {
	if itemCode == "" {
		return nil
	}

	drugIndexes.mutex.RLock()
	records, found := drugIndexes.ByCode[itemCode]
	drugIndexes.mutex.RUnlock()

	if !found || len(records) == 0 {
		return nil
	}

	// Берем первую запись
	record := records[0]
	return &MatchResult{
		GUID_ES:         record.GUID_ES,
		MatchMethod:     "CODE",
		MatchConfidence: 90.0,
		SupplierName:    record.Name,
	}
}

// matchByCode сопоставляет по коду товара (fallback для случаев без in-memory индексов)
func (pm *PriceMatcher) matchByCode(ctx context.Context, itemCode string) *MatchResult {
	// Пытаемся найти по KOD_ES (если код числовой)
	var guidES, name string
	var err error

	// Проверяем, является ли код числовым
	query := `
		SELECT CAST(GUID_ES AS TEXT) AS GUID_ES, NAME
		FROM es_ef2
		WHERE CAST(KOD_ES AS TEXT) = @code
		  AND is_active = 1
		  AND DELETED IS NULL
	`
	err = pm.database.QueryRowContext(ctx, query, sql.Named("code", itemCode)).Scan(&guidES, &name)

	if err == nil {
		return &MatchResult{
			GUID_ES:         guidES,
			MatchMethod:     "CODE",
			MatchConfidence: 90.0,
			SupplierName:    name,
		}
	}

	// Пытаемся найти по ID_ES
	query = `
		SELECT CAST(GUID_ES AS TEXT) AS GUID_ES, NAME
		FROM es_ef2
		WHERE CAST(ID_ES AS TEXT) = @code
		  AND is_active = 1
		  AND DELETED IS NULL
	`
	err = pm.database.QueryRowContext(ctx, query, sql.Named("code", itemCode)).Scan(&guidES, &name)

	if err == nil {
		return &MatchResult{
			GUID_ES:         guidES,
			MatchMethod:     "CODE",
			MatchConfidence: 85.0,
			SupplierName:    name,
		}
	}

	return nil
}

// matchByName сопоставляет по наименованию (нечеткий поиск)
// matchByNameInMemory сопоставляет по наименованию используя in-memory индекс
func (pm *PriceMatcher) matchByNameInMemory(itemName string, drugIndexes *DrugIndexes) *MatchResult {
	normalizedName := strings.ToUpper(strings.TrimSpace(itemName))
	if normalizedName == "" {
		return nil
	}

	// Ищем точное совпадение
	drugIndexes.mutex.RLock()
	records, found := drugIndexes.ByName[normalizedName]
	drugIndexes.mutex.RUnlock()

	if found && len(records) > 0 {
		record := records[0]
		return &MatchResult{
			GUID_ES:         record.GUID_ES,
			MatchMethod:     "NAME",
			MatchConfidence: 95.0,
			SupplierName:    record.Name,
		}
	}

	// Ищем частичное совпадение (первые слова)
	words := strings.Fields(normalizedName)
	if len(words) >= 2 {
		// Простой поиск по началу наименования
		drugIndexes.mutex.RLock()
		var bestMatch *DrugRecord
		var bestScore float64

		for key, recs := range drugIndexes.ByName {
			if strings.HasPrefix(key, words[0]) {
				// Подсчитываем совпадение слов
				keyWords := strings.Fields(key)
				score := 0.0
				if len(keyWords) > 0 && keyWords[0] == words[0] {
					score += 50.0
				}
				if len(keyWords) > 1 && keyWords[1] == words[1] {
					score += 30.0
				}
				if len(keyWords) > 2 && len(words) > 2 && keyWords[2] == words[2] {
					score += 20.0
				}

				if score > bestScore {
					bestScore = score
					bestMatch = recs[0]
				}
			}
		}
		drugIndexes.mutex.RUnlock()

		if bestMatch != nil && bestScore >= 50.0 {
			confidence := bestScore
			if confidence > 95.0 {
				confidence = 95.0
			}
			return &MatchResult{
				GUID_ES:         bestMatch.GUID_ES,
				MatchMethod:     "NAME",
				MatchConfidence: confidence,
				SupplierName:    bestMatch.Name,
			}
		}
	}

	return nil
}

// matchByName сопоставляет по наименованию (fallback для случаев без in-memory индексов)
func (pm *PriceMatcher) matchByName(ctx context.Context, itemName string) *MatchResult {
	// Нормализуем название (убираем лишние пробелы, приводим к верхнему регистру для сравнения)
	normalizedName := strings.ToUpper(strings.TrimSpace(itemName))

	// Ищем точное совпадение (SQL Server не поддерживает TRIM напрямую, используем LTRIM/RTRIM)
	query := `
		SELECT CAST(GUID_ES AS TEXT) AS GUID_ES, NAME
		FROM es_ef2
		WHERE UPPER(LTRIM(RTRIM(NAME))) = @name
		  AND is_active = 1
		  AND DELETED IS NULL
LIMIT 1
`

	var guidES, name string
	err := pm.database.QueryRowContext(ctx, query, sql.Named("name", normalizedName)).Scan(&guidES, &name)
	if err == nil {
		return &MatchResult{
			GUID_ES:         guidES,
			MatchMethod:     "NAME",
			MatchConfidence: 95.0,
			SupplierName:    name,
		}
	}

	// Ищем частичное совпадение (название содержит ключевые слова)
	// Берем первые 3 слова из названия для поиска
	words := strings.Fields(normalizedName)
	if len(words) >= 2 {
		searchPattern := words[0] + "%" + words[1] + "%"
		query = `
			SELECT CAST(GUID_ES AS TEXT) AS GUID_ES, NAME
			FROM es_ef2
			WHERE UPPER(NAME) LIKE @pattern
			  AND is_active = 1
			  AND DELETED IS NULL
			ORDER BY LEN(NAME) ASC
LIMIT 1
`
		err = pm.database.QueryRowContext(ctx, query, sql.Named("pattern", searchPattern)).Scan(&guidES, &name)
		if err == nil {
			return &MatchResult{
				GUID_ES:         guidES,
				MatchMethod:     "NAME",
				MatchConfidence: 70.0,
				SupplierName:    name,
			}
		}
	}

	return nil
}

// saveSupplierPrice сохраняет сопоставление в таблицу SupplierPrice
// Проверяет существующие прайсы по той же точке импорта и обновляет их с сохранением истории
func (pm *PriceMatcher) saveSupplierPrice(ctx context.Context, invoiceData models.InvoiceData, match *MatchResult) error {
	// Проверяем, что GUID_ES не пустой и валидный
	if match.GUID_ES == "" {
		if pm.logger != nil {
			pm.logger.Warn("Попытка сохранить запись без GUID_ES, пропускаем")
		}
		return fmt.Errorf("GUID_ES не может быть пустым")
	}

	// Проверяем валидность UUID формата
	_, err := uuid.Parse(match.GUID_ES)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Warn("Неправильный формат GUID_ES: %s, ошибка: %v", match.GUID_ES, err)
		}
		return fmt.Errorf("неправильный формат GUID_ES: %w", err)
	}

	// Проверяем, что Price не nil
	if invoiceData.Price == nil {
		if pm.logger != nil {
			pm.logger.Warn("Попытка сохранить запись без цены, пропускаем")
		}
		return fmt.Errorf("цена не может быть пустой")
	}

	// Получаем ImportPointID из InvoiceImport для проверки существующих прайсов и определения PriceListID
	var importPointID string
	var priceListID sql.NullString
	importPointQuery := `
		SELECT CAST(ii.ImportPointID AS TEXT) AS ImportPointID,
		       CAST(pl.PriceListID AS TEXT) AS PriceListID
		FROM InvoiceImport ii
		LEFT JOIN PriceList pl ON pl.ImportPointID = ii.ImportPointID AND pl.IsActive = 1
		WHERE ii.InvoiceImportID = CAST(@invoiceImportID AS UUID)
	`
	err = pm.database.QueryRowContext(ctx, importPointQuery, sql.Named("invoiceImportID", invoiceData.InvoiceImportID)).Scan(&importPointID, &priceListID)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Warn("Не удалось получить ImportPointID для InvoiceImport %s: %v", invoiceData.InvoiceImportID, err)
		}
		// Продолжаем без проверки существующих прайсов
		importPointID = ""
	}

	// Ищем существующий прайс для того же поставщика и товара из той же точки импорта
	var existingSupplierPriceID sql.NullString
	var existingPrice sql.NullFloat64

	if importPointID != "" {
		// Ищем по SupplierID + GUID_ES + ImportPointID (или ItemCode, если GUID_ES отсутствует)
		findExistingQuery := `
			SELECT CAST(sp.SupplierPriceID AS TEXT) AS SupplierPriceID,
				sp.Price
			FROM SupplierPrice sp
			INNER JOIN InvoiceImport ii ON sp.InvoiceImportID = ii.InvoiceImportID
			WHERE sp.SupplierID = CAST(@supplierID AS UUID)
			  AND ii.ImportPointID = CAST(@importPointID AS UUID)
			  AND sp.IsActive = 1
LIMIT 1
`

		// Если есть GUID_ES, ищем по нему, иначе по ItemCode
		if match.GUID_ES != "" {
			findExistingQuery += ` AND sp.GUID_ES = CAST(@guidES AS UUID)`
		} else if invoiceData.ItemCode != nil && *invoiceData.ItemCode != "" {
			findExistingQuery += ` AND sp.ItemCode = @itemCode`
		} else {
			// Не можем найти без GUID_ES или ItemCode
			findExistingQuery = ""
		}

		if findExistingQuery != "" {
			var args []interface{}
			args = append(args, sql.Named("supplierID", invoiceData.SupplierID))
			args = append(args, sql.Named("importPointID", importPointID))
			if match.GUID_ES != "" {
				args = append(args, sql.Named("guidES", match.GUID_ES))
			} else if invoiceData.ItemCode != nil {
				args = append(args, sql.Named("itemCode", *invoiceData.ItemCode))
			}

			err = pm.database.QueryRowContext(ctx, findExistingQuery, args...).Scan(&existingSupplierPriceID, &existingPrice)
			if err != nil && err != sql.ErrNoRows {
				if pm.logger != nil {
					pm.logger.Warn("Ошибка поиска существующего прайса: %v", err)
				}
			}
		}
	}

	if existingSupplierPriceID.Valid && existingSupplierPriceID.String != "" {
		// Получаем текущие значения для сравнения
		var oldItemName, oldItemCode, oldBarcode, oldBatchNumber, oldInvoiceNumber sql.NullString
		var oldQuantity sql.NullFloat64
		var oldExpiryDate, oldInvoiceDate sql.NullTime

		getOldValuesQuery := `
			SELECT ItemName, ItemCode, Barcode, Quantity, BatchNumber, ExpiryDate, InvoiceNumber, InvoiceDate
			FROM SupplierPrice
			WHERE SupplierPriceID = CAST(@supplierPriceID AS UUID)
		`
		err = pm.database.QueryRowContext(ctx, getOldValuesQuery,
			sql.Named("supplierPriceID", existingSupplierPriceID.String),
		).Scan(&oldItemName, &oldItemCode, &oldBarcode, &oldQuantity, &oldBatchNumber, &oldExpiryDate, &oldInvoiceNumber, &oldInvoiceDate)

		// Сохраняем историю всех изменений
		err = pm.saveFullHistory(ctx, existingSupplierPriceID.String, invoiceData, match,
			existingPrice, oldItemName, oldItemCode, oldBarcode, oldQuantity, oldBatchNumber, oldExpiryDate, oldInvoiceNumber, oldInvoiceDate)
		if err != nil {
			if pm.logger != nil {
				pm.logger.Warn("Ошибка сохранения истории изменений: %v", err)
			}
		}

		// Обновляем прайс
		updateQuery := `
			UPDATE SupplierPrice 
			SET InvoiceImportID = CAST(@invoiceImportID AS UUID),
			    InvoiceDataID = CAST(@invoiceDataID AS UUID),
			    ItemCode = @itemCode,
			    ItemName = @itemName,
			    SupplierItemName = @supplierItemName,
			    Barcode = @barcode,
			    Price = @price,
			    Quantity = @quantity,
			    InvoiceNumber = @invoiceNumber,
			    InvoiceDate = @invoiceDate,
			    BatchNumber = @batchNumber,
			    ExpiryDate = @expiryDate,
			    Manufacturer = @manufacturer,
			    Country = @country,
			    MatchMethod = @matchMethod,
			    MatchConfidence = @matchConfidence,
			    PriceListID = CASE WHEN @priceListID IS NOT NULL AND @priceListID != '' THEN CAST(@priceListID AS UUID) ELSE NULL END,
			    UpdatedAt = (NOW() AT TIME ZONE 'utc')
			WHERE SupplierPriceID = CAST(@supplierPriceID AS UUID)
		`

		args := []interface{}{
			sql.Named("supplierPriceID", existingSupplierPriceID.String),
			sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
			sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
			sql.Named("itemCode", invoiceData.ItemCode),
			sql.Named("itemName", invoiceData.ItemName),
			sql.Named("supplierItemName", match.SupplierName),
			sql.Named("barcode", invoiceData.Barcode),
			sql.Named("price", invoiceData.Price),
			sql.Named("quantity", invoiceData.Quantity),
			sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
			sql.Named("invoiceDate", invoiceData.InvoiceDate),
			sql.Named("batchNumber", invoiceData.BatchNumber),
			sql.Named("expiryDate", invoiceData.ExpiryDate),
			sql.Named("manufacturer", invoiceData.Manufacturer),
			sql.Named("country", invoiceData.Country),
			sql.Named("matchMethod", match.MatchMethod),
			sql.Named("matchConfidence", match.MatchConfidence),
		}

		if priceListID.Valid && priceListID.String != "" {
			args = append(args, sql.Named("priceListID", priceListID.String))
		} else {
			args = append(args, sql.Named("priceListID", nil))
		}

		if pm.logger != nil {
			pm.logger.Debug("Обновление существующего SupplierPrice: SupplierPriceID=%s, SupplierID=%s, GUID_ES=%s, Price=%v",
				existingSupplierPriceID.String, invoiceData.SupplierID, match.GUID_ES, invoiceData.Price)
		}

		_, err = pm.database.ExecContext(ctx, updateQuery, args...)

		if err != nil {
			if pm.logger != nil {
				pm.logger.Error("Ошибка выполнения UPDATE в SupplierPrice: %v, SupplierPriceID=%s",
					err, existingSupplierPriceID.String)
			}
			return fmt.Errorf("ошибка обновления SupplierPrice: %w", err)
		}

		return nil
	}

	// Создаем новый прайс
	// Проверяем уникальность ItemCode в рамках PriceListID, если он указан
	if priceListID.Valid && priceListID.String != "" && invoiceData.ItemCode != nil && *invoiceData.ItemCode != "" {
		var existingPriceID sql.NullString
		checkUniqueQuery := `
			SELECT CAST(SupplierPriceID AS TEXT)
			FROM SupplierPrice
			WHERE PriceListID = CAST(@priceListID AS UUID)
			  AND ItemCode = @itemCode
			  AND IsActive = 1
LIMIT 1
`
		err = pm.database.QueryRowContext(ctx, checkUniqueQuery,
			sql.Named("priceListID", priceListID.String),
			sql.Named("itemCode", *invoiceData.ItemCode),
		).Scan(&existingPriceID)

		if err == nil && existingPriceID.Valid {
			// Найден дубликат - получаем старые значения и сохраняем историю
			var oldPrice sql.NullFloat64
			var oldItemName, oldItemCode, oldBarcode, oldBatchNumber, oldInvoiceNumber sql.NullString
			var oldQuantity sql.NullFloat64
			var oldExpiryDate, oldInvoiceDate sql.NullTime

			getOldValuesQuery := `
				SELECT Price, ItemName, ItemCode, Barcode, Quantity, BatchNumber, ExpiryDate, InvoiceNumber, InvoiceDate
				FROM SupplierPrice
				WHERE SupplierPriceID = CAST(@supplierPriceID AS UUID)
			`
			err = pm.database.QueryRowContext(ctx, getOldValuesQuery,
				sql.Named("supplierPriceID", existingPriceID.String),
			).Scan(&oldPrice, &oldItemName, &oldItemCode, &oldBarcode, &oldQuantity, &oldBatchNumber, &oldExpiryDate, &oldInvoiceNumber, &oldInvoiceDate)

			// Сохраняем историю всех изменений
			if err == nil {
				err = pm.saveFullHistory(ctx, existingPriceID.String, invoiceData, match,
					oldPrice, oldItemName, oldItemCode, oldBarcode, oldQuantity, oldBatchNumber, oldExpiryDate, oldInvoiceNumber, oldInvoiceDate)
				if err != nil && pm.logger != nil {
					pm.logger.Warn("Ошибка сохранения истории изменений для дубликата: %v", err)
				}
			}

			// Обновляем существующий
			updateQuery := `
				UPDATE SupplierPrice 
				SET InvoiceImportID = CAST(@invoiceImportID AS UUID),
				    InvoiceDataID = CAST(@invoiceDataID AS UUID),
				    ItemName = @itemName,
				    SupplierItemName = @supplierItemName,
				    Barcode = @barcode,
				    Price = @price,
				    Quantity = @quantity,
				    InvoiceNumber = @invoiceNumber,
				    InvoiceDate = @invoiceDate,
				    BatchNumber = @batchNumber,
				    ExpiryDate = @expiryDate,
				    Manufacturer = @manufacturer,
				    Country = @country,
				    MatchMethod = @matchMethod,
				    MatchConfidence = @matchConfidence,
				    UpdatedAt = (NOW() AT TIME ZONE 'utc')
				WHERE SupplierPriceID = CAST(@supplierPriceID AS UUID)
			`
			_, err = pm.database.ExecContext(ctx, updateQuery,
				sql.Named("supplierPriceID", existingPriceID.String),
				sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
				sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
				sql.Named("itemName", invoiceData.ItemName),
				sql.Named("supplierItemName", match.SupplierName),
				sql.Named("barcode", invoiceData.Barcode),
				sql.Named("price", invoiceData.Price),
				sql.Named("quantity", invoiceData.Quantity),
				sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
				sql.Named("invoiceDate", invoiceData.InvoiceDate),
				sql.Named("batchNumber", invoiceData.BatchNumber),
				sql.Named("expiryDate", invoiceData.ExpiryDate),
				sql.Named("manufacturer", invoiceData.Manufacturer),
				sql.Named("country", invoiceData.Country),
				sql.Named("matchMethod", match.MatchMethod),
				sql.Named("matchConfidence", match.MatchConfidence))
			if err != nil {
				if pm.logger != nil {
					pm.logger.Error("Ошибка обновления дубликата SupplierPrice: %v", err)
				}
				return fmt.Errorf("ошибка обновления дубликата SupplierPrice: %w", err)
			}
			return nil
		}
	}

	query := `
		INSERT INTO SupplierPrice 
		(SupplierPriceID, SupplierID, InvoiceImportID, InvoiceDataID, GUID_ES,
		 ItemCode, ItemName, SupplierItemName, Barcode, Price, Quantity,
		 InvoiceNumber, InvoiceDate, BatchNumber, ExpiryDate,
		 Manufacturer, Country,
		 MatchMethod, MatchConfidence, IsActive, PriceListID, CreatedAt, UpdatedAt)
		VALUES 
		(CAST(@supplierPriceID AS UUID), CAST(@supplierID AS UUID),
		 CAST(@invoiceImportID AS UUID), CAST(@invoiceDataID AS UUID),
		 CASE WHEN @guidES IS NOT NULL AND @guidES != '' THEN CAST(@guidES AS UUID) ELSE NULL END,
		 @itemCode, @itemName, @supplierItemName, @barcode, @price, @quantity,
		 @invoiceNumber, @invoiceDate, @batchNumber, @expiryDate,
		 @manufacturer, @country,
		 @matchMethod, @matchConfidence, 1,
		 CASE WHEN @priceListID IS NOT NULL AND @priceListID != '' THEN CAST(@priceListID AS UUID) ELSE NULL END,
		 (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'))
	`

	supplierPriceID := uuid.New().String()

	if pm.logger != nil {
		pm.logger.Debug("Создание нового SupplierPrice: SupplierID=%s, GUID_ES=%s, Price=%v",
			invoiceData.SupplierID, match.GUID_ES, invoiceData.Price)
	}

	insertArgs := []interface{}{
		sql.Named("supplierPriceID", supplierPriceID),
		sql.Named("supplierID", invoiceData.SupplierID),
		sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
		sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
		sql.Named("guidES", match.GUID_ES),
		sql.Named("itemCode", invoiceData.ItemCode),
		sql.Named("itemName", invoiceData.ItemName),
		sql.Named("supplierItemName", invoiceData.ItemName), // Используем ItemName из invoiceData
		sql.Named("barcode", invoiceData.Barcode),
		sql.Named("price", invoiceData.Price),
		sql.Named("quantity", invoiceData.Quantity),
		sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
		sql.Named("invoiceDate", invoiceData.InvoiceDate),
		sql.Named("batchNumber", invoiceData.BatchNumber),
		sql.Named("expiryDate", invoiceData.ExpiryDate),
		sql.Named("manufacturer", invoiceData.Manufacturer),
		sql.Named("country", invoiceData.Country),
		sql.Named("matchMethod", match.MatchMethod),
		sql.Named("matchConfidence", match.MatchConfidence),
	}

	if priceListID.Valid && priceListID.String != "" {
		insertArgs = append(insertArgs, sql.Named("priceListID", priceListID.String))
	} else {
		insertArgs = append(insertArgs, sql.Named("priceListID", nil))
	}

	_, err = pm.database.ExecContext(ctx, query, insertArgs...)

	if err != nil {
		if pm.logger != nil {
			pm.logger.Error("Ошибка выполнения INSERT в SupplierPrice: %v, GUID_ES=%s, SupplierID=%s",
				err, match.GUID_ES, invoiceData.SupplierID)
		}
		return fmt.Errorf("ошибка сохранения SupplierPrice: %w", err)
	}

	return nil
}

// saveSupplierPriceWithoutMatch сохраняет несопоставленную запись в SupplierPrice с NULL GUID_ES
// для возможности ручного сопоставления на странице сопоставления
// cachedPriceListID - кэшированный PriceListID, чтобы не делать запрос для каждой записи
func (pm *PriceMatcher) saveSupplierPriceWithoutMatch(ctx context.Context, invoiceData models.InvoiceData, cachedPriceListID sql.NullString) error {
	// Проверяем, что Price не nil
	if invoiceData.Price == nil {
		if pm.logger != nil {
			pm.logger.Warn("Попытка сохранить запись без цены, пропускаем")
		}
		return fmt.Errorf("цена не может быть пустой")
	}

	// Используем кэшированный PriceListID, чтобы не делать запрос для каждой записи
	priceListID := cachedPriceListID

	// Получаем InvoiceImportID
	invoiceImportID := invoiceData.InvoiceImportID
	if invoiceImportID == "" {
		return fmt.Errorf("InvoiceImportID не может быть пустым")
	}

	// Создаем новую запись в SupplierPrice с NULL GUID_ES
	supplierPriceID := uuid.New().String()
	insertQuery := `
		INSERT INTO SupplierPrice 
		(SupplierPriceID, SupplierID, InvoiceImportID, InvoiceDataID, GUID_ES,
		 ItemCode, ItemName, SupplierItemName, Barcode, Price, Quantity,
		 InvoiceNumber, InvoiceDate, BatchNumber, ExpiryDate,
		 Manufacturer, Country,
		 MatchMethod, MatchConfidence, IsActive, PriceListID, CreatedAt, UpdatedAt)
		VALUES 
		(CAST(@supplierPriceID AS UUID), CAST(@supplierID AS UUID),
		 CAST(@invoiceImportID AS UUID), CAST(@invoiceDataID AS UUID),
		 NULL, -- GUID_ES NULL для несопоставленных записей
		 @itemCode, @itemName, @supplierItemName, @barcode, @price, @quantity,
		 @invoiceNumber, @invoiceDate, @batchNumber, @expiryDate,
		 @manufacturer, @country,
		 NULL, -- MatchMethod NULL
		 NULL, -- MatchConfidence NULL
		 1, -- IsActive = true
		 CASE WHEN @priceListID IS NOT NULL AND @priceListID != '' THEN CAST(@priceListID AS UUID) ELSE NULL END,
		 (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'))
	`

	args := []interface{}{
		sql.Named("supplierPriceID", supplierPriceID),
		sql.Named("supplierID", invoiceData.SupplierID),
		sql.Named("invoiceImportID", invoiceImportID),
		sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
		sql.Named("itemCode", invoiceData.ItemCode),
		sql.Named("itemName", invoiceData.ItemName),         // Исправлено: ItemName для ItemName
		sql.Named("supplierItemName", invoiceData.ItemName), // Наименование от поставщика
		sql.Named("barcode", invoiceData.Barcode),
		sql.Named("price", *invoiceData.Price),
		sql.Named("quantity", invoiceData.Quantity),
		sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
		sql.Named("invoiceDate", invoiceData.InvoiceDate),
		sql.Named("batchNumber", invoiceData.BatchNumber),
		sql.Named("expiryDate", invoiceData.ExpiryDate),
		sql.Named("manufacturer", invoiceData.Manufacturer),
		sql.Named("country", invoiceData.Country),
	}

	if priceListID.Valid && priceListID.String != "" {
		args = append(args, sql.Named("priceListID", priceListID.String))
	} else {
		args = append(args, sql.Named("priceListID", nil))
	}

	_, err := pm.database.ExecContext(ctx, insertQuery, args...)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Error("Ошибка выполнения INSERT несопоставленного SupplierPrice: %v, SupplierID=%s",
				err, invoiceData.SupplierID)
		}
		return fmt.Errorf("ошибка сохранения несопоставленного SupplierPrice: %w", err)
	}

	if pm.logger != nil {
		pm.logger.Debug("Создана несопоставленная запись SupplierPrice: SupplierPriceID=%s, SupplierID=%s, ItemName=%s",
			supplierPriceID, invoiceData.SupplierID, getStringPtr(invoiceData.ItemName))
	}

	return nil
}

// savePriceHistory сохраняет запись об изменении цены в историю (старая функция для обратной совместимости)
func (pm *PriceMatcher) savePriceHistory(ctx context.Context, supplierPriceID string, oldPrice, newPrice float64, invoiceImportID string) error {
	query := `
		INSERT INTO SupplierPriceHistory 
		(PriceHistoryID, SupplierPriceID, OldPrice, NewPrice, ChangedAt, ChangedByInvoiceImportID, Reason, ChangeType)
		VALUES 
		(gen_random_uuid(), CAST(@supplierPriceID AS UUID), @oldPrice, @newPrice, 
		 (NOW() AT TIME ZONE 'utc'), CAST(@invoiceImportID AS UUID), @reason, @changeType)
	`

	reason := "PRICE_UPDATE"
	changeType := "PRICE"
	if oldPrice == 0 {
		reason = "PRICE_CREATE"
	}

	_, err := pm.database.ExecContext(ctx, query,
		sql.Named("supplierPriceID", supplierPriceID),
		sql.Named("oldPrice", oldPrice),
		sql.Named("newPrice", newPrice),
		sql.Named("invoiceImportID", invoiceImportID),
		sql.Named("reason", reason),
		sql.Named("changeType", changeType),
	)

	if err != nil {
		return fmt.Errorf("ошибка сохранения истории цены: %w", err)
	}

	return nil
}

// saveFullHistory сохраняет полную историю изменений товара и цены
func (pm *PriceMatcher) saveFullHistory(ctx context.Context, supplierPriceID string, invoiceData models.InvoiceData, match *MatchResult,
	oldPrice sql.NullFloat64, oldItemName, oldItemCode, oldBarcode sql.NullString,
	oldQuantity sql.NullFloat64, oldBatchNumber sql.NullString, oldExpiryDate sql.NullTime,
	oldInvoiceNumber sql.NullString, oldInvoiceDate sql.NullTime) error {

	// Определяем тип изменений
	var changeTypes []string
	var hasChanges bool

	newPrice := 0.0
	if invoiceData.Price != nil {
		newPrice = *invoiceData.Price
	}

	// Проверяем изменения цены
	if oldPrice.Valid && oldPrice.Float64 != newPrice {
		changeTypes = append(changeTypes, "PRICE")
		hasChanges = true
	} else if !oldPrice.Valid && newPrice > 0 {
		changeTypes = append(changeTypes, "PRICE")
		hasChanges = true
	}

	// Проверяем изменения товара
	if oldItemName.Valid {
		newItemName := ""
		if invoiceData.ItemName != nil {
			newItemName = *invoiceData.ItemName
		}
		if oldItemName.String != newItemName {
			changeTypes = append(changeTypes, "ITEM_NAME")
			hasChanges = true
		}
	}

	if oldItemCode.Valid {
		newItemCode := ""
		if invoiceData.ItemCode != nil {
			newItemCode = *invoiceData.ItemCode
		}
		if oldItemCode.String != newItemCode {
			changeTypes = append(changeTypes, "ITEM_CODE")
			hasChanges = true
		}
	}

	if oldBarcode.Valid {
		newBarcode := ""
		if invoiceData.Barcode != nil {
			newBarcode = *invoiceData.Barcode
		}
		if oldBarcode.String != newBarcode {
			changeTypes = append(changeTypes, "BARCODE")
			hasChanges = true
		}
	}

	// Проверяем изменения количества, партии, срока годности, прайса
	if invoiceData.Quantity != nil || oldQuantity.Valid {
		oldQty := 0.0
		newQty := 0.0
		if oldQuantity.Valid {
			oldQty = oldQuantity.Float64
		}
		if invoiceData.Quantity != nil {
			newQty = *invoiceData.Quantity
		}
		if oldQty != newQty {
			changeTypes = append(changeTypes, "QUANTITY")
			hasChanges = true
		}
	}
	if invoiceData.BatchNumber != nil || oldBatchNumber.Valid {
		oldBatch := ""
		newBatch := ""
		if oldBatchNumber.Valid {
			oldBatch = oldBatchNumber.String
		}
		if invoiceData.BatchNumber != nil {
			newBatch = *invoiceData.BatchNumber
		}
		if oldBatch != newBatch {
			changeTypes = append(changeTypes, "BATCH")
			hasChanges = true
		}
	}
	if invoiceData.ExpiryDate != nil || oldExpiryDate.Valid {
		changeTypes = append(changeTypes, "EXPIRY")
		hasChanges = true
	}
	if invoiceData.InvoiceNumber != nil || oldInvoiceNumber.Valid {
		oldInvNum := ""
		newInvNum := ""
		if oldInvoiceNumber.Valid {
			oldInvNum = oldInvoiceNumber.String
		}
		if invoiceData.InvoiceNumber != nil {
			newInvNum = *invoiceData.InvoiceNumber
		}
		if oldInvNum != newInvNum {
			changeTypes = append(changeTypes, "INVOICE")
			hasChanges = true
		}
	}

	if !hasChanges {
		// Нет изменений, не сохраняем историю
		return nil
	}

	changeType := "ALL"
	if len(changeTypes) == 1 {
		changeType = changeTypes[0]
	} else if len(changeTypes) > 1 {
		changeType = strings.Join(changeTypes, ",")
	}

	query := `
		INSERT INTO SupplierPriceHistory 
		(PriceHistoryID, SupplierPriceID, 
		 OldPrice, NewPrice,
		 OldItemName, NewItemName,
		 OldItemCode, NewItemCode,
		 OldBarcode, NewBarcode,
		 OldQuantity, NewQuantity,
		 OldBatchNumber, NewBatchNumber,
		 OldExpiryDate, NewExpiryDate,
		 OldInvoiceNumber, NewInvoiceNumber,
		 OldInvoiceDate, NewInvoiceDate,
		 ChangedAt, ChangedByInvoiceImportID, ChangeType, Reason)
		VALUES 
		(gen_random_uuid(), CAST(@supplierPriceID AS UUID),
		 @oldPrice, @newPrice,
		 @oldItemName, @newItemName,
		 @oldItemCode, @newItemCode,
		 @oldBarcode, @newBarcode,
		 @oldQuantity, @newQuantity,
		 @oldBatchNumber, @newBatchNumber,
		 @oldExpiryDate, @newExpiryDate,
		 @oldInvoiceNumber, @newInvoiceNumber,
		 @oldInvoiceDate, @newInvoiceDate,
		 (NOW() AT TIME ZONE 'utc'), CAST(@invoiceImportID AS UUID), @changeType, @reason)
	`

	reason := "AUTO_UPDATE"
	if !oldPrice.Valid {
		reason = "AUTO_CREATE"
	}

	args := []interface{}{
		sql.Named("supplierPriceID", supplierPriceID),
		sql.Named("oldPrice", oldPrice),
		sql.Named("newPrice", newPrice),
		sql.Named("oldItemName", oldItemName),
		sql.Named("newItemName", invoiceData.ItemName),
		sql.Named("oldItemCode", oldItemCode),
		sql.Named("newItemCode", invoiceData.ItemCode),
		sql.Named("oldBarcode", oldBarcode),
		sql.Named("newBarcode", invoiceData.Barcode),
		sql.Named("oldQuantity", oldQuantity),
		sql.Named("newQuantity", invoiceData.Quantity),
		sql.Named("oldBatchNumber", oldBatchNumber),
		sql.Named("newBatchNumber", invoiceData.BatchNumber),
		sql.Named("oldExpiryDate", oldExpiryDate),
		sql.Named("newExpiryDate", invoiceData.ExpiryDate),
		sql.Named("oldInvoiceNumber", oldInvoiceNumber),
		sql.Named("newInvoiceNumber", invoiceData.InvoiceNumber),
		sql.Named("oldInvoiceDate", oldInvoiceDate),
		sql.Named("newInvoiceDate", invoiceData.InvoiceDate),
		sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
		sql.Named("changeType", changeType),
		sql.Named("reason", reason),
	}

	_, err := pm.database.ExecContext(ctx, query, args...)
	return err
}

// getCachedMatch получает сохраненное сопоставление из таблицы SupplierItemMapping
func (pm *PriceMatcher) getCachedMatch(ctx context.Context, supplierID, itemCode string) *MatchResult {
	query := `
		SELECT CAST(GUID_ES AS TEXT) AS GUID_ES,
			MatchMethod,
			MatchConfidence
		FROM SupplierItemMapping
		WHERE SupplierID = CAST(@supplierID AS UUID)
		  AND ItemCode = @itemCode
		ORDER BY UseCount DESC, LastUsedAt DESC
LIMIT 1
`

	var guidES, matchMethod sql.NullString
	var matchConfidence sql.NullFloat64

	err := pm.database.QueryRowContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("itemCode", itemCode),
	).Scan(&guidES, &matchMethod, &matchConfidence)

	if err != nil {
		if err == sql.ErrNoRows {
			if pm.logger != nil {
				pm.logger.Debug("Кэш не найден в БД для SupplierID=%s, ItemCode='%s'", supplierID, itemCode)
			}
		} else {
			if pm.logger != nil {
				pm.logger.Warn("Ошибка получения кэша из БД: SupplierID=%s, ItemCode='%s', ошибка: %v", supplierID, itemCode, err)
			}
		}
		return nil
	}

	if guidES.Valid && guidES.String != "" {
		// Проверяем, что GUID_ES существует в справочнике
		checkQuery := `
			SELECT CAST(GUID_ES AS TEXT) AS GUID_ES
			FROM es_ef2
			WHERE GUID_ES = CAST(@guidES AS UUID)
			  AND is_active = 1
			  AND DELETED IS NULL
LIMIT 1
`
		var validGUID sql.NullString
		err = pm.database.QueryRowContext(ctx, checkQuery, sql.Named("guidES", guidES.String)).Scan(&validGUID)
		if err == nil && validGUID.Valid {
			method := "CACHED"
			if matchMethod.Valid {
				method = matchMethod.String
			}
			confidence := 100.0
			if matchConfidence.Valid {
				confidence = matchConfidence.Float64
			}
			if pm.logger != nil {
				pm.logger.Debug("Кэш найден и валиден: SupplierID=%s, ItemCode='%s', GUID_ES=%s, Method=%s, Confidence=%.2f",
					supplierID, itemCode, guidES.String, method, confidence)
			}
			return &MatchResult{
				GUID_ES:         guidES.String,
				MatchMethod:     method,
				MatchConfidence: confidence,
				SupplierName:    "", // Не загружаем имя для кешированных записей
			}
		} else {
			if pm.logger != nil {
				pm.logger.Warn("Кэш найден, но GUID_ES не существует в справочнике: SupplierID=%s, ItemCode='%s', GUID_ES=%s",
					supplierID, itemCode, guidES.String)
			}
		}
	}

	return nil
}

// saveMapping сохраняет сопоставление в таблицу SupplierItemMapping
func (pm *PriceMatcher) saveMapping(ctx context.Context, supplierID, itemCode string, match *MatchResult) {
	// Подробное логирование входных параметров
	if pm.logger != nil {
		pm.logger.Debug("💾 saveMapping вызван: SupplierID=%s, ItemCode='%s', Match=%v", supplierID, itemCode, match != nil)
		if match != nil {
			pm.logger.Debug("   Match details: GUID_ES=%s, Method=%s, Confidence=%.2f", match.GUID_ES, match.MatchMethod, match.MatchConfidence)
		}
	}

	if match == nil {
		if pm.logger != nil {
			pm.logger.Warn("⚠️ saveMapping: match == nil, пропускаем сохранение")
		}
		return
	}

	if match.GUID_ES == "" {
		if pm.logger != nil {
			pm.logger.Warn("⚠️ saveMapping: GUID_ES пустой, пропускаем сохранение")
		}
		return
	}

	if itemCode == "" {
		if pm.logger != nil {
			pm.logger.Warn("⚠️ saveMapping: ItemCode пустой, пропускаем сохранение. SupplierID=%s, GUID_ES=%s", supplierID, match.GUID_ES)
		}
		return
	}

	// Проверяем валидность UUID
	_, err := uuid.Parse(supplierID)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Warn("❌ saveMapping: Неправильный формат SupplierID для сохранения сопоставления: %s, ошибка: %v", supplierID, err)
		}
		return
	}

	_, err = uuid.Parse(match.GUID_ES)
	if err != nil {
		if pm.logger != nil {
			pm.logger.Warn("❌ saveMapping: Неправильный формат GUID_ES для сохранения сопоставления: %s, ошибка: %v", match.GUID_ES, err)
		}
		return
	}

	// Используем INSERT ... ON CONFLICT (UPSERT) для обновления или вставки
	query := `
		INSERT INTO "SupplierItemMapping" (
			"SupplierID", "ItemCode", "GUID_ES", "MatchMethod", "MatchConfidence",
			"UseCount", "LastUsedAt", "CreatedAt", "UpdatedAt"
		) VALUES (
			CAST(@supplierID AS UUID), @itemCode, CAST(@guidES AS UUID),
			@matchMethod, @matchConfidence, 1,
			(NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc')
		)
		ON CONFLICT ("SupplierID", "ItemCode") DO UPDATE SET
			"GUID_ES" = EXCLUDED."GUID_ES",
			"MatchMethod" = EXCLUDED."MatchMethod",
			"MatchConfidence" = EXCLUDED."MatchConfidence",
			"UseCount" = "SupplierItemMapping"."UseCount" + 1,
			"LastUsedAt" = (NOW() AT TIME ZONE 'utc'),
			"UpdatedAt" = (NOW() AT TIME ZONE 'utc');
	`

	if pm.logger != nil {
		pm.logger.Debug("💾 Выполняем UPSERT для SupplierItemMapping: SupplierID=%s, ItemCode='%s', GUID_ES=%s", supplierID, itemCode, match.GUID_ES)
	}

	result, err := pm.database.ExecContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("itemCode", itemCode),
		sql.Named("guidES", match.GUID_ES),
		sql.Named("matchMethod", match.MatchMethod),
		sql.Named("matchConfidence", match.MatchConfidence),
	)

	if err != nil {
		if pm.logger != nil {
			pm.logger.Error("❌ Ошибка сохранения сопоставления в кеш: SupplierID=%s, ItemCode='%s', GUID_ES=%s, ошибка: %v",
				supplierID, itemCode, match.GUID_ES, err)
		}
	} else {
		rowsAffected, _ := result.RowsAffected()
		if pm.logger != nil {
			pm.logger.Info("✅ Сопоставление успешно сохранено в кеш: SupplierID=%s, ItemCode='%s', GUID_ES=%s, Method=%s, Confidence=%.2f, RowsAffected=%d",
				supplierID, itemCode, match.GUID_ES, match.MatchMethod, match.MatchConfidence, rowsAffected)
		}
	}
}

// incrementMappingUseCount увеличивает счетчик использования сопоставления
func (pm *PriceMatcher) incrementMappingUseCount(ctx context.Context, supplierID, itemCode string) {
	query := `
		UPDATE SupplierItemMapping
		SET UseCount = UseCount + 1,
		    LastUsedAt = (NOW() AT TIME ZONE 'utc'),
		    UpdatedAt = (NOW() AT TIME ZONE 'utc')
		WHERE SupplierID = CAST(@supplierID AS UUID)
		  AND ItemCode = @itemCode
	`

	_, err := pm.database.ExecContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("itemCode", itemCode),
	)

	if err != nil && pm.logger != nil {
		pm.logger.Warn("Ошибка обновления счетчика использования сопоставления: %v", err)
	}
}

// SaveMappingForItem сохраняет сопоставление в кеш (публичный метод для внешнего использования)
func (pm *PriceMatcher) SaveMappingForItem(ctx context.Context, supplierID, itemCode, guidES, matchMethod string, matchConfidence float64) error {
	if supplierID == "" || itemCode == "" || guidES == "" {
		return fmt.Errorf("supplierID, itemCode и guidES не могут быть пустыми")
	}

	// Проверяем валидность UUID
	_, err := uuid.Parse(supplierID)
	if err != nil {
		return fmt.Errorf("неправильный формат SupplierID: %w", err)
	}

	_, err = uuid.Parse(guidES)
	if err != nil {
		return fmt.Errorf("неправильный формат GUID_ES: %w", err)
	}

	// Используем INSERT ... ON CONFLICT (UPSERT) для обновления или вставки
	query := `
		INSERT INTO "SupplierItemMapping" (
			"SupplierID", "ItemCode", "GUID_ES", "MatchMethod", "MatchConfidence",
			"UseCount", "LastUsedAt", "CreatedAt", "UpdatedAt"
		) VALUES (
			CAST(@supplierID AS UUID), @itemCode, CAST(@guidES AS UUID),
			@matchMethod, @matchConfidence, 1,
			(NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc')
		)
		ON CONFLICT ("SupplierID", "ItemCode") DO UPDATE SET
			"GUID_ES" = EXCLUDED."GUID_ES",
			"MatchMethod" = EXCLUDED."MatchMethod",
			"MatchConfidence" = EXCLUDED."MatchConfidence",
			"UseCount" = "SupplierItemMapping"."UseCount" + 1,
			"LastUsedAt" = (NOW() AT TIME ZONE 'utc'),
			"UpdatedAt" = (NOW() AT TIME ZONE 'utc');
	`

	_, err = pm.database.ExecContext(ctx, query,
		sql.Named("supplierID", supplierID),
		sql.Named("itemCode", itemCode),
		sql.Named("guidES", guidES),
		sql.Named("matchMethod", matchMethod),
		sql.Named("matchConfidence", matchConfidence),
	)

	if err != nil {
		if pm.logger != nil {
			pm.logger.Error("Ошибка сохранения сопоставления в кеш: SupplierID=%s, ItemCode=%s, GUID_ES=%s, ошибка: %v",
				supplierID, itemCode, guidES, err)
		}
		return fmt.Errorf("ошибка сохранения сопоставления: %w", err)
	}

	if pm.logger != nil {
		pm.logger.Info("Сопоставление сохранено в кеш: SupplierID=%s, ItemCode=%s, GUID_ES=%s",
			supplierID, itemCode, guidES)
	}

	return nil
}

// markAsProcessed помечает InvoiceData как обработанную
func (pm *PriceMatcher) markAsProcessed(ctx context.Context, invoiceDataID string) error {
	query := `
		UPDATE InvoiceData 
		SET IsProcessed = 1, ProcessedAt = (NOW() AT TIME ZONE 'utc')
		WHERE InvoiceDataID = CAST(@invoiceDataID AS UUID)
	`

	_, err := pm.database.ExecContext(ctx, query, sql.Named("invoiceDataID", invoiceDataID))
	return err
}

// markAsProcessedBatch помечает несколько InvoiceData как обработанные (batch)
func (pm *PriceMatcher) markAsProcessedBatch(ctx context.Context, invoiceDataIDs []string) error {
	if len(invoiceDataIDs) == 0 {
		return nil
	}

	// Используем VALUES для batch update через JOIN
	// Разбиваем на батчи по 1000 для избежания проблем с большими списками
	batchSize := 1000
	for i := 0; i < len(invoiceDataIDs); i += batchSize {
		end := i + batchSize
		if end > len(invoiceDataIDs) {
			end = len(invoiceDataIDs)
		}
		batch := invoiceDataIDs[i:end]

		// Строим VALUES для batch
		values := make([]string, len(batch))
		for j, id := range batch {
			values[j] = fmt.Sprintf("CAST('%s' AS UUID)", id)
		}

		query := fmt.Sprintf(`
			UPDATE InvoiceData 
			SET IsProcessed = 1, ProcessedAt = (NOW() AT TIME ZONE 'utc')
			WHERE InvoiceDataID IN (%s)
		`, strings.Join(values, ","))

		_, err := pm.database.ExecContext(ctx, query)
		if err != nil {
			return fmt.Errorf("ошибка batch update для InvoiceDataIDs: %w", err)
		}
	}

	return nil
}

// saveSupplierPriceBatch выполняет batch insert для сопоставленных записей
func (pm *PriceMatcher) saveSupplierPriceBatch(ctx context.Context, batch []struct {
	invoiceData models.InvoiceData
	match       *MatchResult
}) error {
	if len(batch) == 0 {
		return nil
	}

	for _, item := range batch {
		if err := pm.saveSupplierPrice(ctx, item.invoiceData, item.match); err != nil {
			return fmt.Errorf("ошибка сохранения сопоставленной записи в батче: %w", err)
		}
	}

	return nil
}

// saveSupplierPriceWithoutMatchBatch выполняет batch insert для несопоставленных записей
func (pm *PriceMatcher) saveSupplierPriceWithoutMatchBatch(ctx context.Context, batch []models.InvoiceData, cachedPriceListID sql.NullString) error {
	if len(batch) == 0 {
		return nil
	}

	tx, err := pm.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ошибка начала транзакции: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO SupplierPrice 
		(SupplierPriceID, SupplierID, InvoiceImportID, InvoiceDataID, GUID_ES,
		 ItemCode, ItemName, SupplierItemName, Barcode, Price, Quantity,
		 InvoiceNumber, InvoiceDate, BatchNumber, ExpiryDate,
		 Manufacturer, Country,
		 MatchMethod, MatchConfidence, IsActive, PriceListID, CreatedAt, UpdatedAt)
		VALUES 
		(CAST(@supplierPriceID AS UUID), CAST(@supplierID AS UUID),
		 CAST(@invoiceImportID AS UUID), CAST(@invoiceDataID AS UUID),
		 NULL,
		 @itemCode, @itemName, @supplierItemName, @barcode, @price, @quantity,
		 @invoiceNumber, @invoiceDate, @batchNumber, @expiryDate,
		 @manufacturer, @country,
		 NULL, NULL, TRUE,
		 CASE WHEN @priceListID IS NOT NULL AND @priceListID != '' THEN CAST(@priceListID AS UUID) ELSE NULL END,
		 (NOW() AT TIME ZONE 'utc'), (NOW() AT TIME ZONE 'utc'))
	`

	stmt, err := db.PrepareRaw(ctx, tx, query)
	if err != nil {
		return fmt.Errorf("ошибка подготовки запроса: %w", err)
	}
	defer stmt.Close()

	for _, invoiceData := range batch {
		supplierPriceID := uuid.New().String()
		args := []interface{}{
			sql.Named("supplierPriceID", supplierPriceID),
			sql.Named("supplierID", invoiceData.SupplierID),
			sql.Named("invoiceImportID", invoiceData.InvoiceImportID),
			sql.Named("invoiceDataID", invoiceData.InvoiceDataID),
			sql.Named("itemCode", invoiceData.ItemCode),
			sql.Named("itemName", invoiceData.ItemName),
			sql.Named("supplierItemName", invoiceData.ItemName),
			sql.Named("barcode", invoiceData.Barcode),
			sql.Named("price", invoiceData.Price),
			sql.Named("quantity", invoiceData.Quantity),
			sql.Named("invoiceNumber", invoiceData.InvoiceNumber),
			sql.Named("invoiceDate", invoiceData.InvoiceDate),
			sql.Named("batchNumber", invoiceData.BatchNumber),
			sql.Named("expiryDate", invoiceData.ExpiryDate),
			sql.Named("manufacturer", invoiceData.Manufacturer),
			sql.Named("country", invoiceData.Country),
		}

		if cachedPriceListID.Valid && cachedPriceListID.String != "" {
			args = append(args, sql.Named("priceListID", cachedPriceListID.String))
		} else {
			args = append(args, sql.Named("priceListID", nil))
		}

		_, err := stmt.ExecContext(ctx, args...)
		if err != nil {
			return fmt.Errorf("ошибка вставки записи в батч: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ошибка коммита транзакции: %w", err)
	}

	return nil
}

// getStringPtr безопасно получает строку из указателя
func getStringPtr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
