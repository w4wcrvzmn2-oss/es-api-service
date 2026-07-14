package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

// handleCreateSupplierPrice создает новый прайс поставщика
func (s *Server) handleCreateSupplierPrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	var req struct {
		SupplierID    string   `json:"supplier_id"`
		GUID_ES       string   `json:"guid_es"`
		Price         float64  `json:"price"`
		RegionID      *string  `json:"region_id,omitempty"`
		MarkupPct     *float64 `json:"markup_pct,omitempty"`
		ItemCode      *string  `json:"item_code,omitempty"`
		ItemName      *string  `json:"item_name,omitempty"`
		Barcode       *string  `json:"barcode,omitempty"`
		Quantity      *float64 `json:"quantity,omitempty"`
		InvoiceNumber *string  `json:"invoice_number,omitempty"`
		InvoiceDate   *string  `json:"invoice_date,omitempty"`
		IsActive      bool     `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}
	if req.SupplierID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан supplier_id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	markupPct := 0.0
	if req.MarkupPct != nil {
		markupPct = *req.MarkupPct
	}

	// Используем существующий или создаем новый InvoiceImport
	var invoiceImportID string
	err := s.database.GORMWith(ctx).Raw(
		"SELECT TOP 1 CAST(InvoiceImportID AS NVARCHAR(50)) FROM InvoiceImport WHERE SupplierID = CAST(@supplierID AS UNIQUEIDENTIFIER) ORDER BY CreatedAt DESC",
		sql.Named("supplierID", req.SupplierID),
	).Row().Scan(&invoiceImportID)

	if err != nil || invoiceImportID == "" {
		query := `
			DECLARE @ImportID UNIQUEIDENTIFIER = NEWID();
			INSERT INTO InvoiceImport (InvoiceImportID, SupplierID, FileName, ImportStatus, RecordsTotal, RecordsProcessed, CreatedAt)
			VALUES (@ImportID, CAST(@supplierID AS UNIQUEIDENTIFIER), N'Manual Entry', N'COMPLETED', 0, 0, GETUTCDATE());
			SELECT CAST(@ImportID AS NVARCHAR(50));
		`
		err = s.database.GORMWith(ctx).Raw(query, sql.Named("supplierID", req.SupplierID)).Row().Scan(&invoiceImportID)
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка создания импорта: %v", err)
			}
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания импорта: %v", err))
			return
		}
	}

	// Создаем InvoiceData если нужно
	var invoiceDataID string
	err = s.database.GORMWith(ctx).Raw(
		"SELECT TOP 1 CAST(InvoiceDataID AS NVARCHAR(50)) FROM InvoiceData WHERE InvoiceImportID = CAST(@importID AS UNIQUEIDENTIFIER) ORDER BY CreatedAt DESC",
		sql.Named("importID", invoiceImportID),
	).Row().Scan(&invoiceDataID)

	if err != nil || invoiceDataID == "" {
		itemNameVal := "Manual Entry"
		if req.ItemName != nil {
			itemNameVal = *req.ItemName
		}
		itemCodeVal := ""
		if req.ItemCode != nil {
			itemCodeVal = *req.ItemCode
		}
		quantityVal := 0.0
		if req.Quantity != nil {
			quantityVal = *req.Quantity
		}
		query := `
			DECLARE @DataID UNIQUEIDENTIFIER = NEWID();
			INSERT INTO InvoiceData (InvoiceDataID, InvoiceImportID, ItemCode, ItemName, Price, Quantity, CreatedAt)
			VALUES (@DataID, CAST(@importID AS UNIQUEIDENTIFIER),
				@itemCode, @itemName, @price, @quantity, GETUTCDATE());
			SELECT CAST(@DataID AS NVARCHAR(50));
		`
		err = s.database.GORMWith(ctx).Raw(query,
			sql.Named("importID", invoiceImportID),
			sql.Named("itemCode", itemCodeVal),
			sql.Named("itemName", itemNameVal),
			sql.Named("price", req.Price),
			sql.Named("quantity", quantityVal),
		).Row().Scan(&invoiceDataID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания данных: %v", err))
			return
		}
	}

	// Вставляем прайс и сразу возвращаем его расчётную цену
	query := `
		INSERT INTO SupplierPrice (
			SupplierID, InvoiceImportID, InvoiceDataID, GUID_ES,
			ItemCode, ItemName, Barcode, Price, Quantity,
			InvoiceNumber, InvoiceDate, RegionID, MarkupPct,
			IsActive, CreatedAt, UpdatedAt
		)
		OUTPUT
			CAST(INSERTED.SupplierPriceID AS NVARCHAR(50)),
			INSERTED.Price * (1 + ISNULL(INSERTED.MarkupPct, 0) / 100.0)
		VALUES (
			CAST(@supplierID AS UNIQUEIDENTIFIER),
			CAST(@invoiceImportID AS UNIQUEIDENTIFIER),
			CAST(@invoiceDataID AS UNIQUEIDENTIFIER),
			CASE WHEN @guidES IS NOT NULL AND @guidES != '' THEN CAST(@guidES AS UNIQUEIDENTIFIER) ELSE NULL END,
			@itemCode, @itemName, @barcode, @price, @quantity,
			@invoiceNumber, @invoiceDate,
			CASE WHEN @regionID IS NOT NULL AND @regionID != '' THEN CAST(@regionID AS UNIQUEIDENTIFIER) ELSE NULL END,
			@markupPct, @isActive,
			GETUTCDATE(), GETUTCDATE()
		);
	`

	var priceID string
	var finalPrice float64

	var invoiceDateVal *time.Time
	if req.InvoiceDate != nil && *req.InvoiceDate != "" {
		if parsed, err := time.Parse(time.RFC3339, *req.InvoiceDate); err == nil {
			invoiceDateVal = &parsed
		}
	}

	var guidESVal interface{}
	if req.GUID_ES != "" {
		guidESVal = req.GUID_ES
	} else {
		guidESVal = nil
	}

	err = s.database.GORMWith(ctx).Raw(query,
		sql.Named("supplierID", req.SupplierID),
		sql.Named("invoiceImportID", invoiceImportID),
		sql.Named("invoiceDataID", invoiceDataID),
		sql.Named("guidES", guidESVal),
		sql.Named("itemCode", getStringPtr(req.ItemCode)),
		sql.Named("itemName", getStringPtr(req.ItemName)),
		sql.Named("barcode", getStringPtr(req.Barcode)),
		sql.Named("price", req.Price),
		sql.Named("quantity", getFloatPtr(req.Quantity)),
		sql.Named("invoiceNumber", getStringPtr(req.InvoiceNumber)),
		sql.Named("invoiceDate", invoiceDateVal),
		sql.Named("regionID", getStringPtr(req.RegionID)),
		sql.Named("markupPct", markupPct),
		sql.Named("isActive", req.IsActive),
	).Row().Scan(&priceID, &finalPrice)

	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания прайса: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания прайса: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"supplier_price_id": priceID,
		"final_price":       finalPrice,
		"message":           "Прайс успешно создан",
	})
}

// handleUpdateSupplierPrice обновляет существующий прайс
func (s *Server) handleUpdateSupplierPrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	var priceID string
	for i, part := range pathParts {
		if part == "supplier-prices" && i+1 < len(pathParts) {
			priceID = pathParts[i+1]
			break
		}
	}
	if priceID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}

	var req struct {
		Price     *float64 `json:"price,omitempty"`
		RegionID  *string  `json:"region_id,omitempty"`
		MarkupPct *float64 `json:"markup_pct,omitempty"`
		IsActive  *bool    `json:"is_active,omitempty"`
		Quantity  *float64 `json:"quantity,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга запроса: %v", err))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var updates []string
	var args []interface{}
	args = append(args, sql.Named("priceID", priceID))

	if req.Price != nil {
		updates = append(updates, "Price = @price")
		args = append(args, sql.Named("price", *req.Price))
	}
	if req.RegionID != nil {
		if *req.RegionID == "" {
			updates = append(updates, "RegionID = NULL")
		} else {
			updates = append(updates, "RegionID = CAST(@regionID AS UNIQUEIDENTIFIER)")
			args = append(args, sql.Named("regionID", *req.RegionID))
		}
	}
	if req.MarkupPct != nil {
		updates = append(updates, "MarkupPct = @markupPct")
		args = append(args, sql.Named("markupPct", *req.MarkupPct))
	}
	if req.IsActive != nil {
		updates = append(updates, "IsActive = @isActive")
		args = append(args, sql.Named("isActive", *req.IsActive))
	}
	if req.Quantity != nil {
		updates = append(updates, "Quantity = @quantity")
		args = append(args, sql.Named("quantity", *req.Quantity))
	}
	if len(updates) == 0 {
		s.writeError(w, http.StatusBadRequest, "Не указаны поля для обновления")
		return
	}
	updates = append(updates, "UpdatedAt = GETUTCDATE()")

	query := fmt.Sprintf(`
		UPDATE SupplierPrice
		SET %s
		WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER);

		SELECT
			CAST(SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
			Price,
			Price * (1 + ISNULL(MarkupPct, 0) / 100.0) AS FinalPrice,
			ISNULL(MarkupPct, 0) AS MarkupPct,
			CAST(RegionID AS NVARCHAR(50)) AS RegionID,
			IsActive
		FROM SupplierPrice
		WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER);
	`, strings.Join(updates, ", "))

	var result struct {
		PriceID    string
		Price      float64
		FinalPrice float64
		MarkupPct  float64
		RegionID   sql.NullString
		IsActive   bool
	}
	err := s.database.GORMWith(ctx).Raw(query, args...).Row().Scan(
		&result.PriceID,
		&result.Price,
		&result.FinalPrice,
		&result.MarkupPct,
		&result.RegionID,
		&result.IsActive,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusNotFound, "Прайс не найден")
			return
		}
		if s.logger != nil {
			s.logger.Error("Ошибка обновления прайса: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка обновления прайса: %v", err))
		return
	}

	response := map[string]interface{}{
		"supplier_price_id": result.PriceID,
		"price":             result.Price,
		"final_price":       result.FinalPrice,
		"markup_pct":        result.MarkupPct,
		"is_active":         result.IsActive,
		"message":           "Прайс успешно обновлен",
	}
	if result.RegionID.Valid {
		response["region_id"] = result.RegionID.String
	} else {
		response["region_id"] = nil
	}
	s.writeJSON(w, http.StatusOK, response)
}

