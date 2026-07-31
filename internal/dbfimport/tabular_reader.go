package dbfimport

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LindsayBradford/go-dbf/godbf"
	"github.com/xuri/excelize/v2"
)

var supportedDataFileExts = []string{".dbf", ".xlsx", ".xlsm"}

// DetectDataFileFormat возвращает поддерживаемый формат файла данных.
func DetectDataFileFormat(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".dbf":
		return "dbf"
	case ".xlsx", ".xlsm":
		return "excel"
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
	default:
		return nil, nil, fmt.Errorf("неподдерживаемый формат файла: %s", filepath.Ext(filePath))
	}
}

func readDBFRecords(filePath string) ([]string, []map[string]interface{}, error) {
	dbfTable, err := godbf.NewFromFile(filePath, "CP866")
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть DBF файл: %w", err)
	}

	fieldNames := dbfTable.FieldNames()
	headerRecordCount := dbfTable.NumberOfRecords()
	records := make([]map[string]interface{}, 0, headerRecordCount)
	maxAttempts := headerRecordCount * 2
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	consecutiveErrors := 0
	maxConsecutiveErrors := 10

	for i := 0; i < maxAttempts; i++ {
		record := make(map[string]interface{})
		recordHasError := false
		allFieldsError := true

		for _, fieldName := range fieldNames {
			fieldValue, err := dbfTable.FieldValueByName(i, fieldName)
			if err != nil {
				recordHasError = true
				if fieldName == fieldNames[0] {
					errMsg := err.Error()
					if errMsg == "index out of range" ||
						strings.Contains(errMsg, "out of range") ||
						strings.Contains(errMsg, "EOF") ||
						strings.Contains(errMsg, "index") {
						consecutiveErrors++
						if consecutiveErrors >= maxConsecutiveErrors {
							return fieldNames, records, nil
						}
						break
					}
				}
				fieldValue = ""
			} else {
				allFieldsError = false
				record[fieldName] = fieldValue
			}
		}

		if allFieldsError && recordHasError {
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				return fieldNames, records, nil
			}
			continue
		}

		if !allFieldsError {
			for _, fieldName := range fieldNames {
				if _, exists := record[fieldName]; exists {
					continue
				}
				fieldValue, err := dbfTable.FieldValueByName(i, fieldName)
				if err != nil {
					record[fieldName] = ""
					continue
				}
				record[fieldName] = fieldValue
			}
			records = append(records, record)
			consecutiveErrors = 0
		}
	}

	return fieldNames, records, nil
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

	sheetName := sheets[0]
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось прочитать лист %s: %w", sheetName, err)
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("Excel файл не содержит строк")
	}

	headerRowIdx := -1
	var headers []string
	for i, row := range rows {
		candidate := normalizeExcelHeaders(row)
		if len(candidate) == 0 {
			continue
		}
		headers = candidate
		headerRowIdx = i
		break
	}
	if headerRowIdx == -1 || len(headers) == 0 {
		return nil, nil, fmt.Errorf("не удалось определить строку заголовков в Excel")
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
