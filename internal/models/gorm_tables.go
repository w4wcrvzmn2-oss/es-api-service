package models

import "time"

// Этот файл — GORM-модели для таблиц, для которых ранее не было Go-структур.
// Соответствия колонкам берутся из реальной БД (сверено через sys.columns 2026-05-19).
// NamingStrategy в db.wrapGorm: NoLowerCase + SingularTable —
// поэтому имена полей PascalCase автоматически биндятся на одноимённые колонки.

// =====================================================================
// Region / Product — справочники
// =====================================================================

// Region — справочник регионов.
type Region struct {
	RegionID  string  `json:"region_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Name      string  `json:"name"`
	Code      *string `json:"code,omitempty"`
	IsActive  bool    `json:"is_active"`
}

// Product — эталонный товарный справочник (drugs).
type Product struct {
	ProductID   string    `json:"product_id" gorm:"primaryKey;type:uuid"`
	Name        string    `json:"name"`
	INN         *string   `json:"inn,omitempty"`
	Form        *string   `json:"form,omitempty"`
	Dosage      *string   `json:"dosage,omitempty"`
	PackSize    *string   `json:"pack_size,omitempty"`
	Description *string   `json:"description,omitempty"`
	RxRequired  *bool     `json:"rx_required,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

// =====================================================================
// Поставщики — справочники регионов, прайс-листов, итемов, маппинги
// =====================================================================

// SupplierRegion — связь поставщика с регионом обслуживания (M:N через таблицу).
type SupplierRegion struct {
	SupplierRegionID string    `json:"supplier_region_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID       string    `json:"supplier_id" gorm:"type:uuid"`
	RegionID         string    `json:"region_id" gorm:"type:uuid"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

// SupplierMarkupPolicy — политика наценок поставщика.
type SupplierMarkupPolicy struct {
	PolicyID      string     `json:"policy_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID    string     `json:"supplier_id" gorm:"type:uuid"`
	RegionID      *string    `json:"region_id,omitempty" gorm:"type:uuid"`
	ProductID     *string    `json:"product_id,omitempty" gorm:"type:uuid"`
	MarkupPct     float64    `json:"markup_pct"`
	RoundingStep  *float64   `json:"rounding_step,omitempty"`
	IsActive      bool       `json:"is_active"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

// SupplierItem — каноническая позиция поставщика.
// В текущей БД таблица пустая (после денормализации SupplierPrice).
// Оставлено для совместимости и будущего использования.
type SupplierItem struct {
	SupplierItemID string     `json:"supplier_item_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID     string     `json:"supplier_id" gorm:"type:uuid"`
	ExternalSKU    string     `json:"external_sku"`
	Name           string     `json:"name"`
	Dosage         *string    `json:"dosage,omitempty"`
	Form           *string    `json:"form,omitempty"`
	PackSize       *string    `json:"pack_size,omitempty"`
	Barcode        *string    `json:"barcode,omitempty"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

// SupplierItemMapping — кэш «как этот ItemCode у этого поставщика мапился на Product.ProductID».
type SupplierItemMapping struct {
	MappingID       string     `json:"mapping_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID      string     `json:"supplier_id" gorm:"type:uuid"`
	ItemCode        string     `json:"item_code"`
	GUIDES          string     `json:"guid_es" gorm:"column:GUID_ES;type:uuid"`
	MatchMethod     *string    `json:"match_method,omitempty"`
	MatchConfidence *float64   `json:"match_confidence,omitempty"`
	UseCount        int        `json:"use_count"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"autoUpdateTime:false;default:(NOW() AT TIME ZONE 'utc')"`
}

// =====================================================================
// Прайс-листы
// =====================================================================

// SupplierPriceList — заголовок прайс-листа.
type SupplierPriceList struct {
	PriceListID    string     `json:"price_list_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID     string     `json:"supplier_id" gorm:"type:uuid"`
	PriceFeedRunID *string    `json:"price_feed_run_id,omitempty" gorm:"type:uuid"`
	VersionLabel   *string    `json:"version_label,omitempty"`
	ValidFrom      *time.Time `json:"valid_from,omitempty"`
	ValidTo        *time.Time `json:"valid_to,omitempty"`
	IsActive       bool       `json:"is_active"`
	CreatedAt      time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

// PriceListRegion — наценка прайс-листа для региона.
type PriceListRegion struct {
	PriceListRegionID string  `json:"price_list_region_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PriceListID       string  `json:"price_list_id" gorm:"type:uuid"`
	RegionID          string  `json:"region_id" gorm:"type:uuid"`
	MarkupPct         float64 `json:"markup_pct"`
	IsActive          bool    `json:"is_active"`
}

// SupplierPrice — денормализованная строка прайса.
// Связь с SupplierItem сейчас не используется (см. MD/15);
// GUID_ES — ссылка на Product.ProductID (legacy-имя сохранено в БД).
type SupplierPrice struct {
	SupplierPriceID  string     `json:"supplier_price_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID       string     `json:"supplier_id" gorm:"type:uuid"`
	InvoiceImportID  string     `json:"invoice_import_id" gorm:"type:uuid"`
	InvoiceDataID    string     `json:"invoice_data_id" gorm:"type:uuid"`
	GUIDES           *string    `json:"guid_es,omitempty" gorm:"column:GUID_ES;type:uuid"`
	ItemCode         *string    `json:"item_code,omitempty"`
	ItemName         *string    `json:"item_name,omitempty"`
	SupplierItemName *string    `json:"supplier_item_name,omitempty"`
	Barcode          *string    `json:"barcode,omitempty"`
	Price            float64    `json:"price"`
	Quantity         *float64   `json:"quantity,omitempty"`
	InvoiceNumber    *string    `json:"invoice_number,omitempty"`
	InvoiceDate      *time.Time `json:"invoice_date,omitempty"`
	BatchNumber      *string    `json:"batch_number,omitempty"`
	ExpiryDate       *time.Time `json:"expiry_date,omitempty"`
	MatchMethod      *string    `json:"match_method,omitempty"`
	MatchConfidence  *float64   `json:"match_confidence,omitempty"`
	IsActive         bool       `json:"is_active"`
	CreatedAt        time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"autoUpdateTime:false;default:(NOW() AT TIME ZONE 'utc')"`
	RegionID         *string    `json:"region_id,omitempty" gorm:"type:uuid"`
	MarkupPct        *float64   `json:"markup_pct,omitempty"`
	FinalPrice       *float64   `json:"final_price,omitempty"`
	PriceListID      *string    `json:"price_list_id,omitempty" gorm:"type:uuid"`
	Series           *string    `json:"series,omitempty"`
	Manufacturer     *string    `json:"manufacturer,omitempty"`
	Country          *string    `json:"country,omitempty"`
}

// =====================================================================
// Покупательские «приложения» (учётные записи приложения)
// =====================================================================

// BuyerApplication — учётка приложения покупателя. JWT с buyer_user_id мапится на
// (BuyerUser, BuyerApplication) через resolveBuyerUser в buyer_order_handlers.go.
type BuyerApplication struct {
	BuyerApplicationID string     `json:"buyer_application_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	BuyerID            string     `json:"buyer_id" gorm:"type:uuid"`
	BuyerUserID        string     `json:"buyer_user_id" gorm:"type:uuid"`
	LastPriceUpdate    *time.Time `json:"last_price_update,omitempty"`
	IsActive           bool       `json:"is_active"`
	CreatedAt          time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}

// SupplierExportConfig — куда и как поставщик выгружает заказы (FTP/почта).
type SupplierExportConfig struct {
	SupplierExportConfigID string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID             string    `json:"supplier_id" gorm:"type:uuid"`
	Method                 string    `json:"method"` // none|ftp|email|both
	Format                 string    `json:"format"` // DBF
	FtpHost                *string   `json:"ftp_host,omitempty"`
	FtpPort                int       `json:"ftp_port"`
	FtpUser                *string   `json:"ftp_user,omitempty"`
	FtpPassword            *string   `json:"ftp_password,omitempty"`
	FtpDir                 *string   `json:"ftp_dir,omitempty"`
	EmailTo                *string   `json:"email_to,omitempty"`
	SmtpHost               *string   `json:"smtp_host,omitempty"`
	SmtpPort               int       `json:"smtp_port"`
	SmtpUser               *string   `json:"smtp_user,omitempty"`
	SmtpPassword           *string   `json:"smtp_password,omitempty"`
	SmtpFrom               *string   `json:"smtp_from,omitempty"`
	IsActive               bool      `json:"is_active"`
	UpdatedAt              time.Time `json:"updated_at" gorm:"autoCreateTime:false"`
}

// OrderExportLog — журнал выгрузок заказов поставщика.
type OrderExportLog struct {
	OrderExportLogID string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SupplierID       string    `json:"supplier_id" gorm:"type:uuid"`
	Method           *string   `json:"method,omitempty"`
	FileName         *string   `json:"file_name,omitempty"`
	OrdersCount      int       `json:"orders_count"`
	Status           string    `json:"status"` // ok|error
	Message          *string   `json:"message,omitempty"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false"`
}

// BuyerPriceList — назначение management-прайса (PriceList) покупателю.
// Если у покупателя есть активные назначения — каталог показывает товары
// только из этих прайсов; нет назначений — работает по региону (страховка).
type BuyerPriceList struct {
	BuyerPriceListID string    `json:"buyer_price_list_id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	BuyerID          string    `json:"buyer_id" gorm:"type:uuid"`
	PriceListID      string    `json:"price_list_id" gorm:"type:uuid"`
	MarkupPct        float64   `json:"markup_pct" gorm:"type:decimal(6,2)"` // индивидуальная наценка клиента на этот прайс
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
}
