-- Индексы под массовую выдачу/delta прайсов (6×~230k).
-- Без CONCURRENTLY — можно гонять в pgAdmin / DBeaver одним скриптом (в транзакции).
-- На большой таблице индекс создаст краткую блокировку записи; для старта ок.

CREATE INDEX IF NOT EXISTS "IX_SupplierPrice_Supplier_Active_Updated"
  ON "SupplierPrice" ("SupplierID", "IsActive", "UpdatedAt" DESC);

CREATE INDEX IF NOT EXISTS "IX_SupplierPrice_Supplier_InvoiceImport"
  ON "SupplierPrice" ("SupplierID", "InvoiceImportID")
  WHERE "IsActive" = TRUE;

CREATE INDEX IF NOT EXISTS "IX_SupplierPrice_UpdatedAt"
  ON "SupplierPrice" ("UpdatedAt");
