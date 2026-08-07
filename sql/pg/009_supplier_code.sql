-- 009: Код поставщика (только цифры). Попадает в DBF-выгрузку заказов (колонка SUP_CODE).
ALTER TABLE "Supplier" ADD COLUMN IF NOT EXISTS "Code" text;
