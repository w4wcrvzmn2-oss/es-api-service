package orderexport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// OrderLine — одна строка выгрузки (позиция заказа).
type OrderLine struct {
	OrderID      string
	GlobalSign   string
	OrderDate    time.Time
	Buyer        string
	Address      string
	Code         string
	Name         string
	SuppName     string
	SupCode      string // код поставщика (цифры)
	Barcode      string
	Manufacturer string
	Country      string
	Series       string
	Batch        string
	Expiry       *time.Time
	Qty          float64
	Price        float64
	Sum          float64
}

type fieldDesc struct {
	name     string
	typ      byte // C, N, D
	length   byte
	decimals byte
}

// BuildOrdersDBF собирает DBF (dBase III, CP866) из позиций заказов.
func BuildOrdersDBF(lines []OrderLine) ([]byte, error) {
	fields := []fieldDesc{
		{"ORDER_ID", 'C', 36, 0},
		{"GLOBALSIGN", 'C', 16, 0}, // ровно 10 символов (dBase limit); было GLOBAL_SIGN (11) — ломало таблицу полей
		{"ORD_DATE", 'D', 8, 0},
		{"BUYER", 'C', 80, 0},
		{"ADDRESS", 'C', 120, 0},
		{"CODE", 'C', 40, 0},
		{"NAME", 'C', 120, 0},
		{"SUP_NAME", 'C', 120, 0},
		{"BARCODE", 'C', 20, 0},
		{"MANUFACT", 'C', 80, 0},
		{"COUNTRY", 'C', 40, 0},
		{"SERIES", 'C', 30, 0},
		{"BATCH", 'C', 30, 0},
		{"EXPIRY", 'D', 8, 0},
		{"QTY", 'N', 12, 3},
		{"PRICE", 'N', 12, 2},
		{"SUMMA", 'N', 14, 2},
		{"SUP_CODE", 'C', 10, 0}, // код поставщика — в конце, чтобы не сдвигать существующие поля
	}

	var recLen uint16 = 1 // deleted flag
	for _, f := range fields {
		recLen += uint16(f.length)
	}

	headerLen := uint16(32 + len(fields)*32 + 1)
	now := time.Now()
	buf := &bytes.Buffer{}

	// Header (32 bytes)
	hdr := make([]byte, 32)
	hdr[0] = 0x03 // dBase III
	hdr[1] = byte(now.Year() - 1900)
	hdr[2] = byte(now.Month())
	hdr[3] = byte(now.Day())
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(lines)))
	binary.LittleEndian.PutUint16(hdr[8:], headerLen)
	binary.LittleEndian.PutUint16(hdr[10:], recLen)
	if _, err := buf.Write(hdr); err != nil {
		return nil, err
	}

	// Field descriptors
	for _, f := range fields {
		desc := make([]byte, 32)
		name := strings.ToUpper(f.name)
		// Имя поля dBase — максимум 10 символов; байт[10] обязан быть нулевым
		// терминатором. Длинное имя (напр. 11-символьное) затирает терминатор
		// и ломает таблицу полей — ридеры читают колонки пусто/со сдвигом.
		if len(name) > 10 {
			name = name[:10]
		}
		copy(desc[0:10], []byte(name)) // desc[10] остаётся 0 — терминатор
		desc[11] = f.typ
		desc[16] = f.length
		desc[17] = f.decimals
		if _, err := buf.Write(desc); err != nil {
			return nil, err
		}
	}
	buf.WriteByte(0x0D) // terminator

	enc := charmap.CodePage866.NewEncoder()
	for _, line := range lines {
		rec := make([]byte, recLen)
		rec[0] = 0x20 // not deleted
		off := 1
		writeC := func(s string, n int) {
			b := encodeCP866(enc, s, n)
			copy(rec[off:off+n], b)
			off += n
		}
		writeD := func(t time.Time) {
			var s string
			if t.IsZero() {
				s = strings.Repeat(" ", 8)
			} else {
				s = t.Format("20060102")
			}
			copy(rec[off:off+8], []byte(s))
			off += 8
		}
		writeN := func(v float64, length, decimals int) {
			fmtStr := fmt.Sprintf("%%%d.%df", length, decimals)
			s := fmt.Sprintf(fmtStr, v)
			if len(s) > length {
				s = s[len(s)-length:]
			}
			padded := make([]byte, length)
			for i := range padded {
				padded[i] = ' '
			}
			copy(padded[length-len(s):], []byte(s))
			copy(rec[off:off+length], padded)
			off += length
		}

		writeC(line.OrderID, 36)
		writeC(line.GlobalSign, 16)
		writeD(line.OrderDate)
		writeC(line.Buyer, 80)
		writeC(line.Address, 120)
		writeC(line.Code, 40)
		writeC(line.Name, 120)
		writeC(line.SuppName, 120)
		writeC(line.Barcode, 20)
		writeC(line.Manufacturer, 80)
		writeC(line.Country, 40)
		writeC(line.Series, 30)
		writeC(line.Batch, 30)
		if line.Expiry != nil {
			writeD(*line.Expiry)
		} else {
			writeD(time.Time{})
		}
		writeN(line.Qty, 12, 3)
		writeN(line.Price, 12, 2)
		writeN(line.Sum, 14, 2)
		writeC(line.SupCode, 10)

		if _, err := buf.Write(rec); err != nil {
			return nil, err
		}
	}
	buf.WriteByte(0x1A) // EOF
	return buf.Bytes(), nil
}

type encoder interface {
	Bytes([]byte) ([]byte, error)
}

func encodeCP866(enc encoder, s string, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	if s == "" {
		return out
	}
	// Обрезаем по рунам, затем кодируем.
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes)
		encoded, err := enc.Bytes([]byte(candidate))
		if err == nil && len(encoded) <= n {
			copy(out, encoded)
			return out
		}
		runes = runes[:len(runes)-1]
	}
	// Fallback: ASCII-safe truncate
	b := []byte(s)
	if !utf8.Valid(b) {
		b = []byte(strings.ToValidUTF8(s, "?"))
	}
	if len(b) > n {
		b = b[:n]
	}
	copy(out, b)
	return out
}
