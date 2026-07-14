package db

import (
	mssqldb "github.com/microsoft/go-mssqldb"
)

// UUIDParam оборачивает строку GUID в mssqldb.UniqueIdentifier,
// чтобы GORM/драйвер передал её в MSSQL как uniqueidentifier, а не nvarchar.
// Без этого MSSQL ругается «Ошибка при преобразовании строки символов в тип uniqueidentifier»
// при использовании именованных параметров через `?` в GORM-запросах.
//
// Возвращает nil-эквивалент (UUID v0) если строка пустая — но WHERE не должен
// передавать пустую UUID, защита от случайностей.
func UUIDParam(s string) mssqldb.UniqueIdentifier {
	var u mssqldb.UniqueIdentifier
	if s == "" {
		return u
	}
	_ = u.Scan(s)
	return u
}

// UUIDParamPtr возвращает *mssqldb.UniqueIdentifier для optional GUID:
// nil если строка пустая или nil, иначе указатель на UUID-параметр.
func UUIDParamPtr(s *string) *mssqldb.UniqueIdentifier {
	if s == nil || *s == "" {
		return nil
	}
	u := UUIDParam(*s)
	return &u
}
