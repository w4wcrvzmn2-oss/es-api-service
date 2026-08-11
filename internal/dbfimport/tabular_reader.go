package dbfimport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LindsayBradford/go-dbf/godbf"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

var dbfExpectedRe = regexp.MustCompile(`header expected (\d+)`)

// openDBFTolerant открывает DBF (CP866). Если файл на несколько байт короче, чем
// ждёт заголовок (частая беда выгрузок — обрезан завершающий EOF-байт), дописывает
// недостающие байты во временную копию и повторяет. Иначе строгая go-dbf падает.
func openDBFTolerant(filePath string) (*godbf.DbfTable, error) {
	t, err := godbf.NewFromFile(filePath, "CP866")
	if err == nil {
		return t, nil
	}
	m := dbfExpectedRe.FindStringSubmatch(err.Error())
	if m == nil {
		return nil, err
	}
	want, _ := strconv.Atoi(m[1])
	raw, e := os.ReadFile(filePath)
	if e != nil {
		return nil, err
	}
	// Дописываем только если не хватает немного (иначе файл реально битый).
	if want <= len(raw) || want-len(raw) > 64 {
		return nil, err
	}
	padded := make([]byte, want)
	copy(padded, raw)
	for i := len(raw); i < want; i++ {
		padded[i] = 0x20
	}
	tmp := filePath + ".padded"
	if e := os.WriteFile(tmp, padded, 0644); e != nil {
		return nil, err
	}
	defer os.Remove(tmp)
	return godbf.NewFromFile(tmp, "CP866")
}

var supportedDataFileExts = []string{".dbf", ".xlsx", ".xlsm", ".xls", ".xml", ".sst"}

// DetectDataFileFormat возвращает поддерживаемый формат файла данных.
func DetectDataFileFormat(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".dbf":
		return "dbf"
	case ".xlsx", ".xlsm":
		return "excel"
	case ".xls":
		return "xls"
	case ".xml":
		return "xml"
	case ".sst":
		return "sst"
	default:
		return ""
	}
}

// IsSupportedDataFile сообщает, можно ли обработать файл как прайс.
func IsSupportedDataFile(filePath string) bool {
	return DetectDataFileFormat(filePath) != ""
}

// SupportedDataFileExtensions возвращает список поддерживаемых расширений.
func SupportedDataFileExtensions() []string {
	out := make([]string, len(supportedDataFileExts))
	copy(out, supportedDataFileExts)
	return out
}

// ReadTabularFile читает DBF или Excel и возвращает заголовки и записи.
func ReadTabularFile(filePath string) ([]string, []map[string]interface{}, error) {
	switch DetectDataFileFormat(filePath) {
	case "dbf":
		return readDBFRecords(filePath)
	case "excel":
		return readExcelRecords(filePath)
	case "xls":
		return readXLSRecords(filePath)
	case "xml":
		return readXMLRecords(filePath)
	case "sst":
		return readSSTRecords(filePath)
	default:
		return nil, nil, fmt.Errorf("неподдерживаемый формат файла: %s", filepath.Ext(filePath))
	}
}

func readDBFRecords(filePath string) (fieldNames []string, records []map[string]interface{}, err error) {
	// Нативный толерантный dBase III читатель. go-dbf слишком строг — падает/паникует
	// на реальных файлах (обрезанный EOF-байт, нестандартный «footer», мемо-поля).
	defer func() {
		if rec := recover(); rec != nil {
			if len(fieldNames) > 0 {
				err = nil
			} else {
				fieldNames, records = nil, nil
				err = fmt.Errorf("не удалось разобрать DBF: %v", rec)
			}
		}
	}()
	return readDBFNative(filePath)
}

