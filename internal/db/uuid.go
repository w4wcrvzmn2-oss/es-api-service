package db

// UUIDParam возвращает строку GUID для параметра PostgreSQL uuid.
func UUIDParam(s string) string {
	return s
}

// UUIDParamPtr возвращает *string для optional UUID: nil если пусто.
func UUIDParamPtr(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	v := *s
	return &v
}
