package models

import (
	"database/sql"
	"time"
)

// Supplier представляет поставщика
type Supplier struct {
	SupplierID     string    `json:"supplier_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	Name           string    `json:"name"`
	Address        *string   `json:"address,omitempty"`
	Contacts       *string   `json:"contacts,omitempty"`
	INN            *string   `json:"inn,omitempty"`
	ContractNumber *string   `json:"contract_number,omitempty"`
	Login          *string   `json:"login,omitempty"`
	Password       *string   `json:"password,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:GETUTCDATE()"`
}

// ImportPoint представляет точку импорта (настройки источника прайса)
type ImportPoint struct {
	ImportPointID  string    `json:"import_point_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	SupplierID     *string   `json:"supplier_id,omitempty" gorm:"type:uniqueidentifier"`
	SupplierName   *string   `json:"supplier_name,omitempty" gorm:"-"`
	Name           string    `json:"name"`
	Description    *string   `json:"description,omitempty"`
	SourceType     string    `json:"source_type"`
	DBFFilePath    *string   `json:"dbf_file_path,omitempty"`
	SourceFilePath *string   `json:"source_file_path,omitempty"`
	FtpHost        *string   `json:"ftp_host,omitempty"`
	FtpPort        *int      `json:"ftp_port,omitempty"`
	FtpUser        *string   `json:"ftp_user,omitempty"`
	FtpPassword    *string   `json:"ftp_password,omitempty"`
	FtpRemotePath  *string   `json:"ftp_remote_path,omitempty"`
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:GETUTCDATE()"`
}

// DBFFieldMapping представляет маппинг поля DBF на поле базы данных
type DBFFieldMapping struct {
	MappingID       string    `json:"mapping_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	ImportPointID   string    `json:"import_point_id" gorm:"type:uniqueidentifier"`
	DBFFieldName    string    `json:"dbf_field_name"`
	TargetFieldName string    `json:"target_field_name"`
	DataType        string    `json:"data_type"`
	IsRequired      bool      `json:"is_required"`
	DefaultValue    *string   `json:"default_value,omitempty"`
	TransformRule   *string   `json:"transform_rule,omitempty"`
	DisplayOrder    int       `json:"display_order"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:GETUTCDATE()"`
}

// InvoiceImport представляет историю импорта накладной
type InvoiceImport struct {
	InvoiceImportID  string     `json:"invoice_import_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	ImportPointID    string     `json:"import_point_id" gorm:"type:uniqueidentifier"`
	FileName         string     `json:"file_name"`
	FilePath         string     `json:"file_path"`
	FileSize         *int64     `json:"file_size,omitempty"`
	RecordsTotal     int        `json:"records_total"`
	RecordsProcessed int        `json:"records_processed"`
	RecordsSkipped   int        `json:"records_skipped"`
	RecordsError     int        `json:"records_error"`
	ImportStatus     string     `json:"import_status"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	CreatedBy        *string    `json:"created_by,omitempty"`
}

