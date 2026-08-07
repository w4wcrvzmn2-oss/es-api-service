package dbfimport

import (
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

func TestDetectSSTFormat(t *testing.T) {
	if DetectDataFileFormat("price1_260806.sst") != "sst" {
		t.Fatal("sst not detected")
	}
	if !IsSupportedDataFile("PRICE.SST") {
		t.Fatal("sst must be supported")
	}
}

func TestReadSSTUTF16(t *testing.T) {
	body := "[Header]\r\n" +
		"06.08.2026;15:10:02;0f689e60-f76c-41c8-b87f-a3860969db1f;127187\r\n" +
		"[Body]\r\n" +
		"058097f9;4221f895;Нейромультивит таб. №20;Бауш Хелс ООО;04607013581616;39;512.05;30.09.2028;.00;0;\r\n" +
		"ec2b6730;55bc93a9;Магний хелат №120;Эвалар ЗАО;04602242024040;3;2023.09;06.04.2029;.00;0;\r\n"

	// Кодируем в UTF-16LE с BOM — как реальный .sst.
	enc := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	utf16bytes, _, err := transform.Bytes(enc, []byte(body))
	if err != nil {
		t.Fatalf("encode utf16: %v", err)
	}
	p := writeTemp(t, "price.sst", utf16bytes)

	headers, records, err := ReadTabularFile(p)
	if err != nil {
		t.Fatalf("read sst: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("want 2 records, got %d", len(records))
	}
	// [2]=name → COLUMN_3, [6]=price → COLUMN_7 (индексы 1-based).
	if records[0]["COLUMN_3"] != "Нейромультивит таб. №20" {
		t.Fatalf("bad name: %+v", records[0]["COLUMN_3"])
	}
	if records[0]["COLUMN_7"] != "512.05" {
		t.Fatalf("bad price: %+v", records[0]["COLUMN_7"])
	}
	if !hasHeader(headers, "COLUMN_6") || !hasHeader(headers, "COLUMN_7") {
		t.Fatalf("missing positional headers: %v", headers)
	}
	_ = filepath.Base(p)
}
