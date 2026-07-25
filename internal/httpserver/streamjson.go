package httpserver

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// JSONStreamer обеспечивает потоковую сериализацию JSON массивов
type JSONStreamer struct {
	writer    io.Writer
	firstItem bool
}

// NewJSONStreamer создаёт новый JSONStreamer
func NewJSONStreamer(w io.Writer) *JSONStreamer {
	return &JSONStreamer{
		writer:    w,
		firstItem: true,
	}
}

// WriteArrayStart записывает начало JSON массива
func (js *JSONStreamer) WriteArrayStart() error {
	_, err := js.writer.Write([]byte("["))
	return err
}

// WriteArrayEnd записывает конец JSON массива
func (js *JSONStreamer) WriteArrayEnd() error {
	_, err := js.writer.Write([]byte("]"))
	return err
}

// WriteItem записывает один элемент массива
func (js *JSONStreamer) WriteItem(item interface{}) error {
	if !js.firstItem {
		if _, err := js.writer.Write([]byte(",")); err != nil {
			return err
		}
	}
	js.firstItem = false

	data, err := json.Marshal(item)
	if err != nil {
		return err
	}

	_, err = js.writer.Write(data)
	return err
}

// WriteRowAsJSON записывает строку из sql.Rows как JSON объект
func (js *JSONStreamer) WriteRowAsJSON(rows *sql.Rows, columnsInfo interface{}) error {
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Создаём слайс интерфейсов для сканирования
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	if err := rows.Scan(valuePtrs...); err != nil {
		return err
	}

	// Создаём map для JSON объекта
	result := make(map[string]interface{})
	for i, col := range columns {
		val := values[i]

		// Обработка NULL значений и специальных типов
		if val == nil {
			result[col] = nil
		} else {
			result[col] = convertSQLValueWithColumnInfo(val, col)
		}
	}

	return js.WriteItem(result)
}

// convertSQLValueWithColumnInfo преобразует значение из SQL с учётом имени колонки
func convertSQLValueWithColumnInfo(val interface{}, columnName string) interface{} {
	if val == nil {
		return nil
	}

	// Проверяем по имени колонки, является ли это GUID
	isGUIDColumn := isGUIDColumnName(columnName)

	// UUID / бинарные GUID из PostgreSQL
	switch v := val.(type) {
	case uuid.UUID:
		return v.String()
	case [16]byte:
		return formatGUID(v[:])
	case []byte:
		// Проверяем, является ли это GUID по длине или имени колонки
		if len(v) == 16 || isGUIDColumn {
			// Конвертируем в стандартный формат GUID
			return formatGUID(v)
		}
		// Для других бинарных данных проверяем на печатность
		if isPrintableBytes(v) {
			return string(v)
		}
		// Если не печатное - возвращаем как hex
		return hex.EncodeToString(v)
	case time.Time:
		// Форматируем время в RFC3339
		return v.Format(time.RFC3339)
	}

	// Обработка через reflection для остальных типов
	v := reflect.ValueOf(val)
	switch v.Kind() {
	case reflect.Int64:
		return v.Int()
	case reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			bytes := v.Bytes()
			// Проверяем по имени колонки
			if isGUIDColumn && len(bytes) == 16 {
				return formatGUID(bytes)
			}
			if isPrintableBytes(bytes) {
				return string(bytes)
			}
			return hex.EncodeToString(bytes)
		}
	}

	// Для всех остальных типов используем строковое представление
	return fmt.Sprintf("%v", val)
}

// isGUIDColumnName проверяет, является ли колонка GUID по её имени
func isGUIDColumnName(columnName string) bool {
	name := strings.ToLower(columnName)
	return strings.Contains(name, "guid") ||
		strings.HasPrefix(name, "guid_") ||
		strings.HasSuffix(name, "_guid") ||
		strings.Contains(name, "uuid") ||
		(strings.Contains(name, "_id") && !strings.Contains(name, "acid")) ||
		(strings.HasSuffix(name, "id") && len(name) > 2)
}

// formatGUID форматирует 16-байтовый массив как стандартный GUID
func formatGUID(b []byte) string {
	if len(b) != 16 {
		return hex.EncodeToString(b)
	}

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		// Первые 4 байта (little-endian)
		uint32(b[3])<<24|uint32(b[2])<<16|uint32(b[1])<<8|uint32(b[0]),
		// Следующие 2 байта (little-endian)
		uint16(b[5])<<8|uint16(b[4]),
		// Следующие 2 байта (little-endian)
		uint16(b[7])<<8|uint16(b[6]),
		// Следующие 2 байта (big-endian)
		uint16(b[8])<<8|uint16(b[9]),
		// Последние 6 байт (big-endian)
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]))
}

// isPrintableBytes проверяет, являются ли байты печатными символами
func isPrintableBytes(b []byte) bool {
	if len(b) == 0 {
		return true
	}

	// Проверяем на UTF-8 текст
	if isValidUTF8Text(b) {
		return true
	}

	// Проверяем на ASCII печатные символы
	for _, byte := range b {
		if byte < 32 || byte > 126 {
			return false
		}
	}
	return true
}

// isValidUTF8Text проверяет, является ли байтовый массив валидным UTF-8 текстом
func isValidUTF8Text(b []byte) bool {
	s := string(b)
	// Проверяем, что это валидный UTF-8 и содержит печатные символы
	for _, r := range s {
		if r == '\uFFFD' || (r < 32 && r != '\t' && r != '\n' && r != '\r') {
			return false
		}
	}
	return len(strings.TrimSpace(s)) > 0
}
