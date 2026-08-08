-- Настройки выгрузки накладных поставщика: формат, под-формат 1С,
-- загруженный шаблон и сопоставление колонок (маппинг от ИИ, правится вручную).
ALTER TABLE "SupplierExportConfig" ADD COLUMN IF NOT EXISTS "OneCSubFormat"    text;
ALTER TABLE "SupplierExportConfig" ADD COLUMN IF NOT EXISTS "TemplateFileName" text;
ALTER TABLE "SupplierExportConfig" ADD COLUMN IF NOT EXISTS "ColumnMapping"    text;
