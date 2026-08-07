package dbfimport

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// readXMLRecords читает произвольный «плоский» XML-прайс и приводит его к тому же
// виду (заголовки + записи), что DBF и Excel, чтобы пройти общий маппинг полей.
//
// Формат XML у поставщиков не стандартизирован, поэтому строки прайса определяются
// эвристически: ищется наиболее «весомый» набор одноимённых элементов-братьев
// (например <offer>, <Товар>, <item>, <ROW>). Для каждой такой строки колонками
// становятся её атрибуты и вложенные листовые элементы.
//
// Поддерживаются оба распространённых стиля:
//
//	<offers><offer><code>1</code><name>Аспирин</name><price>10</price></offer>...</offers>
//	<items><item code="1" name="Аспирин" price="10"/>...</items>
func readXMLRecords(filePath string) ([]string, []map[string]interface{}, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось открыть XML файл: %w", err)
	}
	defer f.Close()

	root, err := parseXMLTree(f)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось разобрать XML: %w", err)
	}

	rows := findRowNodes(root)
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("в XML не найдены повторяющиеся элементы прайса")
	}

	headers := make([]string, 0, 16)
	seen := make(map[string]bool)
	records := make([]map[string]interface{}, 0, len(rows))

	for _, row := range rows {
		rec := make(map[string]interface{})
		collectRowFields(row, rec, seen, &headers)
		if len(rec) > 0 {
			records = append(records, rec)
		}
	}

	if len(headers) == 0 || len(records) == 0 {
		return nil, nil, fmt.Errorf("в XML не удалось выделить колонки прайса")
	}

	return headers, records, nil
}

// xmlNode — упрощённое дерево XML (нужны имена, атрибуты, текст и дети).
type xmlNode struct {
	Name     string
	Attrs    []xmlAttr
	Children []*xmlNode
	Text     string
}

type xmlAttr struct {
	Name  string
	Value string
}

func (n *xmlNode) isLeaf() bool { return len(n.Children) == 0 }

func (n *xmlNode) fieldCount() int {
	c := len(n.Attrs)
	for _, ch := range n.Children {
		if ch.isLeaf() {
			c++
		}
	}
	return c
}

// parseXMLTree строит дерево, корректно декодируя кириллические кодировки
// (windows-1251 / koi8-r), которые часто встречаются в прайсах.
func parseXMLTree(r io.Reader) (*xmlNode, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.CharsetReader = charsetReader

	root := &xmlNode{Name: "#root"}
	stack := []*xmlNode{root}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{Name: t.Name.Local}
			for _, a := range t.Attr {
				if a.Name.Local == "" {
					continue
				}
				node.Attrs = append(node.Attrs, xmlAttr{Name: a.Name.Local, Value: strings.TrimSpace(a.Value)})
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			node := stack[len(stack)-1]
			if s := strings.TrimSpace(string(t)); s != "" {
				if node.Text == "" {
					node.Text = s
				} else {
					node.Text += " " + s
				}
			}
		}
	}

	return root, nil
}

// findRowNodes выбирает набор строк прайса — наиболее «весомую» группу
// одноимённых элементов-братьев (по количеству строк и полей в них).
func findRowNodes(root *xmlNode) []*xmlNode {
	var best []*xmlNode
	bestScore := 0

	var walk func(n *xmlNode)
	walk = func(n *xmlNode) {
		groups := make(map[string][]*xmlNode)
		order := make([]string, 0, len(n.Children))
		for _, ch := range n.Children {
			if _, ok := groups[ch.Name]; !ok {
				order = append(order, ch.Name)
			}
			groups[ch.Name] = append(groups[ch.Name], ch)
		}

		for _, name := range order {
			g := groups[name]
			totalFields := 0
			for _, m := range g {
				totalFields += m.fieldCount()
			}
			// Кандидат в строки: есть поля хотя бы у части элементов.
			if totalFields == 0 {
				continue
			}
			// Вес: число строк × среднее число полей. Повторяющиеся элементы
			// с данными выигрывают у одиночных обёрток.
			score := len(g) * totalFields
			if score > bestScore {
				bestScore = score
				best = g
			}
		}

		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)

	return best
}

// collectRowFields разворачивает строку в плоский набор колонок: атрибуты строки
// и все листовые элементы её поддерева (по локальному имени).
func collectRowFields(row *xmlNode, rec map[string]interface{}, seen map[string]bool, headers *[]string) {
	put := func(name, value string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if !seen[name] {
			seen[name] = true
			*headers = append(*headers, name)
		}
		// Первое непустое значение выигрышнее пустого (атрибут vs пустой тег).
		if existing, ok := rec[name]; ok {
			if s, _ := existing.(string); strings.TrimSpace(s) != "" {
				return
			}
		}
		rec[name] = value
	}

	for _, a := range row.Attrs {
		put(a.Name, a.Value)
	}

	var walkLeaves func(n *xmlNode)
	walkLeaves = func(n *xmlNode) {
		for _, ch := range n.Children {
			for _, a := range ch.Attrs {
				put(ch.Name+"_"+a.Name, a.Value)
			}
			if ch.isLeaf() {
				put(ch.Name, ch.Text)
			} else {
				walkLeaves(ch)
			}
		}
	}
	walkLeaves(row)
}

// charsetReader декодирует не-UTF-8 XML (windows-1251/cp1251, koi8-r, windows-1252).
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "windows-1251", "cp1251", "windows1251":
		return transform.NewReader(input, charmap.Windows1251.NewDecoder()), nil
	case "koi8-r", "koi8r":
		return transform.NewReader(input, charmap.KOI8R.NewDecoder()), nil
	case "windows-1252", "cp1252":
		return transform.NewReader(input, charmap.Windows1252.NewDecoder()), nil
	case "iso-8859-1", "latin1":
		return transform.NewReader(input, charmap.ISO8859_1.NewDecoder()), nil
	default:
		// Неизвестную кодировку читаем как есть — лучше, чем падать.
		return input, nil
	}
}
