package httpserver

import (
	"encoding/json"
	"es_api_service/internal/dbfimport"
	"fmt"
	"net/http"
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

// handleAnalyzeDBF анализирует файл прайса (DBF/Excel) и возвращает список полей
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
		s.logger.Info("Анализ файла прайса: %s", req.FilePath)
	}

	headers, records, err := dbfimport.ReadTabularFile(req.FilePath)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось открыть файл данных: %v", err))
		return
	}

	rawFields := dbfimport.BuildFieldInfos(headers, records)
	fields := make([]DBFFieldInfo, 0, len(rawFields))
	for _, rawField := range rawFields {
		fields = append(fields, DBFFieldInfo{
			Name:     rawField["name"].(string),
			Type:     rawField["type"].(string),
			Length:   rawField["length"].(int),
			Decimals: rawField["decimals"].(int),
		})
	}

	result := map[string]interface{}{
		"file_path":     req.FilePath,
		"records_count": len(records),
		"fields":        fields,
		"field_count":   len(fields),
		"file_format":   dbfimport.DetectDataFileFormat(req.FilePath),
	}

	if s.logger != nil {
		s.logger.Info("Файл прайса проанализирован: %d полей, %d записей", len(fields), len(records))
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
