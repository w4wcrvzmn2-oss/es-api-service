-- Сохраняем ссылку на строку прайса при создании заказа (десктоп шлёт supplier_price_id).
-- Нужно для выгрузки DBF: наименование, код, штрихкод даже без сопоставления GUID_ES.
--
-- Запускать на рабочей БД сервиса (не master). В SSMS: один скрипт целиком, F5.

SET NOCOUNT ON;

-- Если индекс остался от прошлой неудачной попытки, а колонки ещё нет — убираем индекс.
IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'SupplierPriceID'
)
AND EXISTS (
    SELECT 1 FROM sys.indexes
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'IX_OrderItem_SupplierPriceID'
)
BEGIN
    DROP INDEX IX_OrderItem_SupplierPriceID ON dbo.OrderItem;
    PRINT N'Удалён битый индекс IX_OrderItem_SupplierPriceID (колонки ещё не было).';
END
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'SupplierPriceID'
)
BEGIN
    ALTER TABLE dbo.OrderItem ADD SupplierPriceID UNIQUEIDENTIFIER NULL;
    PRINT N'Добавлена колонка SupplierPriceID.';
END
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'ItemName'
)
BEGIN
    ALTER TABLE dbo.OrderItem ADD ItemName NVARCHAR(500) NULL;
    PRINT N'Добавлена колонка ItemName.';
END
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'ItemCode'
)
BEGIN
    ALTER TABLE dbo.OrderItem ADD ItemCode NVARCHAR(100) NULL;
    PRINT N'Добавлена колонка ItemCode.';
END
GO

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'Barcode'
)
BEGIN
    ALTER TABLE dbo.OrderItem ADD Barcode NVARCHAR(50) NULL;
    PRINT N'Добавлена колонка Barcode.';
END
GO

IF EXISTS (
    SELECT 1 FROM sys.columns
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'SupplierPriceID'
)
AND NOT EXISTS (
    SELECT 1 FROM sys.indexes
    WHERE object_id = OBJECT_ID(N'dbo.OrderItem') AND name = N'IX_OrderItem_SupplierPriceID'
)
BEGIN
    CREATE NONCLUSTERED INDEX IX_OrderItem_SupplierPriceID ON dbo.OrderItem(SupplierPriceID)
    WHERE SupplierPriceID IS NOT NULL;
    PRINT N'Создан индекс IX_OrderItem_SupplierPriceID.';
END
GO

-- Проверка результата
SELECT c.name AS ColumnName, t.name AS DataType, c.max_length
FROM sys.columns c
JOIN sys.types t ON c.user_type_id = t.user_type_id
WHERE c.object_id = OBJECT_ID(N'dbo.OrderItem')
  AND c.name IN (N'SupplierPriceID', N'ItemName', N'ItemCode', N'Barcode')
ORDER BY c.name;
GO
