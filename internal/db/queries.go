package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// QueryParams представляет параметры запроса
type QueryParams struct {
	Limit        int
	Offset       int
	UpdatedAfter *time.Time
	IDGt         *int64
	Columns      []string
}

// ParseQueryParams парсит параметры запроса из строковых значений
func ParseQueryParams(limitStr, offsetStr, updatedAfterStr, idGtStr, columnsStr string) (*QueryParams, error) {
	params := &QueryParams{
		Limit:  1000,
		Offset: 0,
	}

	if limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return nil, fmt.Errorf("неверный параметр limit: %w", err)
		}
		if limit < 1 || limit > 100000 {
			return nil, fmt.Errorf("limit должен быть от 1 до 100000")
		}
		params.Limit = limit
	}

	if offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return nil, fmt.Errorf("неверный параметр offset: %w", err)
		}
		if offset < 0 {
			return nil, fmt.Errorf("offset не может быть отрицательным")
		}
		params.Offset = offset
	}

	if updatedAfterStr != "" {
		updatedAfter, err := time.Parse(time.RFC3339, updatedAfterStr)
		if err != nil {
			return nil, fmt.Errorf("неверный формат updated_after (ожидается RFC3339): %w", err)
		}
		params.UpdatedAfter = &updatedAfter
	}

	if idGtStr != "" {
		idGt, err := strconv.ParseInt(idGtStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("неверный параметр id_gt: %w", err)
		}
		params.IDGt = &idGt
	}

	if columnsStr != "" {
		columns := strings.Split(columnsStr, ",")
		for i, col := range columns {
			columns[i] = strings.TrimSpace(col)
		}
		params.Columns = columns
	}

	return params, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// BuildSelectQuery строит SQL запрос (PostgreSQL).
func BuildSelectQuery(tableName string, params *QueryParams, tableColumns []string) (string, []interface{}, error) {
	var query strings.Builder
	var args []interface{}

	query.WriteString("SELECT ")
	if len(params.Columns) > 0 {
		columnMap := make(map[string]bool)
		for _, col := range tableColumns {
			columnMap[strings.ToLower(col)] = true
		}
		validColumns := []string{}
		for _, col := range params.Columns {
			if col == "*" {
				validColumns = []string{"*"}
				break
			}
			if columnMap[strings.ToLower(col)] {
				validColumns = append(validColumns, quoteIdent(col))
			} else {
				return "", nil, fmt.Errorf("колонка '%s' не существует в таблице %s", col, tableName)
			}
		}
		query.WriteString(strings.Join(validColumns, ", "))
	} else {
		query.WriteString("*")
	}

	query.WriteString(fmt.Sprintf(" FROM %s", quoteIdent(tableName)))

	var whereConditions []string
	argIndex := 1

	if params.UpdatedAfter != nil {
		for _, col := range tableColumns {
			if strings.ToLower(col) == "updated_at" {
				whereConditions = append(whereConditions, fmt.Sprintf("%s > $%d", quoteIdent("updated_at"), argIndex))
				args = append(args, *params.UpdatedAfter)
				argIndex++
				break
			}
		}
	}

	if params.IDGt != nil {
		pkColumn := ""
		for _, col := range tableColumns {
			colLower := strings.ToLower(col)
			if colLower == "id" || strings.HasSuffix(colLower, "_id") {
				pkColumn = col
				break
			}
		}
		if pkColumn != "" {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > $%d", quoteIdent(pkColumn), argIndex))
			args = append(args, *params.IDGt)
			argIndex++
		}
	}

	if len(whereConditions) > 0 {
		query.WriteString(" WHERE " + strings.Join(whereConditions, " AND "))
	}

	pkColumn := ""
	for _, col := range tableColumns {
		colLower := strings.ToLower(col)
		if colLower == "id" || strings.HasSuffix(colLower, "_id") {
			pkColumn = col
			break
		}
	}
	if pkColumn == "" && len(tableColumns) > 0 {
		pkColumn = tableColumns[0]
	}
	if pkColumn == "" {
		return "", nil, fmt.Errorf("не удалось найти колонку для сортировки в таблице %s", tableName)
	}

	query.WriteString(fmt.Sprintf(" ORDER BY %s OFFSET %d LIMIT %d", quoteIdent(pkColumn), params.Offset, params.Limit))
	return query.String(), args, nil
}

// BuildCountQuery строит запрос COUNT (PostgreSQL).
func BuildCountQuery(tableName string, params *QueryParams, tableColumns []string) (string, []interface{}, error) {
	var query strings.Builder
	var args []interface{}
	argIndex := 1

	query.WriteString(fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(tableName)))

	var whereConditions []string

	if params.UpdatedAfter != nil {
		for _, col := range tableColumns {
			if strings.ToLower(col) == "updated_at" {
				whereConditions = append(whereConditions, fmt.Sprintf("%s > $%d", quoteIdent("updated_at"), argIndex))
				args = append(args, *params.UpdatedAfter)
				argIndex++
				break
			}
		}
	}

	if params.IDGt != nil {
		pkColumn := ""
		for _, col := range tableColumns {
			colLower := strings.ToLower(col)
			if colLower == "id" || strings.HasSuffix(colLower, "_id") {
				pkColumn = col
				break
			}
		}
		if pkColumn != "" {
			whereConditions = append(whereConditions, fmt.Sprintf("%s > $%d", quoteIdent(pkColumn), argIndex))
			args = append(args, *params.IDGt)
			argIndex++
		}
	}

	if len(whereConditions) > 0 {
		query.WriteString(" WHERE " + strings.Join(whereConditions, " AND "))
	}

	return query.String(), args, nil
}
