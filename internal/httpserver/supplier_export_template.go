package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"es_api_service/internal/dbfimport"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// exportField — исходное поле накладной, которое ИИ сопоставляет с колонкой шаблона.
type exportField struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// exportFieldCatalog — что доступно в наших данных для выгрузки накладной.
func exportFieldCatalog() []exportField {
	return []exportField{
		{"global_sign", "Номер (EX)", "Глобальный номер заказа в формате EX-####### (добавляется всегда)"},
		{"order_date", "Дата", "Дата заказа/накладной"},
		{"supplier_code", "Код поставщика", "Числовой код поставщика (напр. 0001)"},
		{"supplier_name", "Поставщик", "Название поставщика"},
		{"buyer_name", "Покупатель", "Название аптеки/покупателя"},
		{"location_address", "Аптека (адрес)", "Адрес грузополучателя"},
		{"item_name", "Наименование", "Название товара/препарата"},
		{"item_code", "Код товара", "Код/артикул товара"},
		{"barcode", "Штрихкод", "Штрихкод EAN"},
		{"qty", "Количество", "Количество единиц"},
		{"unit_price", "Цена", "Цена за единицу"},
		{"sum", "Сумма", "Сумма по позиции (кол-во × цена)"},
		{"manufacturer", "Производитель", "Производитель товара"},
		{"series", "Серия", "Серия партии"},
		{"expiry", "Срок годности", "Срок годности"},
	}
}

type aiMapColumn struct {
	Name    string   `json:"name"`
	Samples []string `json:"samples"`
}
type aiMapField struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}
type aiMapRequest struct {
	Columns []aiMapColumn `json:"columns"`
	Fields  []aiMapField  `json:"fields"`
}
type aiMapResult struct {
	Mappings []struct {
		Column string `json:"column"`
		Field  string `json:"field"`
	} `json:"mappings"`
}

func aiServiceBase() string {
	if v := os.Getenv("ELF_AI_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://192.168.95.116:8808" // Mac mini (LAN)
}

// aiMapTemplate — просит ИИ сопоставить колонки шаблона с нашими полями.
func aiMapTemplate(columns []aiMapColumn, fields []aiMapField) (*aiMapResult, error) {
	body, _ := json.Marshal(aiMapRequest{Columns: columns, Fields: fields})
	req, err := http.NewRequest(http.MethodPost, aiServiceBase()+"/map-template", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ai map-template status %d", resp.StatusCode)
	}
	var out aiMapResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func templatesDir() string {
	dir := filepath.Join(".", "uploads", "templates")
	if exe, e := os.Executable(); e == nil {
		dir = filepath.Join(filepath.Dir(exe), "uploads", "templates")
	}
	return dir
}

// handleSCExportTemplate — POST /api/sc/export/template
// Загрузка шаблона накладной → чтение колонок → ИИ-маппинг → сохранение в конфиг.
func (s *Server) handleSCExportTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		s.writeError(w, http.StatusBadRequest, "Ошибка загрузки файла")
		return
	}
	file, handler, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Файл шаблона не найден")
		return
	}
	defer file.Close()
	if !dbfimport.IsSupportedDataFile(handler.Filename) {
		s.writeError(w, http.StatusBadRequest, "Поддерживаются .dbf, .xlsx, .xls, .xml, .sst")
		return
	}

	dir := templatesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка директории шаблонов")
		return
	}
	safe := strings.ReplaceAll(filepath.Base(handler.Filename), "..", "_")
	fname := fmt.Sprintf("%s_%s", sid, safe)
	fpath := filepath.Join(dir, fname)
	dst, err := os.Create(fpath)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка создания файла")
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		s.writeError(w, http.StatusInternalServerError, "Ошибка записи файла")
		return
	}
	dst.Close()

	headers, records, err := dbfimport.ReadTabularFile(fpath)
	if err != nil || len(headers) == 0 {
		s.writeError(w, http.StatusBadRequest, "Не удалось прочитать колонки шаблона")
		return
	}

	cols := make([]aiMapColumn, 0, len(headers))
	for _, h := range headers {
		samples := []string{}
		for ri := 0; ri < len(records) && ri < 3; ri++ {
			if v, ok := records[ri][h]; ok {
				if sv := strings.TrimSpace(fmt.Sprintf("%v", v)); sv != "" {
					samples = append(samples, sv)
				}
			}
		}
		cols = append(cols, aiMapColumn{Name: h, Samples: samples})
	}

	cat := exportFieldCatalog()
	fields := make([]aiMapField, 0, len(cat))
	for _, f := range cat {
		fields = append(fields, aiMapField{Key: f.Key, Description: f.Description})
	}

	aiRes, aiErr := aiMapTemplate(cols, fields)
	if aiErr != nil && s.logger != nil {
		s.logger.Warn("ИИ-маппинг шаблона недоступен для поставщика %s: %v", sid, aiErr)
	}

	mapping := make([]map[string]string, 0, len(headers))
	for _, h := range headers {
		field := "none"
		if aiRes != nil {
			for _, m := range aiRes.Mappings {
				if strings.EqualFold(strings.TrimSpace(m.Column), strings.TrimSpace(h)) {
					field = m.Field
					break
				}
			}
		}
		mapping = append(mapping, map[string]string{"column": h, "field": field})
	}
	mappingJSON, _ := json.Marshal(mapping)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.saveExportTemplate(ctx, sid, fname, string(mappingJSON)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка сохранения шаблона")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"template_file_name": fname,
		"columns":            headers,
		"fields":             cat,
		"mapping":            mapping,
		"ai_ok":              aiErr == nil,
	})
}

// handleSCExportMapping — PUT /api/sc/export/mapping
// Сохраняет поправленный вручную маппинг колонок.
func (s *Server) handleSCExportMapping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}
	var body struct {
		Mapping []map[string]string `json:"mapping"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Неверный формат запроса")
		return
	}
	mappingJSON, _ := json.Marshal(body.Mapping)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.database.GORMWith(ctx).
		Exec(`UPDATE "SupplierExportConfig" SET "ColumnMapping"=?, "UpdatedAt"=now() WHERE "SupplierID" = CAST(? AS UUID)`,
			string(mappingJSON), sid).Error; err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка сохранения маппинга")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"mapping": body.Mapping})
}

func (s *Server) saveExportTemplate(ctx context.Context, sid, fname, mappingJSON string) error {
	return s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		var cnt int64
		if e := tx.Raw(`SELECT COUNT(*) FROM "SupplierExportConfig" WHERE "SupplierID" = CAST(? AS UUID)`, sid).Scan(&cnt).Error; e != nil {
			return e
		}
		if cnt > 0 {
			return tx.Exec(`UPDATE "SupplierExportConfig" SET "TemplateFileName"=?, "ColumnMapping"=?, "UpdatedAt"=now() WHERE "SupplierID" = CAST(? AS UUID)`,
				fname, mappingJSON, sid).Error
		}
		return tx.Exec(`INSERT INTO "SupplierExportConfig"
			("SupplierExportConfigID","SupplierID","Method","Format","FtpPort","SmtpPort","IsActive","UpdatedAt","TemplateFileName","ColumnMapping")
			VALUES (CAST(? AS UUID), CAST(? AS UUID), 'none', 'dbf', 21, 587, true, now(), ?, ?)`,
			uuid.New().String(), sid, fname, mappingJSON).Error
	})
}
