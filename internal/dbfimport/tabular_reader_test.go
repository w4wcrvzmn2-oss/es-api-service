package dbfimport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatalf("write temp %s: %v", name, err)
	}
	return p
}

func TestDetectXMLFormat(t *testing.T) {
	if DetectDataFileFormat("price.xml") != "xml" {
		t.Fatal("xml not detected")
	}
	if !IsSupportedDataFile("PRICE.XML") {
		t.Fatal("xml must be supported")
	}
}

func TestReadXMLElementStyle(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<offers>
  <offer><code>A1</code><name>Аспирин</name><price>10.50</price></offer>
  <offer><code>A2</code><name>Парацетамол</name><price>20</price></offer>
</offers>`
	p := writeTemp(t, "el.xml", []byte(xml))
	headers, records, err := ReadTabularFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d", len(records))
	}
	if !hasHeader(headers, "code") || !hasHeader(headers, "name") || !hasHeader(headers, "price") {
		t.Fatalf("missing headers: %v", headers)
	}
	if records[0]["name"] != "Аспирин" || records[1]["price"] != "20" {
		t.Fatalf("bad values: %+v", records)
	}
}

func TestReadXMLAttributeStyle(t *testing.T) {
	xml := `<items>
  <item code="A1" name="Аспирин" price="10"/>
  <item code="A2" name="Но-шпа" price="30"/>
  <item code="A3" name="Уголь" price="5"/>
</items>`
	p := writeTemp(t, "attr.xml", []byte(xml))
	headers, records, err := ReadTabularFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("want 3 records, got %d", len(records))
	}
	if !hasHeader(headers, "code") || !hasHeader(headers, "price") {
		t.Fatalf("missing headers: %v", headers)
	}
	if records[2]["name"] != "Уголь" {
		t.Fatalf("bad value: %+v", records[2])
	}
}

func TestReadXMLWindows1251(t *testing.T) {
	// «Аспирин» в windows-1251.
	body := []byte{
		0xC0, 0xF1, 0xEF, 0xE8, 0xF0, 0xE8, 0xED, // Аспирин
	}
	head := []byte(`<?xml version="1.0" encoding="windows-1251"?><r><o><name>`)
	tail := []byte(`</name><price>10</price></o><o><name>x</name><price>20</price></o></r>`)
	p := writeTemp(t, "cp1251.xml", append(append(head, body...), tail...))
	_, records, err := ReadTabularFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 2 || records[0]["name"] != "Аспирин" {
		t.Fatalf("cp1251 decode failed: %+v", records)
	}
}

func TestExcelHeaderRowSkipsTitle(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	// A1 — титул из одной ячейки; строка 3 — реальные заголовки.
	_ = f.SetCellValue(sheet, "A1", "Прайс-лист ООО Ромашка на 06.08.2026")
	_ = f.SetCellValue(sheet, "A3", "Код")
	_ = f.SetCellValue(sheet, "B3", "Наименование")
	_ = f.SetCellValue(sheet, "C3", "Цена")
	_ = f.SetCellValue(sheet, "A4", "A1")
	_ = f.SetCellValue(sheet, "B4", "Аспирин")
	_ = f.SetCellValue(sheet, "C4", "10")
	_ = f.SetCellValue(sheet, "A5", "A2")
	_ = f.SetCellValue(sheet, "B5", "Но-шпа")
	_ = f.SetCellValue(sheet, "C5", "30")

	p := filepath.Join(t.TempDir(), "price.xlsx")
	if err := f.SaveAs(p); err != nil {
		t.Fatalf("save xlsx: %v", err)
	}

	headers, records, err := ReadTabularFile(p)
	if err != nil {
		t.Fatalf("read xlsx: %v", err)
	}
	if !hasHeader(headers, "Код") || !hasHeader(headers, "Наименование") || !hasHeader(headers, "Цена") {
		t.Fatalf("title row not skipped, headers=%v", headers)
	}
	if len(records) != 2 || records[0]["Наименование"] != "Аспирин" {
		t.Fatalf("bad records: %+v", records)
	}
}

func hasHeader(headers []string, name string) bool {
	for _, h := range headers {
		if h == name {
			return true
		}
	}
	return false
}
