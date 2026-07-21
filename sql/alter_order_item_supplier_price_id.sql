-- Сохраняем ссылку на строку прайса при создании заказа (десктоп шлёт supplier_price_id).
-- Нужно для выгрузки DBF: наименование, код, штрихкод даже без сопоставления GUID_ES.
IF COL_LENGTH('dbo.OrderItem', 'SupplierPriceID') IS NULL
BEGIN
    ALTER TABLE dbo.OrderItem ADD SupplierPriceID UNIQUEIDENTIFIER NULL;
END
GO

IF COL_LENGTH('dbo.OrderItem', 'ItemName') IS NULL
BEGIN
    ALTER TABLE dbo.OrderItem ADD ItemName NVARCHAR(500) NULL;
END
GO

IF COL_LENGTH('dbo.OrderItem', 'ItemCode') IS NULL
BEGIN
    ALTER TABLE dbo.OrderItem ADD ItemCode NVARCHAR(100) NULL;
END
GO

IF COL_LENGTH('dbo.OrderItem', 'Barcode') IS NULL
BEGIN
    ALTER TABLE dbo.OrderItem ADD Barcode NVARCHAR(50) NULL;
END
GO

-- Подсказка для выборок по заказу поставщика (если индекса ещё нет).
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
    WHERE name = 'IX_OrderItem_SupplierPriceID' AND object_id = OBJECT_ID('dbo.OrderItem')
)
BEGIN
    CREATE NONCLUSTERED INDEX IX_OrderItem_SupplierPriceID ON dbo.OrderItem(SupplierPriceID)
    WHERE SupplierPriceID IS NOT NULL;
END
GO
