package db

import (
	"fmt"
	"regexp"
	"strings"
)

// boolColPat — колонки BOOLEAN, которые в MSSQL сравнивали с 0/1.
const boolColPat = `IsActive|IsDefault|IsProcessed|IsRequired|IsManual|OldIsConfirmed|NewIsConfirmed|RxRequired|ControlMinOrder|is_active`

var (
	reBoolToggle = regexp.MustCompile(`(?i)CASE\s+WHEN\s+((?:[\w]+\.)?(?:` + boolColPat + `))\s*=\s*1\s+THEN\s+0\s+ELSE\s+1\s+END`)
	reBoolEq1    = regexp.MustCompile(`(?i)\b((?:[\w]+\.)?(?:` + boolColPat + `))\s*=\s*1\b`)
	reBoolEq0    = regexp.MustCompile(`(?i)\b((?:[\w]+\.)?(?:` + boolColPat + `))\s*=\s*0\b`)
	reGetDate    = regexp.MustCompile(`(?i)\bGETDATE\s*\(\s*\)`)
	reNVarchar   = regexp.MustCompile(`(?i)\bNVARCHAR\s*(\(\s*\d+\s*\))?`)
	reBracketOrd = regexp.MustCompile(`(?i)\[Order\]`)
)

// NormalizeBoolSQL заменяет сравнения/присваивания bool-колонок с 0/1 на TRUE/FALSE.
// Нужно потому что в PostgreSQL IsActive — BOOLEAN, а не bit/int как в MSSQL.
func NormalizeBoolSQL(sql string) string {
	if sql == "" {
		return sql
	}
	sql = reBoolToggle.ReplaceAllString(sql, `NOT $1`)
	sql = reBoolEq1.ReplaceAllString(sql, `$1 = TRUE`)
	sql = reBoolEq0.ReplaceAllString(sql, `$1 = FALSE`)
	// COALESCE(@flag, 1) / COALESCE(?, 1) → TRUE/FALSE (иначе integer vs boolean)
	sql = regexp.MustCompile(`(?i)COALESCE\s*\(\s*(@[A-Za-z_][A-Za-z0-9_]*|\?)\s*,\s*1\s*\)`).ReplaceAllString(sql, `COALESCE($1, TRUE)`)
	sql = regexp.MustCompile(`(?i)COALESCE\s*\(\s*(@[A-Za-z_][A-Za-z0-9_]*|\?)\s*,\s*0\s*\)`).ReplaceAllString(sql, `COALESCE($1, FALSE)`)
	return sql
}

// NormalizeMSSQLSQL убирает типичные T-SQL конструкции, оставшиеся после миграции.
func NormalizeMSSQLSQL(sql string) string {
	if sql == "" {
		return sql
	}
	sql = reGetDate.ReplaceAllString(sql, `(NOW() AT TIME ZONE 'utc')`)
	sql = reNVarchar.ReplaceAllString(sql, `TEXT`)
	sql = reBracketOrd.ReplaceAllString(sql, `"Order"`)
	sql = rewriteDateAdd(sql)
	sql = rewriteLen(sql)
	sql = rewritePlusConcat(sql)
	// is_active / "is_active" = 1|0 в ON CONFLICT / SET
	sql = regexp.MustCompile(`(?i)("is_active"|is_active)\s*=\s*1\b`).ReplaceAllString(sql, `$1 = TRUE`)
	sql = regexp.MustCompile(`(?i)("is_active"|is_active)\s*=\s*0\b`).ReplaceAllString(sql, `$1 = FALSE`)
	return sql
}