// InvoiceData представляет импортированные данные накладной
type InvoiceData struct {
	InvoiceDataID   string     `json:"invoice_data_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	InvoiceImportID string     `json:"invoice_import_id" gorm:"type:uniqueidentifier"`
	SupplierID      string     `json:"supplier_id" gorm:"type:uniqueidentifier"`
	InvoiceNumber   *string    `json:"invoice_number,omitempty"`
	InvoiceDate     *time.Time `json:"invoice_date,omitempty"`
	ItemCode        *string    `json:"item_code,omitempty"`
	ItemName        *string    `json:"item_name,omitempty"`
	Quantity        *float64   `json:"quantity,omitempty"`
	Price           *float64   `json:"price,omitempty"`
	BatchNumber     *string    `json:"batch_number,omitempty"`
	ExpiryDate      *time.Time `json:"expiry_date,omitempty"`
	Manufacturer    *string    `json:"manufacturer,omitempty"`
	Country         *string    `json:"country,omitempty"`
	Barcode         *string    `json:"barcode,omitempty"`
	RawData         *string    `json:"raw_data,omitempty"`
	IsProcessed     bool       `json:"is_processed"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	ErrorMessage    *string    `json:"error_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
}

// SupplierRequest представляет запрос на создание/обновление поставщика
type SupplierRequest struct {
	Name           string   `json:"name"`
	Address        *string  `json:"address,omitempty"`
	Contacts       *string  `json:"contacts,omitempty"`
	INN            *string  `json:"inn,omitempty"`
	ContractNumber *string  `json:"contract_number,omitempty"`
	Login          *string  `json:"login,omitempty"`
	Password       *string  `json:"password,omitempty"`
	IsActive       bool     `json:"is_active"`
	RegionIDs      []string `json:"region_ids,omitempty"`
}

// ImportPointRequest представляет запрос на создание/обновление точки импорта
type ImportPointRequest struct {
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
	SourceType     string  `json:"source_type"`
	SourceFilePath *string `json:"source_file_path,omitempty"`
	FtpHost        *string `json:"ftp_host,omitempty"`
	FtpPort        *int    `json:"ftp_port,omitempty"`
	FtpUser        *string `json:"ftp_user,omitempty"`
	FtpPassword    *string `json:"ftp_password,omitempty"`
	FtpRemotePath  *string `json:"ftp_remote_path,omitempty"`
	IsActive       bool    `json:"is_active"`
}

// DBFFieldMappingRequest представляет запрос на создание/обновление маппинга
type DBFFieldMappingRequest struct {
	DBFFieldName   string  `json:"dbf_field_name"`
	TargetFieldName string `json:"target_field_name"`
	DataType       string  `json:"data_type"`
	IsRequired     bool    `json:"is_required"`
	DefaultValue   *string `json:"default_value,omitempty"`
	TransformRule  *string `json:"transform_rule,omitempty"`
	DisplayOrder   int     `json:"display_order"`
}

// ImportFileRequest представляет запрос на импорт файла
type ImportFileRequest struct {
	ImportPointID string `json:"import_point_id"`
	FilePath      string `json:"file_path"`
}

// ===============================
// МОДЕЛИ ДЛЯ ПОКУПАТЕЛЕЙ
// ===============================

// Buyer представляет покупателя
type Buyer struct {
	BuyerID   string    `json:"buyer_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	Name      string    `json:"name"`
	INN       *string   `json:"inn,omitempty"`
	RegionID  *string   `json:"region_id,omitempty" gorm:"type:uniqueidentifier"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
}

// BuyerLocation представляет адрес покупателя
type BuyerLocation struct {
	BuyerLocationID string    `json:"buyer_location_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	BuyerID         string    `json:"buyer_id" gorm:"type:uniqueidentifier"`
	Address         string    `json:"address"`
	RegionID        *string   `json:"region_id,omitempty" gorm:"type:uniqueidentifier"`
	IsDefault       bool      `json:"is_default"`
	CreatedAt       time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
}

// BuyerUser представляет пользователя покупателя
type BuyerUser struct {
	BuyerUserID string    `json:"buyer_user_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	BuyerID     string    `json:"buyer_id" gorm:"type:uniqueidentifier"`
	FullName    string    `json:"full_name"`
	Email       string    `json:"email"`
	Phone       *string   `json:"phone,omitempty"`
	Role        *string   `json:"role,omitempty"`
	IsActive    bool      `json:"is_active"`
	Password    *string   `json:"-"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
}

// BuyerRequest представляет запрос на создание/обновление покупателя
type BuyerRequest struct {
	Name     string  `json:"name"`
	INN      *string `json:"inn,omitempty"`
	RegionID *string `json:"region_id,omitempty"`
	IsActive bool    `json:"is_active"`
}

// BuyerUserRequest представляет запрос на создание пользователя покупателя
type BuyerUserRequest struct {
	BuyerID  string  `json:"buyer_id"`
	FullName string  `json:"full_name"`
	Email    string  `json:"email"`
	Phone    *string `json:"phone,omitempty"`
	Role     *string `json:"role,omitempty"`
	IsActive bool    `json:"is_active"`
	Password *string `json:"password,omitempty"`
}

// BuyerLocationRequest представляет запрос на создание адреса покупателя
type BuyerLocationRequest struct {
	BuyerID   string  `json:"buyer_id"`
	Address   string  `json:"address"`
	RegionID  *string `json:"region_id,omitempty"`
	IsDefault bool    `json:"is_default"`
}

// ===============================
// МОДЕЛИ ДЛЯ ЗАКАЗОВ
// ===============================

