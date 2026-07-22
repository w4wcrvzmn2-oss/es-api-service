package orderexport

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"
)

// OrderDBFFileName — имя DBF для одного заказа.
func OrderDBFFileName(orderID string, orderDate time.Time) string {
	short := strings.ReplaceAll(orderID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("order_%s_%s.dbf", orderDate.UTC().Format("20060102"), short)
}

// BuildZip упаковывает набор DBF в один ZIP (имена файлов сортируются).
func BuildZip(files map[string][]byte) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("нет файлов для архива")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
