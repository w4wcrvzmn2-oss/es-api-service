package orderexport

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
)

// ExportColumn — колонка выгрузки: заголовок (из шаблона поставщика) + наше поле-источник.
type ExportColumn struct {
	Header string
	Field  string // ключ поля (global_sign, item_name, qty, ...) или "" — пустая колонка
}

// BuildExport строит выгрузку накладной по маппингу в заданном формате.
// rows — по одной map[field]=значение на позицию. Возвращает данные + расширение файла.
func BuildExport(format, subformat string, columns []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	// GlobalSign (EX-…) добавляется всегда — если поставщик его не разметил.
	hasGS := false
	for _, c := range columns {
		if c.Field == "global_sign" {
			hasGS = true
			break
		}
	}
	if !hasGS {
		columns = append([]ExportColumn{{Header: "GlobalSign", Field: "global_sign"}}, columns...)
	}

	switch strings.ToLower(format) {
	case "xlsx", "excel":
		return buildXLSX(columns, rows)
	case "csv":
		return buildCSV(columns, rows)
	case "xml":
		return buildXML(columns, rows)
	case "dbf":
		return buildGenericDBF(columns, rows)
	case "1c", "1с":
		return build1C(subformat, columns, rows)
	default:
		return buildCSV(columns, rows)
	}
}

func cellVal(row map[string]string, field string) string {
	if field == "" {
		return ""
	}
	return row[field]
}

// ---- CSV ----
func buildCSV(cols []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	var b bytes.Buffer
	b.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM — чтобы Excel RU не ломал кириллицу
	hs := make([]string, len(cols))
	for i, c := range cols {
		hs[i] = csvEscape(c.Header)
	}
	b.WriteString(strings.Join(hs, ";") + "\r\n")
	for _, r := range rows {
		vs := make([]string, len(cols))
		for i, c := range cols {
			vs[i] = csvEscape(cellVal(r, c.Field))
		}
		b.WriteString(strings.Join(vs, ";") + "\r\n")
	}
	return b.Bytes(), "csv", nil
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ";\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// ---- XML ----
func buildXML(cols []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n<Накладная>\n")
	for _, r := range rows {
		b.WriteString("  <Позиция>\n")
		for _, c := range cols {
			tag := xmlTag(c.Header)
			b.WriteString("    <" + tag + ">" + xmlEscape(cellVal(r, c.Field)) + "</" + tag + ">\n")
		}
		b.WriteString("  </Позиция>\n")
	}
	b.WriteString("</Накладная>\n")
	return b.Bytes(), "xml", nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func xmlTag(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "Поле"
	}
	var out []rune
	for i, r := range h {
		if r == ' ' || r == '-' || r == '.' || r == '/' || r == '\\' || r == '(' || r == ')' {
			out = append(out, '_')
			continue
		}
		if i == 0 && r >= '0' && r <= '9' {
			out = append(out, '_')
		}
		out = append(out, r)
	}
	return string(out)
}

// ---- XLSX ----
func buildXLSX(cols []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Накладная"
	_ = f.SetSheetName("Sheet1", sheet)
	for i, c := range cols {
		ref, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, ref, c.Header)
	}
	for ri, r := range rows {
		for ci, c := range cols {
			ref, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			_ = f.SetCellValue(sheet, ref, cellVal(r, c.Field))
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "xlsx", nil
}

// ---- DBF (dBase III, CP866; все поля символьные) ----
func buildGenericDBF(cols []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	const flen = 60
	names := make([]string, len(cols))
	used := map[string]bool{}
	for i, c := range cols {
		names[i] = dbfFieldName(c.Header, i, used)
	}
	var recLen uint16 = 1
	for range cols {
		recLen += flen
	}
	headerLen := uint16(32 + len(cols)*32 + 1)
	now := time.Now()

	buf := &bytes.Buffer{}
	hdr := make([]byte, 32)
	hdr[0] = 0x03
	hdr[1] = byte(now.Year() - 1900)
	hdr[2] = byte(now.Month())
	hdr[3] = byte(now.Day())
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(rows)))
	binary.LittleEndian.PutUint16(hdr[8:], headerLen)
	binary.LittleEndian.PutUint16(hdr[10:], recLen)
	buf.Write(hdr)

	for _, n := range names {
		desc := make([]byte, 32)
		copy(desc[0:10], []byte(n))
		desc[11] = 'C'
		desc[16] = flen
		buf.Write(desc)
	}
	buf.WriteByte(0x0D)

	enc := charmap.CodePage866.NewEncoder()
	for _, r := range rows {
		rec := make([]byte, recLen)
		rec[0] = 0x20
		off := 1
		for _, c := range cols {
			b := encodeCP866(enc, cellVal(r, c.Field), flen)
			copy(rec[off:off+flen], b)
			off += flen
		}
		buf.Write(rec)
	}
	buf.WriteByte(0x1A)
	return buf.Bytes(), "dbf", nil
}

func dbfFieldName(header string, idx int, used map[string]bool) string {
	var out []byte
	for _, r := range strings.ToUpper(header) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out = append(out, byte(r))
		}
		if len(out) >= 10 {
			break
		}
	}
	name := string(out)
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = fmt.Sprintf("F%d", idx+1)
	}
	if len(name) > 10 {
		name = name[:10]
	}
	base := name
	k := 1
	for used[name] {
		suf := fmt.Sprintf("%d", k)
		if len(base)+len(suf) > 10 {
			base = base[:10-len(suf)]
		}
		name = base + suf
		k++
	}
	used[name] = true
	return name
}

// ---- 1С (каркас; под-форматы добавляются по спецификации/примеру) ----
func build1C(subformat string, cols []ExportColumn, rows []map[string]string) ([]byte, string, error) {
	switch strings.ToLower(subformat) {
	case "commerceml", "cml":
		// TODO: полноценный CommerceML по спецификации 1С
		return buildXML(cols, rows)
	case "dbf":
		return buildGenericDBF(cols, rows)
	case "xlsx", "excel":
		return buildXLSX(cols, rows)
	default:
		// текстовый обмен по умолчанию
		return buildCSV(cols, rows)
	}
}