// OrderStatus представляет статус заказа
type OrderStatus struct {
	OrderStatusID string  `json:"order_status_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	IsActive      bool    `json:"is_active"`
}

// Order представляет заказ.
// Таблица называется [Order] — зарезервированное слово MS SQL, GORM-driver sqlserver
// квотирует его автоматически через TableName().
type Order struct {
	OrderID            string     `json:"order_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	BuyerUserID        string     `json:"buyer_user_id" gorm:"type:uniqueidentifier"`
	BuyerApplicationID string     `json:"buyer_application_id" gorm:"type:uniqueidentifier"`
	BuyerLocationID    *string    `json:"buyer_location_id,omitempty" gorm:"type:uniqueidentifier"`
	OrderStatusID      string     `json:"order_status_id" gorm:"type:uniqueidentifier"`
	CreatedAt          time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	PlacedAt           *time.Time `json:"placed_at,omitempty"`
	TotalAmount        *float64   `json:"total_amount,omitempty"`
	Comment            *string    `json:"comment,omitempty"`
	// Расширенные поля для отображения — не в БД.
	BuyerUserName   *string `json:"buyer_user_name,omitempty" gorm:"-"`
	BuyerName       *string `json:"buyer_name,omitempty" gorm:"-"`
	OrderStatusName *string `json:"order_status_name,omitempty" gorm:"-"`
	LocationAddress *string `json:"location_address,omitempty" gorm:"-"`
}

// TableName переопределяет имя таблицы — Order зарезервировано в T-SQL.
// Возвращаем без скобок: GORM добавит NamingStrategy-кавычки сам,
// а в местах вставки/обновления используем .Table("[Order]") явно,
// чтобы MSSQL получил квадратные скобки и распознал зарезервированное имя.
func (Order) TableName() string { return "Order" }

// OrderItem представляет позицию заказа.
// После миграции alter_order_item_relax_nullability.sql:
// SupplierItemID/ProductID/RegionID допускают NULL.
type OrderItem struct {
	OrderLineID    string    `json:"order_line_id" gorm:"primaryKey;type:uniqueidentifier;default:NEWID()"`
	OrderID        string    `json:"order_id" gorm:"type:uniqueidentifier"`
	SupplierID     string    `json:"supplier_id" gorm:"type:uniqueidentifier"`
	SupplierItemID *string   `json:"supplier_item_id,omitempty" gorm:"type:uniqueidentifier"`
	ProductID      *string   `json:"product_id,omitempty" gorm:"type:uniqueidentifier"`
	RegionID       *string   `json:"region_id,omitempty" gorm:"type:uniqueidentifier"`
	Qty            float64   `json:"qty"`
	UnitPrice      float64   `json:"unit_price"`
	PriceListID    *string   `json:"price_list_id,omitempty" gorm:"type:uniqueidentifier"`
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime:false;default:GETUTCDATE()"`
	// Расширенные поля для отображения — не в БД.
	SupplierName     *string `json:"supplier_name,omitempty" gorm:"-"`
	SupplierItemName *string `json:"supplier_item_name,omitempty" gorm:"-"`
	ProductName      *string `json:"product_name,omitempty" gorm:"-"`
}

// OrderRequest представляет запрос на создание заказа
type OrderRequest struct {
	BuyerUserID        string             `json:"buyer_user_id"`
	BuyerApplicationID string             `json:"buyer_application_id"`
	BuyerLocationID    *string            `json:"buyer_location_id,omitempty"`
	Comment            *string            `json:"comment,omitempty"`
	Items              []OrderItemRequest `json:"items"`
}

// OrderItemRequest представляет запрос на добавление позиции в заказ
type OrderItemRequest struct {
	SupplierID     string   `json:"supplier_id"`
	SupplierItemID string   `json:"supplier_item_id"`
	ProductID      string   `json:"product_id"`
	RegionID       string   `json:"region_id"`
	Qty            float64  `json:"qty"`
	UnitPrice      float64  `json:"unit_price"`
	PriceListID    *string  `json:"price_list_id,omitempty"`
}

// OrderUpdateRequest представляет запрос на обновление заказа
type OrderUpdateRequest struct {
	OrderStatusID   *string  `json:"order_status_id,omitempty"`
	BuyerLocationID *string  `json:"buyer_location_id,omitempty"`
	Comment         *string  `json:"comment,omitempty"`
}

// OrderWithItems представляет заказ с позициями
type OrderWithItems struct {
	Order
	Items []OrderItem `json:"items"`
}

// ScanSupplier сканирует Supplier из результата запроса
func ScanSupplier(rows *sql.Rows) (*Supplier, error) {
	var s Supplier
	var address, contacts, inn sql.NullString
	var createdAt, updatedAt time.Time
	
	err := rows.Scan(
		&s.SupplierID,
		&s.Name,
		&address,
		&contacts,
		&inn,
		&s.IsActive,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	if address.Valid {
		s.Address = &address.String
	}
	if contacts.Valid {
		s.Contacts = &contacts.String
	}
	if inn.Valid {
		s.INN = &inn.String
	}
	s.CreatedAt = createdAt
	s.UpdatedAt = updatedAt
	
	return &s, nil
}

