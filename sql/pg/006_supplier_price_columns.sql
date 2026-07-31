-- Добавляет колонки, которые могли отсутствовать в старых схемах PG после миграции с MSSQL.
-- psql -U es_api -d elfisa -f 006_supplier_price_columns.sql

ALTER TABLE "SupplierPrice" ADD COLUMN IF NOT EXISTS "Manufacturer" TEXT;
ALTER TABLE "SupplierPrice" ADD COLUMN IF NOT EXISTS "Country" TEXT;
ALTER TABLE "InvoiceData" ADD COLUMN IF NOT EXISTS "Manufacturer" TEXT;
ALTER TABLE "InvoiceData" ADD COLUMN IF NOT EXISTS "Country" TEXT;

-- Совместимость имён: код обращается к "es_ef2", в схеме таблица "ES_EF2"
DO $$
BEGIN
  IF to_regclass('public."ES_EF2"') IS NOT NULL
     AND to_regclass('public."es_ef2"') IS NULL THEN
    EXECUTE 'CREATE OR REPLACE VIEW "es_ef2" AS SELECT * FROM "ES_EF2"';
  END IF;
END $$;

SELECT 'SupplierPrice' AS tbl, COUNT(*)::bigint AS cnt FROM "SupplierPrice"
UNION ALL
SELECT 'es_ef2', COUNT(*)::bigint FROM "es_ef2"
UNION ALL
SELECT 'ES_EF2', COUNT(*)::bigint FROM "ES_EF2";
