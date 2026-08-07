package dbfimport

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// readSSTRecords читает прайс-файлы формата .sst (проприетарный экспорт eprica):
// текст в UTF-16LE с секциями [Header]/[Body], поля разделены ';', строки заголовков
// нет. Колонки позиционные, поэтому им присваиваются имена COLUMN_1..COLUMN_N —
// дальше они сопоставляются с целевыми полями в общем маппинге (как у DBF/Excel).
func readSSTRecords(filePath string) ([]string, []map[string]interface{}, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть SST файл: %w", err)
	}

	text := decodeBOMText(raw)
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	// Находим начало тела: строка [Body]. Если секции нет — берём строки,
	// похожие на данные (есть ';'), пропуская [Header]/метаданные.
	bodyStart := -1
	for i, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), "[Body]") {
			bodyStart = i + 1
			break
		}
	}
	if bodyStart == -1 {
		bodyStart = 0
	}

	colCount := 0
	records := make([]map[string]interface{}, 0, len(lines))
	for i := bodyStart; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		// Пропускаем возможные секционные заголовки вида [Footer].
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			continue
		}
		if !strings.Contains(ln, ";") {
			continue
		}

		fields := strings.Split(ln, ";")
		// Отбрасываем единственный хвостовой пустой элемент от завершающего ';'.
		if n := len(fields); n > 1 && strings.TrimSpace(fields[n-1]) == "" {
			fields = fields[:n-1]
		}
		if len(fields) > colCount {
			colCount = len(fields)
		}

		rec := make(map[string]interface{}, len(fields))
		hasValue := false
		for idx, val := range fields {
			v := strings.TrimSpace(val)
			if v != "" {
				hasValue = true
			}
			rec[sstColumnName(idx)] = v
		}
		if hasValue {
			records = append(records, rec)
		}
	}

	if colCount == 0 || len(records) == 0 {
		return nil, nil, fmt.Errorf("в SST файле не найдены строки прайса")
	}

	headers := make([]string, colCount)
	for i := 0; i < colCount; i++ {
		headers[i] = sstColumnName(i)
	}

	return headers, records, nil
}

// sstColumnName даёт стабильное имя позиционной колонки (1-based, как у Excel-фолбэка).
func sstColumnName(idx int) string {
	return fmt.Sprintf("COLUMN_%d", idx+1)
}

// decodeBOMText декодирует текст с учётом BOM: UTF-16LE/BE и UTF-8. Без BOM — UTF-8.
func decodeBOMText(raw []byte) string {
	if len(raw) >= 2 {
		if raw[0] == 0xFF && raw[1] == 0xFE {
			if out, _, err := transform.Bytes(unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewDecoder(), raw[2:]); err == nil {
				return string(out)
			}
		}
		if raw[0] == 0xFE && raw[1] == 0xFF {
			if out, _, err := transform.Bytes(unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM).NewDecoder(), raw[2:]); err == nil {
				return string(out)
			}
		}
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return string(raw[3:])
	}
	return string(raw)
}
