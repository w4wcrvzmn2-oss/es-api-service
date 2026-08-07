package dbfimport

import (
	"fmt"
	"strings"

	"github.com/extrame/xls"
)

// readXLSRecords читает старый бинарный Excel (.xls, BIFF, Excel 97-2003).
// excelize такой формат не поддерживает, поэтому используется extrame/xls.
// Строки приводятся к матрице и проходят через общий с xlsx сборщик записей.
func readXLSRecords(filePath string) ([]string, []map[string]interface{}, error) {
	wb, err := xls.Open(filePath, "utf-8")
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть XLS файл: %w", err)
	}
	if wb == nil {
		return nil, nil, fmt.Errorf("не удалось прочитать XLS файл")
	}

	for si := 0; si < wb.NumSheets(); si++ {
		sheet := wb.GetSheet(si)
		if sheet == nil {
			continue
		}

		rowCount := int(sheet.MaxRow) + 1
		rows := make([][]string, 0, rowCount)
		for i := 0; i < rowCount; i++ {
			row := sheet.Row(i)
			if row == nil {
				rows = append(rows, nil)
				continue
			}
			last := row.LastCol()
			cells := make([]string, 0, last+1)
			for c := 0; c <= last; c++ {
				cells = append(cells, strings.TrimSpace(row.Col(c)))
			}
			rows = append(rows, cells)
		}

		if countNonEmptyRows(rows) == 0 {
			continue
		}
		return buildRecordsFromRows(rows)
	}

	return nil, nil, fmt.Errorf("XLS файл не содержит данных ни на одном листе")
}
