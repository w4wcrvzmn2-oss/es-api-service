package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/LindsayBradford/go-dbf/godbf"
)

// DBFFieldInfo представляет информацию о поле DBF
type DBFFieldInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Length   int    `json:"length"`
	Decimals int    `json:"decimals"`
}

// AnalyzeDBFRequest представляет запрос на анализ DBF файла
type AnalyzeDBFRequest struct {
	FilePath string `json:"file_path"`
}

// handleAnalyzeDBF анализирует DBF файл и возвращает список полей
func (s *Server) handleAnalyzeDBF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	var req AnalyzeDBFRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	if req.FilePath == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан путь к файлу")
		return
	}

	if s.logger != nil {
		s.logger.Info("Анализ DBF файла: %s", req.FilePath)
	}

	// Открываем DBF файл
	dbfTable, err := godbf.NewFromFile(req.FilePath, "CP866") // Кодировка для русских символов
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось открыть DBF файл: %v", err))
		return
	}

	// Получаем информацию о полях
	fieldNames := dbfTable.FieldNames()
	fields := make([]DBFFieldInfo, 0, len(fieldNames))

	for _, fieldName := range fieldNames {
		// Пытаемся получить значение первого поля для определения типа
		// Для простоты используем базовую информацию
		// В библиотеке go-dbf нет прямого метода получения информации о типе поля
		// Используем попытку чтения для определения типа
		fieldType := "C" // Character по умолчанию
		fieldLength := 0
		decimals := 0

		// Пытаемся прочитать значение для определения типа
		if dbfTable.NumberOfRecords() > 0 {
			value, err := dbfTable.FieldValueByName(0, fieldName)
			if err == nil && value != "" {
				// Определяем тип по значению
				// FieldValueByName возвращает string, пытаемся определить тип
				fieldType = "C" // Character по умолчанию
				fieldLength = len(value)
				
				// Пытаемся распознать число
				// Если строка состоит только из цифр и точек/запятых - это число
				hasOnlyDigits := true
				hasDecimal := false
				for _, r := range value {
					if r >= '0' && r <= '9' {
						continue
					} else if r == '.' || r == ',' {
						hasDecimal = true
						continue
					} else if r == '-' || r == '+' {
						continue
					} else {
						hasOnlyDigits = false
						break
					}
				}
				
				if hasOnlyDigits && fieldLength > 0 {
					fieldType = "N" // Numeric
					if hasDecimal {
						decimals = 2
					}
				}
			}
		}

		fields = append(fields, DBFFieldInfo{
			Name:     fieldName,
			Type:     fieldType,
			Length:   fieldLength,
			Decimals: decimals,
		})
	}

	result := map[string]interface{}{
		"file_path":      req.FilePath,
		"records_count":  dbfTable.NumberOfRecords(),
		"fields":         fields,
		"field_count":    len(fields),
	}

	if s.logger != nil {
		s.logger.Info("DBF файл проанализирован: %d полей, %d записей", len(fields), dbfTable.NumberOfRecords())
	}

	s.writeJSON(w, http.StatusOK, result)
}

// GetTargetFields возвращает список целевых полей для маппинга
func (s *Server) handleGetTargetFields(w http.ResponseWriter, r *http.Request) {

	// Целевые поля для InvoiceData
	targetFields := []map[string]string{
		{"name": "invoice_number", "type": "NVARCHAR", "description": "Номер прайса"},
		{"name": "invoice_date", "type": "DATETIME", "description": "Дата прайса"},
		{"name": "item_code", "type": "NVARCHAR", "description": "Код товара от поставщика"},
		{"name": "item_name", "type": "NVARCHAR", "description": "Наименование товара"},
		{"name": "quantity", "type": "DECIMAL", "description": "Количество"},
		{"name": "price", "type": "DECIMAL", "description": "Цена"},
		{"name": "batch_number", "type": "NVARCHAR", "description": "Номер партии"},
		{"name": "expiry_date", "type": "DATETIME", "description": "Срок годности"},
		{"name": "manufacturer", "type": "NVARCHAR", "description": "Производитель"},
		{"name": "country", "type": "NVARCHAR", "description": "Страна"},
		{"name": "barcode", "type": "NVARCHAR", "description": "Штрихкод"},
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"target_fields": targetFields,
	})
}

