package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"es_api_service/internal/matching"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// handleSearchDrugs ищет препараты в справочнике es_ef2
func (s *Server) handleSearchDrugs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	queryParam := r.URL.Query().Get("q")
	if queryParam == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан поисковый запрос")
		return
	}

	// Очищаем запрос от SQL инъекций
	queryParam = strings.TrimSpace(queryParam)
	searchPattern := "%" + strings.ReplaceAll(queryParam, "%", "[%]") + "%"
	
	// Разбиваем поисковый запрос на слова для многословного поиска
	words := strings.Fields(queryParam)
	
	// Собираем условия для поиска
	// Каждое слово должно быть найдено хотя бы в одном поле
	var whereConditions []string
	var args []interface{}
	
	// Для каждого слова создаем условие поиска по всем полям
	for i, word := range words {
		wordPattern := "%" + strings.ReplaceAll(word, "%", "[%]") + "%"
		paramName := fmt.Sprintf("pattern%d", i)
		
		// Поиск по каждому слову в любом из полей
		wordCondition := fmt.Sprintf(`(
			ef2.NAME LIKE @%s 
			OR ef2.TRN_NAME_RUS LIKE @%s
			OR ef2.INN_NAME_RUS LIKE @%s
			OR ef2.BARCODE LIKE @%s
			OR CAST(ef2.KOD_ES AS NVARCHAR(50)) LIKE @%s
			OR CAST(ef2.ID_ES AS NVARCHAR(50)) LIKE @%s
			OR ep.PRODUCER_NAME LIKE @%s
		)`, paramName, paramName, paramName, paramName, paramName, paramName, paramName)
		
		whereConditions = append(whereConditions, wordCondition)
		args = append(args, sql.Named(paramName, wordPattern))
	}
	
	// Для одного слова >= 8 символов добавляем точное совпадение штрихкода как альтернативу
	if len(words) == 1 && len(words[0]) >= 8 {
		whereConditions[0] = fmt.Sprintf("(%s OR ef2.BARCODE = @exactBarcode)", whereConditions[0])
		args = append(args, sql.Named("exactBarcode", words[0]))
	}
	
	args = append(args, sql.Named("mainPattern", searchPattern))

	searchQuery := fmt.Sprintf(`
		SELECT TOP 100
			CAST(ef2.GUID_ES AS NVARCHAR(50)) AS GUID_ES,
			ef2.NAME,
			ef2.INN_NAME_RUS,
			ef2.CUREFORM_NAME,
			ef2.BARCODE,
			ef2.TRN_NAME_RUS,
			ep.PRODUCER_NAME,
			ef2.REESTR_PRICE,
			ef2.KOD_ES,
			ef2.ID_ES
		FROM es_ef2 ef2
		LEFT JOIN es_producer ep ON ef2.PRODUCER_COD = ep.KOD_PRODUCER
		WHERE %s
		  AND ef2.is_active = 1
		  AND ef2.DELETED IS NULL
		ORDER BY 
			CASE WHEN ef2.BARCODE = @exactBarcodeCheck THEN 1 ELSE 2 END,
			CASE WHEN ef2.NAME LIKE @mainPattern THEN 1 
			     WHEN ef2.NAME LIKE @exactPattern THEN 2
			     ELSE 3 END,
			ef2.NAME
	`, strings.Join(whereConditions, " AND "))
	
	// Добавляем параметры для сортировки
	exactPattern := queryParam + "%"
	args = append(args, 
		sql.Named("exactBarcodeCheck", queryParam),
		sql.Named("exactPattern", exactPattern),
	)
	
	rows, err := s.database.GORMWith(ctx).Raw(searchQuery, args...).Rows()
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка поиска препаратов: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка поиска: %v", err))
		return
	}
	defer rows.Close()

	type Drug struct {
		GUID_ES     string   `json:"guid_es"`
		Name        string   `json:"name"`
		INN         *string  `json:"inn,omitempty"`
		CureForm    *string  `json:"cure_form,omitempty"`
		Barcode     *string  `json:"barcode,omitempty"`
		TradeName   *string  `json:"trade_name,omitempty"`
		ProducerName *string `json:"producer_name,omitempty"`
		RegistryPrice *float64 `json:"registry_price,omitempty"`
		ES_Code     *int64   `json:"es_code,omitempty"`
		ID_ES       *int64   `json:"id_es,omitempty"`
	}

	var drugs []Drug
	for rows.Next() {
		var drug Drug
		var inn, cureForm, barcode, tradeName, producerName sql.NullString
		var registryPrice sql.NullFloat64
		var esCode, idES sql.NullInt64

		err := rows.Scan(
			&drug.GUID_ES,
			&drug.Name,
			&inn,
			&cureForm,
			&barcode,
			&tradeName,
			&producerName,
			&registryPrice,
			&esCode,
			&idES,
		)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Ошибка сканирования препарата: %v", err)
			}
			continue
		}

		if inn.Valid {
			drug.INN = &inn.String
		}
		if cureForm.Valid {
			drug.CureForm = &cureForm.String
		}
		if barcode.Valid {
			drug.Barcode = &barcode.String
		}
		if tradeName.Valid {
			drug.TradeName = &tradeName.String
		}
		if producerName.Valid {
			drug.ProducerName = &producerName.String
		}
		if registryPrice.Valid {
			drug.RegistryPrice = &registryPrice.Float64
		}
		if esCode.Valid {
			drug.ES_Code = &esCode.Int64
		}
		if idES.Valid {
			drug.ID_ES = &idES.Int64
		}

		drugs = append(drugs, drug)
	}

	if drugs == nil {
		drugs = []Drug{}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"query": queryParam,
		"total": len(drugs),
		"drugs": drugs,
	})
}

