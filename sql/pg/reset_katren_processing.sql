-- Сброс зависших импортов Katren (точка Price Katren)
-- Запускать в psql / pgAdmin от имени es_api на БД elfisa.
-- После этого ОДИН раз нажать «Обновить» в UI и НЕ перезапускать службу 1–2 часа.

UPDATE "InvoiceImport"
SET "ImportStatus" = 'FAILED',
    "ErrorMessage" = 'Сброс зависших импортов (ручной, 2026-08-03)',
    "CompletedAt" = (NOW() AT TIME ZONE 'utc')
WHERE "ImportPointID" = '75118c5d-e5dd-4942-bfa0-8b38fbdb77e0'::uuid
  AND "ImportStatus" = 'PROCESSING';

-- Сколько осталось активных:
SELECT "InvoiceImportID", "ImportStatus", "StartedAt", "ErrorMessage"
FROM "InvoiceImport"
WHERE "ImportPointID" = '75118c5d-e5dd-4942-bfa0-8b38fbdb77e0'::uuid
ORDER BY "StartedAt" DESC
LIMIT 10;
