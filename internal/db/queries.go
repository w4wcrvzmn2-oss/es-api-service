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
		Limit:  1000, // значение по умолчанию
		Offset: 0,
	}

	// Парсинг limit
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

	// Парсинг offset
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

	// Парсинг updated_after
	if updatedAfterStr != "" {
		updatedAfter, err := time.Parse(time.RFC3339, updatedAfterStr)
		if err != nil {
			return nil, fmt.Errorf("неверный формат updated_after (ожидается RFC3339): %w", err)
		}
		params.UpdatedAfter = &updatedAfter
	}

	// Парсинг id_gt
	if idGtStr != "" {
		idGt, err := strconv.ParseInt(idGtStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("неверный параметр id_gt: %w", err)
		}
		params.IDGt = &idGt
	}

	// Парсинг columns
	if columnsStr != "" {
		columns := strings.Split(columnsStr, ",")
		for i, col := range columns {
			columns[i] = strings.TrimSpace(col)
		}
		params.Columns = columns
	}

	return params, nil
}

// BuildSelectQuery строит SQL запрос с учётом параметров
func BuildSelectQuery(tableName string, params *QueryParams, tableColumns []string) (string, []interface{}, error) {
	var query strings.Builder
	var args []interface{}
	argIndex := 1

	// SELECT часть
	query.WriteString("SELECT ")
	if len(params.Columns) > 0 {
		// Проверяем, что все запрашиваемые колонки существуют
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
				validColumns = append(validColumns, fmt.Sprintf("[%s]", col))
			} else {
				return "", nil, fmt.Errorf("колонка '%s' не существует в таблице %s", col, tableName)
			}
		}
		query.WriteString(strings.Join(validColumns, ", "))
	} else {
		query.WriteString("*")
	}

	// FROM часть
	query.WriteString(fmt.Sprintf(" FROM [%s]", tableName))

	// WHERE часть
	var whereConditions []string

	if params.UpdatedAfter != nil {
		// Проверяем наличие колонки updated_at
		hasUpdatedAt := false
		for _, col := range tableColumns {
			if strings.ToLower(col) == "updated_at" {
				hasUpdatedAt = true
				break
			}
		}

		if hasUpdatedAt {
			whereConditions = append(whereConditions, fmt.Sprintf("[updated_at] > @p%d", argIndex))
			args = append(args, *params.UpdatedAfter)
			argIndex++
		}
	}

	if params.IDGt != nil {
		// Ищем первичный ключ (обычно это id)
		hasPK := false
		pkColumn := ""
		for _, col := range tableColumns {
			colLower := strings.ToLower(col)
			if colLower == "id" || strings.HasSuffix(colLower, "_id") {
				hasPK = true
				pkColumn = col
				break
			}
		}

		if hasPK {
			whereConditions = append(whereConditions, fmt.Sprintf("[%s] > @p%d", pkColumn, argIndex))
			args = append(args, *params.IDGt)
			argIndex++
		}
	}

	if len(whereConditions) > 0 {
		query.WriteString(" WHERE " + strings.Join(whereConditions, " AND "))
	}

	// ORDER BY часть (обязательно для OFFSET/FETCH в SQL Server)
	// Ищем первичный ключ для сортировки
	pkColumn := ""
	for _, col := range tableColumns {
		colLower := strings.ToLower(col)
		if colLower == "id" || strings.HasSuffix(colLower, "_id") {
			pkColumn = col
			break
		}
	}

	// Если не нашли ID, используем первую колонку
	if pkColumn == "" && len(tableColumns) > 0 {
		pkColumn = tableColumns[0]
	}

	// ORDER BY обязателен для OFFSET/FETCH
	if pkColumn != "" {
		query.WriteString(fmt.Sprintf(" ORDER BY [%s]", pkColumn))
		// OFFSET и FETCH (SQL Server 2012+)
		query.WriteString(fmt.Sprintf(" OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", params.Offset, params.Limit))
	} else {
		// Fallback для старых версий SQL Server или если нет колонок для сортировки
		// Используем TOP вместо OFFSET/FETCH
		return "", nil, fmt.Errorf("не удалось найти колонку для сортировки в таблице %s", tableName)
	}

	return query.String(), args, nil
}

// BuildCountQuery строит запрос для подсчёта общего количества записей
func BuildCountQuery(tableName string, params *QueryParams, tableColumns []string) (string, []interface{}, error) {
	var query strings.Builder
	var args []interface{}
	argIndex := 1

	query.WriteString(fmt.Sprintf("SELECT COUNT(*) FROM [%s]", tableName))

	// WHERE часть (та же логика что и в BuildSelectQuery)
	var whereConditions []string

	if params.UpdatedAfter != nil {
		hasUpdatedAt := false
		for _, col := range tableColumns {
			if strings.ToLower(col) == "updated_at" {
				hasUpdatedAt = true
				break
			}
		}

		if hasUpdatedAt {
			whereConditions = append(whereConditions, fmt.Sprintf("[updated_at] > @p%d", argIndex))
			args = append(args, *params.UpdatedAfter)
			argIndex++
		}
	}

	if params.IDGt != nil {
		hasPK := false
		pkColumn := ""
		for _, col := range tableColumns {
			colLower := strings.ToLower(col)
			if colLower == "id" || strings.HasSuffix(colLower, "_id") {
				hasPK = true
				pkColumn = col
				break
			}
		}

		if hasPK {
			whereConditions = append(whereConditions, fmt.Sprintf("[%s] > @p%d", pkColumn, argIndex))
			args = append(args, *params.IDGt)
			argIndex++
		}
	}

	if len(whereConditions) > 0 {
		query.WriteString(" WHERE " + strings.Join(whereConditions, " AND "))
	}

	return query.String(), args, nil
}
