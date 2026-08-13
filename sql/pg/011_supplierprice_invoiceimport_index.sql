-- 011: индекс для быстрых счётчиков позиций прайса по последнему импорту.
-- Страница /api/price-lists считала COUNT(*) по SupplierPrice, фильтруя только
-- по InvoiceImportID → полный скан 4.8М строк на каждый прайс (~7с на 8 прайсов).
-- Индекс (InvoiceImportID, GUID_ES) даёт index-only scan: и COUNT(*), и
-- COUNT(*) FILTER (GUID_ES IS NULL) считаются без обращения к таблице.
CREATE INDEX CONCURRENTLY IF NOT EXISTS "IX_SupplierPrice_InvoiceImport_Guid"
    ON "SupplierPrice" ("InvoiceImportID", "GUID_ES");
