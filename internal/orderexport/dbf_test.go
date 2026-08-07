package orderexport

import (
	"bytes"
	"testing"
	"time"

	"github.com/LindsayBradford/go-dbf/godbf"
)

// TestBuildOrdersDBF_Parseable гарантирует, что сгенерированный DBF валиден
// (имена полей ≤10 символов с терминатором) и читается стандартным ридером —
// иначе система поставщика видит пустые/сдвинутые колонки.
func TestBuildOrdersDBF_Parseable(t *testing.T) {
	lines := []OrderLine{{
		OrderID:      "abc",
		GlobalSign:   "EX-0000011",
		OrderDate:    time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
		Buyer:        "Покупатель",
		Name:         "Аспирин",
		SuppName:     "Farmsnab",
		SupCode:      "0001",
		Barcode:      "04630035351394",
		Manufacturer: "Твинс Тэк",
		Qty:          3, Price: 10.5, Sum: 31.5,
	}}
	data, err := BuildOrdersDBF(lines)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tbl, err := godbf.NewFromByteArray(data, "CP866")
	if err != nil {
		t.Fatalf("godbf parse failed (битый DBF): %v", err)
	}
	for _, f := range tbl.FieldNames() {
		if len(f) > 10 {
			t.Fatalf("имя поля > 10 символов: %q", f)
		}
	}
	bc, _ := tbl.FieldValueByName(0, "BARCODE")
	if bc != "04630035351394" {
		t.Fatalf("BARCODE не прочитался round-trip: %q", bc)
	}
}

// TestBuildOrdersDBF_SupCode проверяет, что в DBF есть поле SUP_CODE и что
// значение кода поставщика попадает в запись.
func TestBuildOrdersDBF_SupCode(t *testing.T) {
	lines := []OrderLine{
		{
			OrderID:   "11111111-1111-1111-1111-111111111111",
			OrderDate: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
			Buyer:     "Тест",
			Name:      "Аспирин",
			SuppName:  "Farmsnab",
			SupCode:   "0001",
			Qty:       5,
			Price:     10.5,
			Sum:       52.5,
		},
	}
	data, err := BuildOrdersDBF(lines)
	if err != nil {
		t.Fatalf("build dbf: %v", err)
	}

	// Поле SUP_CODE должно быть объявлено в заголовке (имя в верхнем регистре, ASCII).
	if !bytes.Contains(data, []byte("SUP_CODE")) {
		t.Fatalf("SUP_CODE field descriptor not found in DBF header")
	}
	// Значение кода должно присутствовать в теле записи.
	if !bytes.Contains(data, []byte("0001")) {
		t.Fatalf("supplier code value 0001 not found in DBF body")
	}
}