// readDBFNative разбирает dBase III DBF вручную (CP866), терпимо к обрезанному
// хвосту/EOF: читает столько записей, сколько реально есть в файле.
func readDBFNative(filePath string) ([]string, []map[string]interface{}, error) {
	f, e := os.Open(filePath)
	if e != nil {
		return nil, nil, fmt.Errorf("не удалось открыть DBF файл: %w", e)
	}
	defer f.Close()

	hdr := make([]byte, 32)
	if _, e := io.ReadFull(f, hdr); e != nil {
		return nil, nil, fmt.Errorf("DBF: не прочитан заголовок: %w", e)
	}
	numRecords := int(binary.LittleEndian.Uint32(hdr[4:8]))
	headerLen := int(binary.LittleEndian.Uint16(hdr[8:10]))
	recLen := int(binary.LittleEndian.Uint16(hdr[10:12]))
	if headerLen < 33 || recLen < 1 || headerLen > 1<<20 {
		return nil, nil, fmt.Errorf("DBF: битый заголовок (headerLen=%d recLen=%d)", headerLen, recLen)
	}

	fd := make([]byte, headerLen-32)
	if _, e := io.ReadFull(f, fd); e != nil {
		return nil, nil, fmt.Errorf("DBF: не прочитаны описания полей: %w", e)
	}
	type fld struct {
		name   string
		off    int
		length int
	}
	var fields []fld
	pos := 1 // байт 0 записи — флаг удаления
	seen := map[string]int{}
	for off := 0; off+32 <= len(fd); off += 32 {
		if fd[off] == 0x0D { // терминатор описаний полей
			break
		}
		base := strings.TrimRight(string(fd[off:off+11]), "\x00 ")
		name := base
		if name == "" {
			name = fmt.Sprintf("F%d", len(fields)+1)
		}
		if n := seen[base]; n > 0 {
			name = fmt.Sprintf("%s_%d", name, n+1)
		}
		seen[base]++
		length := int(fd[off+16])
		fields = append(fields, fld{name: name, off: pos, length: length})
		pos += length
	}
	if len(fields) == 0 {
		return nil, nil, fmt.Errorf("DBF: не найдено ни одного поля")
	}

	names := make([]string, len(fields))
	for i, fl := range fields {
		names[i] = fl.name
	}

	dec := charmap.CodePage866.NewDecoder()
	records := make([]map[string]interface{}, 0, numRecords)
	rec := make([]byte, recLen)
	for i := 0; numRecords <= 0 || i < numRecords; i++ {
		n, e := io.ReadFull(f, rec)
		if e != nil || n < recLen {
			break // хвост обрезан — берём что успели прочитать
		}
		if rec[0] == 0x2A { // удалённая запись
			continue
		}
		m := make(map[string]interface{}, len(fields))
		for _, fl := range fields {
			if fl.off+fl.length > len(rec) {
				break
			}
			raw := bytes.TrimRight(rec[fl.off:fl.off+fl.length], " \x00")
			val, _, de := transform.Bytes(dec, raw)
			if de != nil {
				val = raw
			}
			m[fl.name] = strings.TrimSpace(string(val))
		}
		records = append(records, m)
	}
	return names, records, nil
}

func readExcelRecords(filePath string) ([]string, []map[string]interface{}, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть Excel файл: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, fmt.Errorf("в Excel файле нет листов")
	}

	// Берём первый лист, где есть данные (частая беда — пустой первый лист).
	var sheetName string
	var rows [][]string
	for _, name := range sheets {
		r, e := f.GetRows(name)
		if e != nil {
			continue
		}
		if countNonEmptyRows(r) > 0 {
			sheetName = name
			rows = r
			break
		}
	}
	if sheetName == "" {
		return nil, nil, fmt.Errorf("Excel файл не содержит данных ни на одном листе")
	}

	return buildRecordsFromRows(rows)
}

// buildRecordsFromRows превращает матрицу строк (xlsx/xls) в заголовки и записи:
// определяет строку заголовков и собирает значения под ними.
func buildRecordsFromRows(rows [][]string) ([]string, []map[string]interface{}, error) {
	headerRowIdx, headers := detectExcelHeaderRow(rows)
	if headerRowIdx == -1 || len(headers) == 0 {
		return nil, nil, fmt.Errorf("не удалось определить строку заголовков")
	}

	records := make([]map[string]interface{}, 0, len(rows)-headerRowIdx-1)
	for i := headerRowIdx + 1; i < len(rows); i++ {
		row := rows[i]
		record := make(map[string]interface{}, len(headers))
		hasValue := false
		for colIdx, header := range headers {
			value := ""
			if colIdx < len(row) {
				value = strings.TrimSpace(row[colIdx])
			}
			if value != "" {
				hasValue = true
			}
			record[header] = value
		}
		if hasValue {
			records = append(records, record)
		}
	}

	return headers, records, nil
}