// DATEADD(unit, n, expr) → (expr) + INTERVAL 'n unit'
func rewriteDateAdd(sql string) string {
	upper := strings.ToUpper(sql)
	for {
		idx := strings.Index(upper, "DATEADD(")
		if idx < 0 {
			break
		}
		// find matching ')'
		start := idx + len("DATEADD(")
		depth := 1
		end := -1
		for i := start; i < len(sql); i++ {
			switch sql[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break
		}
		inner := sql[start:end]
		parts := splitCSVRespectingParens(inner)
		if len(parts) != 3 {
			// skip malformed — advance past this DATEADD(
			upper = upper[:idx] + strings.Repeat("X", end+1-idx) + upper[end+1:]
			continue
		}
		unit := strings.ToLower(strings.TrimSpace(parts[0]))
		n := strings.TrimSpace(parts[1])
		expr := strings.TrimSpace(parts[2])
		switch unit {
		case "minute", "minutes":
			unit = "minutes"
		case "hour", "hours":
			unit = "hours"
		case "day", "days":
			unit = "days"
		case "second", "seconds":
			unit = "seconds"
		case "week", "weeks":
			unit = "weeks"
		case "month", "months":
			unit = "months"
		case "year", "years":
			unit = "years"
		default:
			upper = upper[:idx] + strings.Repeat("X", end+1-idx) + upper[end+1:]
			continue
		}
		repl := fmt.Sprintf(`((%s) + INTERVAL '%s %s')`, expr, n, unit)
		sql = sql[:idx] + repl + sql[end+1:]
		upper = strings.ToUpper(sql)
	}
	return sql
}

func splitCSVRespectingParens(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// LEN(x) → LENGTH(x) (MSSQL)
func rewriteLen(sql string) string {
	upper := strings.ToUpper(sql)
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		if i+4 <= len(sql) && upper[i:i+4] == "LEN(" {
			// avoid matching LENGTH(
			if i > 0 {
				prev := upper[i-1]
				if (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
					b.WriteByte(sql[i])
					i++
					continue
				}
			}
			b.WriteString("LENGTH(")
			i += 4
			continue
		}
		b.WriteByte(sql[i])
		i++
	}
	return b.String()
}

// '%'+@x+'%' → '%' || @x || '%'
func rewritePlusConcat(sql string) string {
	re := regexp.MustCompile(`(?i)'%'(\s*)\+(\s*)(@[A-Za-z_][A-Za-z0-9_]*)(\s*)\+(\s*)'%'`)
	return re.ReplaceAllString(sql, `'%'$1||$2$3$4||$5'%'`)
}

// PreparePGSQL нормализует MSSQL-остатки, bool-литералы и квотирует PascalCase для PostgreSQL.
func PreparePGSQL(sql string) string {
	return QuotePascalSQL(NormalizeBoolSQL(NormalizeMSSQLSQL(sql)))
}

// QuotePascalSQL оборачивает PascalCase-идентификаторы в двойные кавычки,
// чтобы PostgreSQL не складывал их в нижний регистр (PriceList → "PriceList").
// Уже закавыченные идентификаторы и строковые литералы не трогает.
// Ключевые слова SQL в UPPER CASE (SELECT, FROM, …) не затрагиваются.
func QuotePascalSQL(sql string) string {
	if sql == "" {
		return sql
	}
	var b strings.Builder
	b.Grow(len(sql) + 64)
	i := 0
	for i < len(sql) {
		c := sql[i]

		// строковый литерал '...'
		if c == '\'' {
			b.WriteByte(c)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						b.WriteByte(sql[i])
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}

		// уже закавыченный идентификатор "..."
		if c == '"' {
			b.WriteByte(c)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}

		// es_* (mssql-стиль в нижнем регистре) → каноническое имя в кавычках.
		if c >= 'a' && c <= 'z' && (i == 0 || !isIdentByte(sql[i-1])) {
			if i+3 <= len(sql) && (sql[i] == 'e' || sql[i] == 'E') &&
				(sql[i+1] == 's' || sql[i+1] == 'S') && sql[i+2] == '_' {
				start := i
				i++
				for i < len(sql) && isIdentByte(sql[i]) {
					i++
				}
				ident := sql[start:i]
				canon := canonicalEsTableName(ident)
				b.WriteByte('"')
				b.WriteString(canon)
				b.WriteByte('"')
				continue
			}
		}

		// PascalCase / ALL_CAPS идентификаторы (INN, GUID_ES, ES_ATC).
		// UPPER SQL-ключевые слова (SELECT, AS, UUID, …) не трогаем.
		if c >= 'A' && c <= 'Z' && (i == 0 || !isIdentByte(sql[i-1])) {
			start := i
			i++
			hasLower := false
			for i < len(sql) {
				ch := sql[i]
				if !isIdentByte(ch) {
					break
				}
				if ch >= 'a' && ch <= 'z' {
					hasLower = true
				}
				i++
			}
			ident := sql[start:i]
			if strings.HasPrefix(strings.ToUpper(ident), "ES_") && !isSkippedPascalWord(ident) {
				canon := canonicalEsTableName(ident)
				b.WriteByte('"')
				b.WriteString(canon)
				b.WriteByte('"')
				continue
			}
			if !isSkippedPascalWord(ident) && (hasLower || isAllCapsIdent(ident)) {
				b.WriteByte('"')
				b.WriteString(ident)
				b.WriteByte('"')
			} else {
				b.WriteString(ident)
			}
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

func isIdentByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

// isAllCapsIdent — INN, GUID_ES, ES_ATC (буквы только A–Z, допустимы цифры/_).
func isAllCapsIdent(s string) bool {
	if s == "" {
		return false
	}
	hasLetter := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' {
			return false
		}
		if ch >= 'A' && ch <= 'Z' {
			hasLetter = true
		}
	}
	return hasLetter
}

// canonicalEsTableName приводит es_* к фактическому имени таблицы в PG.
// В 002_elfisa_schema.sql препарат лежит как "es_ef2", остальные справочники — "ES_*".
func canonicalEsTableName(ident string) string {
	switch strings.ToLower(ident) {
	case "es_ef2":
		// В 002_elfisa_schema.sql создано как "es_ef2" (lowercase).
		return "es_ef2"
	default:
		return strings.ToUpper(ident)
	}
}

func isSkippedPascalWord(s string) bool {
	switch strings.ToUpper(s) {
	case "OVER", "AS", "ON", "IN", "IS", "OR", "AND", "NOT", "BY", "SET",
		"ASC", "DESC", "END", "CASE", "WHEN", "THEN", "ELSE", "NULL",
		"TRUE", "FALSE", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "CROSS",
		"JOIN", "FROM", "WHERE", "INTO", "VALUES", "UPDATE", "DELETE", "INSERT",
		"SELECT", "LIMIT", "OFFSET", "HAVING", "UNION", "EXCEPT", "INTERSECT",
		"EXISTS", "BETWEEN", "LIKE", "ILIKE", "CAST", "COALESCE", "NULLIF",
		"COUNT", "SUM", "AVG", "MIN", "MAX", "NOW", "INTERVAL", "FILTER",
		"WITHIN", "GENERATE", "SERIES", "RETURNING", "CONFLICT", "NOTHING",
		"EXCLUDED", "LATERAL", "WITH", "RECURSIVE", "DISTINCT", "ALL",
		"ANY", "SOME", "ARRAY", "ROW", "ROWS", "UNBOUNDED", "PRECEDING",
		"FOLLOWING", "CURRENT", "PARTITION", "WINDOW", "USING", "DO",
		"PRIMARY", "FOREIGN", "REFERENCES", "CHECK", "UNIQUE", "INDEX",
		"TABLE", "VIEW", "SCHEMA", "DATABASE", "GRANT", "REVOKE",
		"BEGIN", "COMMIT", "ROLLBACK", "TRANSACTION", "ISOLATION",
		"TEXT", "UUID", "INTEGER", "BIGINT", "BOOLEAN", "NUMERIC", "DECIMAL",
		"REAL", "DOUBLE", "PRECISION", "FLOAT", "FLOAT4", "FLOAT8", "INT", "INT2", "INT4", "INT8",
		"SMALLINT", "OID", "MONEY", "XML", "CIDR", "INET", "MACADDR",
		"VARCHAR", "CHAR", "BYTEA", "JSON", "JSONB",
		"TIMESTAMP", "TIMESTAMPTZ", "DATE", "TIME", "SERIAL", "BIGSERIAL",
		"AT", "ZONE", "YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND",
		"TO", "OF", "FOR", "IF", "ELSEIF", "ELSIF", "LOOP", "WHILE",
		"GROUP", "ORDER", "REPLACE", "TRUNCATE", "CASCADE", "RESTRICT",
		"ADD", "DROP", "COLUMN", "CONSTRAINT", "DEFAULT", "KEY", "TYPE",
		"WITHOUT", "ONLY", "FIRST", "LAST", "NULLS", "FETCH", "NEXT",
		"BOTH", "SIMILAR", "ESCAPE", "UNKNOWN", "LOCAL", "SESSION",
		"TEMPORARY", "TEMP", "UNLOGGED", "MATERIALIZED", "CONCURRENTLY":
		return true
	default:
		return false
	}
}