// handleUpdateSupplierPriceMatch обновляет сопоставление прайса поставщика
func (s *Server) handleUpdateSupplierPriceMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	// Извлекаем ID из пути /api/supplier-prices/:id/match
	// Сначала пробуем получить ID из контекста (если он был установлен в роутере)
	var priceID string
	if ctxID := r.Context().Value("priceID"); ctxID != nil {
		if idStr, ok := ctxID.(string); ok && idStr != "" {
			priceID = idStr
			if s.logger != nil {
				s.logger.Debug("ID прайса получен из контекста: '%s'", priceID)
			}
		}
	}
	
	// Если ID не найден в контексте, извлекаем из пути
	if priceID == "" {
		path := strings.Trim(r.URL.Path, "/")
		pathParts := strings.Split(path, "/")
		
		if s.logger != nil {
			s.logger.Debug("Обработка пути для сопоставления: %s, части: %v, количество частей: %d", r.URL.Path, pathParts, len(pathParts))
		}
		
		// Ищем supplier-prices в пути
		for i, part := range pathParts {
			if part == "supplier-prices" && i+1 < len(pathParts) {
				// Следующий элемент должен быть ID
				candidateID := pathParts[i+1]
				
				if s.logger != nil {
					s.logger.Debug("Найден кандидат ID: '%s' (индекс: %d, длина: %d, hex: %x)", 
						candidateID, i+1, len(candidateID), []byte(candidateID))
				}
				
				// Проверяем, что это не "summary", "create", "match" или другой служебный путь
				if candidateID != "summary" && candidateID != "create" && candidateID != "" && candidateID != "match" {
					// Очищаем ID от возможных лишних символов
					candidateID = strings.TrimSpace(candidateID)
					
					// Убираем возможные управляющие символы и невидимые символы
					candidateID = strings.TrimFunc(candidateID, func(r rune) bool {
						return r < 32 || r == 127 || r == 0
					})
					
					// Декодируем URL-кодированные символы
					decodedID, err := url.QueryUnescape(candidateID)
					if err != nil {
						// Если декодирование не удалось, пробуем использовать исходное значение
						if s.logger != nil {
							s.logger.Warn("Ошибка декодирования ID прайса: %v, используем исходное значение: %s (hex: %x)", 
								err, candidateID, []byte(candidateID))
						}
						priceID = candidateID
					} else {
						priceID = strings.TrimSpace(decodedID)
						if s.logger != nil && decodedID != candidateID {
							s.logger.Debug("ID прайса декодирован: исходный='%s', декодированный='%s'", candidateID, decodedID)
						}
					}
					
					// Убираем возможные невидимые символы после декодирования
					priceID = strings.Map(func(r rune) rune {
						if r < 32 && r != '\t' && r != '\n' && r != '\r' {
							return -1 // Удаляем управляющие символы
						}
						return r
					}, priceID)
					
					break
				} else {
					if s.logger != nil {
						s.logger.Debug("Пропущен служебный путь: '%s'", candidateID)
					}
				}
			}
		}
	}
	
	if s.logger != nil && priceID != "" {
		s.logger.Debug("Извлеченный ID прайса: '%s' (длина: %d, hex: %x, байты: %v)", 
			priceID, len(priceID), []byte(priceID), []byte(priceID))
	}

	if priceID == "" {
		// Если ID не найден, логируем путь для отладки
		if s.logger != nil {
			path := strings.Trim(r.URL.Path, "/")
			pathParts := strings.Split(path, "/")
			s.logger.Warn("Не удалось извлечь ID прайса из пути: %s, части пути: %v, количество частей: %d", 
				r.URL.Path, pathParts, len(pathParts))
		}
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}

	// Валидация UUID - проверяем формат перед парсингом
	// UUID должен быть в формате: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 символов)
	uuidPattern := `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	if matched, _ := regexp.MatchString(uuidPattern, strings.ToLower(priceID)); !matched {
		if s.logger != nil {
			s.logger.Warn("ID прайса не соответствует формату UUID: '%s' (длина: %d, hex: %x), путь: %s", 
				priceID, len(priceID), []byte(priceID), r.URL.Path)
		}
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый формат ID прайса: %s (ожидается UUID)", priceID))
		return
	}
	
	// Проверяем валидность UUID через парсер
	if _, err := uuid.Parse(priceID); err != nil {
		if s.logger != nil {
			s.logger.Warn("Ошибка парсинга UUID: '%s' (ошибка: %v), путь: %s", priceID, err, r.URL.Path)
		}
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый формат ID прайса: %s", priceID))
		return
	}

	var req struct {
		GUID_ES         *string  `json:"guid_es,omitempty"`
		MatchMethod     *string  `json:"match_method,omitempty"`
		MatchConfidence *float64 `json:"match_confidence,omitempty"`
		IsConfirmed     *bool    `json:"is_confirmed,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	// Логируем полученные данные для отладки
	if s.logger != nil {
		s.logger.Debug("Получен запрос на обновление сопоставления: PriceID=%s, GUID_ES=%v, MatchMethod=%v, MatchConfidence=%v",
			priceID, req.GUID_ES, req.MatchMethod, req.MatchConfidence)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Формируем динамический UPDATE запрос
	var updates []string
	var args []interface{}
	args = append(args, sql.Named("priceID", priceID))

	if req.GUID_ES != nil {
		if *req.GUID_ES == "" {
			updates = append(updates, "GUID_ES = NULL")
		} else {
			// Валидация UUID для GUID_ES
			if _, err := uuid.Parse(*req.GUID_ES); err != nil {
				s.writeError(w, http.StatusBadRequest, "Недопустимый формат GUID_ES")
				return
			}
			updates = append(updates, "GUID_ES = CAST(@guidES AS UNIQUEIDENTIFIER)")
			args = append(args, sql.Named("guidES", *req.GUID_ES))
		}
	}

	if req.MatchMethod != nil {
		// Валидация метода сопоставления
		validMethods := map[string]bool{
			"BARCODE": true,
			"CODE":    true,
			"NAME":    true,
			"MANUAL":  true,
		}
		if !validMethods[*req.MatchMethod] {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Недопустимый метод сопоставления: %s", *req.MatchMethod))
			return
		}
		updates = append(updates, "MatchMethod = @matchMethod")
		args = append(args, sql.Named("matchMethod", *req.MatchMethod))
	}

	if req.MatchConfidence != nil {
		if *req.MatchConfidence < 0 || *req.MatchConfidence > 100 {
			s.writeError(w, http.StatusBadRequest, "MatchConfidence должен быть в диапазоне 0-100")
			return
		}
		updates = append(updates, "MatchConfidence = @matchConfidence")
		args = append(args, sql.Named("matchConfidence", *req.MatchConfidence))
	}

	if len(updates) == 0 {
		if s.logger != nil {
			s.logger.Warn("Не указаны поля для обновления: PriceID=%s, GUID_ES=%v, MatchMethod=%v, MatchConfidence=%v",
				priceID, req.GUID_ES, req.MatchMethod, req.MatchConfidence)
		}
		s.writeError(w, http.StatusBadRequest, "Не указаны поля для обновления. Укажите хотя бы одно из полей: guid_es, match_method, match_confidence")
		return
	}
	
	if s.logger != nil {
		s.logger.Debug("Будут обновлены поля: %v", updates)
	}

	updates = append(updates, "UpdatedAt = GETUTCDATE()")

	updateQuery := fmt.Sprintf(`
		UPDATE SupplierPrice
		SET %s
		WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)
	`, strings.Join(updates, ", "))

	// Сначала получаем ItemCode и SupplierID из SupplierPrice для сохранения в кеш
	var supplierID, itemCode sql.NullString
	var guidES sql.NullString
	getQuery := `
		SELECT CAST(SupplierID AS NVARCHAR(50)) AS SupplierID,
		       ItemCode,
		       CASE WHEN GUID_ES IS NULL THEN NULL ELSE CAST(GUID_ES AS NVARCHAR(50)) END AS GUID_ES
		FROM SupplierPrice
		WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)
	`
	err := s.database.GORMWith(ctx).Raw(getQuery, sql.Named("priceID", priceID)).Row().Scan(&supplierID, &itemCode, &guidES)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка получения данных прайса для кеширования: %v", err)
		}
		// Продолжаем выполнение, даже если не удалось получить данные для кеша
	}

	res := s.database.GORMWith(ctx).Exec(updateQuery, args...)
	if res.Error != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка обновления сопоставления: %v", res.Error)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления: %v", res.Error))
		return
	}

	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Прайс не найден")
		return
	}

	// Сохраняем сопоставление в кеш, если обновление было успешным и есть GUID_ES
	if supplierID.Valid && itemCode.Valid && itemCode.String != "" {
		// Получаем обновленные данные после UPDATE
		var finalGUIDES sql.NullString
		var finalMatchMethod sql.NullString
		var finalMatchConfidence sql.NullFloat64
		
		getUpdatedQuery := `
			SELECT CASE WHEN GUID_ES IS NULL THEN NULL ELSE CAST(GUID_ES AS NVARCHAR(50)) END AS GUID_ES,
			       MatchMethod,
			       MatchConfidence
			FROM SupplierPrice
			WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)
		`
		err = s.database.GORMWith(ctx).Raw(getUpdatedQuery, sql.Named("priceID", priceID)).Row().Scan(
			&finalGUIDES, &finalMatchMethod, &finalMatchConfidence)
		
		if err == nil && finalGUIDES.Valid && finalGUIDES.String != "" {
			// Сохраняем в кеш
			matchMethod := "MANUAL"
			if finalMatchMethod.Valid && finalMatchMethod.String != "" {
				matchMethod = finalMatchMethod.String
			}
			matchConfidence := 100.0
			if finalMatchConfidence.Valid {
				matchConfidence = finalMatchConfidence.Float64
			}
			
			// Используем PriceMatcher для сохранения в кеш
			priceMatcher := matching.NewPriceMatcher(s.database, s.logger)
			err = priceMatcher.SaveMappingForItem(ctx, supplierID.String, itemCode.String, 
				finalGUIDES.String, matchMethod, matchConfidence)
			if err != nil && s.logger != nil {
				s.logger.Warn("Ошибка сохранения сопоставления в кеш: %v", err)
			} else if s.logger != nil {
				s.logger.Info("Сопоставление сохранено в кеш: SupplierID=%s, ItemCode=%s, GUID_ES=%s", 
					supplierID.String, itemCode.String, finalGUIDES.String)
			}
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "success",
		"message": "Сопоставление обновлено",
		"supplier_price_id": priceID,
	})
}

