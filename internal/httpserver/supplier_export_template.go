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

// exportFieldCatalog — все поля наших данных, доступные для выгрузки накладной/заказа.
// Описания содержат подсказки по типовым именам колонок/тегов (ORDERID, DOC_NUM,
// GOODS_NAME, EAN13, VAT_RATE и т.п.) — по ним ИИ точнее сопоставляет шаблон.
func exportFieldCatalog() []exportField {
	// ВАЖНО: global_sign (номер EX) НЕ включаем в список маппинга — колонка EX
	// добавляется в выгрузку автоматически всегда (см. BuildExport). Иначе поставщик
	// мог бы назначить её вручную и получить дубль.
	return []exportField{
		{"none", "— пусто/константа —", "Колонка не из данных (пустая или заполняется вручную/константой, напр. QTY_BOX=0, VAT_RATE=0.0000)"},

		// — шапка заказа —
		{"order_id", "ID заказа", "Уникальный ID заказа (GUID/UUID). Теги: ID_ORDER, ORDERID"},
		{"order_number", "Номер заказа", "Номер документа заказа, напр. Zkz-4546-A18. Теги: DOC_NUM, номер накладной"},
		{"order_date", "Дата заказа", "Дата заказа/накладной. Теги: DOC_DATE, PDATE, дата"},
		{"order_time", "Время заказа", "Время заказа. Теги: DOC_TIME, WTIME"},
		{"order_total", "Сумма заказа", "Общая сумма всего заказа. Теги: TOTAL_SUM, итого, AMOUNT"},
		{"comment", "Комментарий к заказу", "Общий комментарий к заказу. Теги: COMMENT"},
		{"payment_id", "Форма оплаты", "ID/код формы оплаты. Теги: PAYID"},
		{"row_count", "Кол-во позиций", "Число строк/позиций в заказе. Теги: ROWCOUNT"},
		{"addition", "Доплата/надбавка", "Доплата, надбавка или доп. поле. Теги: ADDITION"},

		// — прайс-лист (источник) —
		{"price_list_id", "ID прайс-листа", "ID прайс-листа (GUID). Теги: ID_PRICE_LIST"},
		{"price_list_item_id", "ID позиции прайса", "ID строки прайс-листа для позиции. Теги: ID_PRICE_LIST_ITEM"},
		{"price_list_date", "Дата прайс-листа", "Дата/время прайс-листа. Теги: DATE_PL"},
		{"price_list_name", "Название прайса", "Название прайс-листа ТЕКСТОМ (напр. «Прайс А1»), это НЕ id. Теги: PRICE если значение текст"},
		{"price_category", "Категория цены", "Категория/тип/колонка цены (напр. «Базовая»). Теги: CNAME"},

		// — клиент/покупатель —
		{"buyer_code", "Код клиента", "Код покупателя в системе. Теги: ID_CODE_C, CLIENTID"},
		{"delivery_code", "Код доставки", "Код доставки покупателя (для маршрута/логистики поставщика). Теги: DELIVERYCODE, DOSTAVKA, ROUTE"},
		{"buyer_name", "Покупатель", "Название аптеки/покупателя. Теги: CLIENT, CNAME"},
		{"buyer_inn", "ИНН клиента", "ИНН покупателя. Теги: CLINN, ИНН"},
		{"buyer_phone", "Телефон клиента", "Телефон покупателя. Теги: CPHONE"},
		{"location_address", "Адрес (грузополучатель)", "Адрес доставки/аптеки. Теги: CADDR, адрес"},
		{"address_id", "ID адреса", "ID адреса доставки клиента. Теги: ADDRID"},

		// — поставщик —
		{"supplier_code", "Код поставщика", "Числовой код поставщика (напр. 0001)"},
		{"supplier_name", "Поставщик", "Название поставщика. Теги: SNAME"},

		// — позиция заказа —
		{"order_item_id", "ID строки заказа", "Уникальный ID позиции заказа (GUID). Теги: ID_ORDER_ITEM, LINEID"},
		{"item_name", "Наименование", "Название товара/препарата. Теги: GOODS_NAME, GNAME, наименование"},
		{"item_code", "Код товара", "Код/артикул товара. Теги: CODE, CODEX, GOODS_CODE"},
		{"goods_guid", "ID товара/партии", "GUID товара или партии. Теги: GOODS_CODE (если GUID)"},
		{"barcode", "Штрихкод", "Штрихкод EAN-13. Теги: EAN13, штрихкод"},
		{"manufacturer", "Производитель", "Производитель. Теги: PRODUCER, PNAME"},
		{"country", "Страна", "Страна производства. Теги: CNTRY"},
		{"qty", "Количество", "Заказанное количество. Теги: QUANTITY, QTY, кол-во"},
		{"qty_box", "Кол-во в коробке", "Количество в коробке (часто 0). Теги: QTY_BOX"},
		{"qty_sheaf", "Кол-во в связке", "Количество в связке (часто 0). Теги: QUANTITY_SHEAF"},
		{"unit", "Единица", "Единица измерения. Теги: UNIT"},
		{"unit_price", "Цена", "Цена за единицу — ЧИСЛО с копейками. Теги: COST, PRICE (если число), цена"},
		{"discount_price", "Цена со скидкой", "Цена со скидкой/с доставкой — число. Теги: DCOST"},
		{"purchase_price", "Закупочная цена", "Закупочная/базовая цена — число. Теги: PCOST, OCOST"},
		{"alt_price", "Альт. цена", "Альтернативная цена (часто 0). Теги: ALT_PRICE"},
		{"vat_rate", "НДС %", "Ставка НДС (часто 0.0000). Теги: VAT_RATE, NDS, НДС"},
		{"sum", "Сумма позиции", "Сумма по позиции (кол-во × цена)"},
		{"series", "Серия", "Серия партии. Теги: SERIES, серия"},
		{"expiry", "Срок годности", "Срок годности. Теги: PERIOD, срок"},
		{"registry_price", "Реестровая цена", "Реестровая цена (ЖНВЛС). Теги: REGCOST"},
		{"ratio", "Кратность", "Кратность отпуска/фасовки. Теги: RATIO"},
		{"volume", "Объём/фасовка", "Объём, вес или фасовка. Теги: VOLUME"},
		{"okp_code", "Код ОКП", "Код ОКП/ОКПД товара. Теги: CODEOKP"},
		{"item_note", "Примечание к позиции", "Заметка по позиции (напр. «МАРКИРОВАН»). Теги: NOTE"},
		{"client_line_id", "ID строки у клиента", "ID строки заказа в системе клиента. Теги: CLLINEID"},
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

	// Собираем до 8 РАЗНЫХ непустых значений на колонку, просматривая до 60 строк,
	// — чтобы ИИ видел реальное содержимое (GUID'ы, даты, цены, штрихкоды), а не
	// первые пустые/одинаковые ячейки, и точнее понимал, что это за поле.
	const (
		maxScanRows   = 60
		maxSamplesCol = 8
	)
	cols := make([]aiMapColumn, 0, len(headers))
	for _, h := range headers {
		samples := make([]string, 0, maxSamplesCol)
		seen := make(map[string]bool)
		for ri := 0; ri < len(records) && ri < maxScanRows && len(samples) < maxSamplesCol; ri++ {
			v, ok := records[ri][h]
			if !ok {
				continue
			}
			sv := strings.TrimSpace(fmt.Sprintf("%v", v))
			if sv == "" || seen[sv] {
				continue
			}
			seen[sv] = true
			samples = append(samples, sv)
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