func countNonEmptyCells(row []string) int {
	n := 0
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			n++
		}
	}
	return n
}

func countNonEmptyRows(rows [][]string) int {
	n := 0
	for _, r := range rows {
		if countNonEmptyCells(r) > 0 {
			n++
		}
	}
	return n
}

// detectExcelHeaderRow выбирает строку заголовков как первую строку с наибольшим
// числом заполненных ячеек среди первых строк листа. Это отсекает «шапки»-титулы
// вроде «Прайс-лист ООО …» в одной ячейке над реальной таблицей.
func detectExcelHeaderRow(rows [][]string) (int, []string) {
	const scanLimit = 25
	limit := len(rows)
	if limit > scanLimit {
		limit = scanLimit
	}

	bestIdx := -1
	bestCount := 0
	for i := 0; i < limit; i++ {
		c := countNonEmptyCells(rows[i])
		// Заголовок обычно шире одной ячейки; строго больший счёт выигрывает,
		// при равенстве остаётся более ранняя строка.
		if c >= 2 && c > bestCount {
			bestCount = c
			bestIdx = i
		}
	}

	// Фолбэк: если «широкой» строки нет — первая непустая строка.
	if bestIdx == -1 {
		for i, row := range rows {
			if countNonEmptyCells(row) > 0 {
				bestIdx = i
				break
			}
		}
	}
	if bestIdx == -1 {
		return -1, nil
	}

	headers := normalizeExcelHeaders(rows[bestIdx])
	return bestIdx, headers
}

func normalizeExcelHeaders(row []string) []string {
	headers := make([]string, 0, len(row))
	seen := make(map[string]int)
	for idx, cell := range row {
		name := strings.TrimSpace(cell)
		if name == "" {
			name = fmt.Sprintf("COLUMN_%d", idx+1)
		}
		if n := seen[name]; n > 0 {
			name = fmt.Sprintf("%s_%d", name, n+1)
		}
		seen[strings.TrimSpace(cell)]++
		headers = append(headers, name)
	}

	hasNonEmpty := false
	for _, h := range row {
		if strings.TrimSpace(h) != "" {
			hasNonEmpty = true
			break
		}
	}
	if !hasNonEmpty {
		return nil
	}
	return headers
}

// BuildFieldInfos подготавливает описание колонок для analyze.
func BuildFieldInfos(headers []string, records []map[string]interface{}) []map[string]interface{} {
	fields := make([]map[string]interface{}, 0, len(headers))
	for _, header := range headers {
		fieldType := "C"
		fieldLength := 0
		decimals := 0

		for _, record := range records {
			raw, ok := record[header]
			if !ok {
				continue
			}
			value := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if value == "" {
				continue
			}
			fieldLength = len(value)
			fieldType, decimals = inferFieldType(value)
			break
		}

		fields = append(fields, map[string]interface{}{
			"name":     header,
			"type":     fieldType,
			"length":   fieldLength,
			"decimals": decimals,
		})
	}
	return fields
}

func inferFieldType(value string) (string, int) {
	if _, err := strconv.ParseInt(strings.ReplaceAll(value, " ", ""), 10, 64); err == nil {
		return "N", 0
	}

	normalized := strings.ReplaceAll(value, " ", "")
	normalized = strings.ReplaceAll(normalized, ",", ".")
	if _, err := strconv.ParseFloat(normalized, 64); err == nil {
		if strings.Contains(normalized, ".") {
			return "N", 2
		}
		return "N", 0
	}

	dateLayouts := []string{
		"2006-01-02",
		"02.01.2006",
		"02/01/2006",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return "D", 0
		}
	}

	return "C", 0
}