// handleDeleteSupplierPrice мягко удаляет (деактивирует) прайс
func (s *Server) handleDeleteSupplierPrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	pathParts := strings.Split(r.URL.Path, "/")
	var priceID string
	for i, part := range pathParts {
		if part == "supplier-prices" && i+1 < len(pathParts) {
			priceID = pathParts[i+1]
			break
		}
	}
	if priceID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := s.database.GORMWith(ctx).Exec(`
		UPDATE SupplierPrice
		SET IsActive = 0, UpdatedAt = GETUTCDATE()
		WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)`,
		sql.Named("priceID", priceID),
	)
	if res.Error != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка удаления прайса: %v", res.Error)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка удаления прайса: %v", res.Error))
		return
	}
	if res.RowsAffected == 0 {
		s.writeError(w, http.StatusNotFound, "Прайс не найден")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":           "Прайс успешно удален (деактивирован)",
		"supplier_price_id": priceID,
	})
}

// handleToggleSupplierPrice включает/выключает прайс
func (s *Server) handleToggleSupplierPrice(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	var priceID string
	for i, part := range pathParts {
		if part == "supplier-prices" && i+1 < len(pathParts) && i+2 < len(pathParts) && pathParts[i+2] == "toggle" {
			priceID = pathParts[i+1]
			break
		}
	}
	if priceID == "" {
		s.writeError(w, http.StatusBadRequest, "Не указан ID прайса")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var isActive bool
	err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			UPDATE SupplierPrice
			SET IsActive = CASE WHEN IsActive = 1 THEN 0 ELSE 1 END,
			    UpdatedAt = GETUTCDATE()
			WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)`,
			sql.Named("priceID", priceID),
		).Error; err != nil {
			return err
		}
		return tx.Raw(
			`SELECT IsActive FROM SupplierPrice WHERE SupplierPriceID = CAST(@priceID AS UNIQUEIDENTIFIER)`,
			sql.Named("priceID", priceID),
		).Row().Scan(&isActive)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.writeError(w, http.StatusNotFound, "Прайс не найден")
			return
		}
		if s.logger != nil {
			s.logger.Error("Ошибка переключения прайса: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка переключения прайса: %v", err))
		return
	}

	state := "выключен"
	if isActive {
		state = "включен"
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"supplier_price_id": priceID,
		"is_active":         isActive,
		"message":           fmt.Sprintf("Прайс %s", state),
	})
}

// Вспомогательные функции
func getStringPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func getFloatPtr(f *float64) interface{} {
	if f == nil {
		return nil
	}
	return *f
}
