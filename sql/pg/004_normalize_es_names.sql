-- Нормализация имён справочников ES в elfisa.
-- На сервере: psql -U es_api -d elfisa -f 004_normalize_es_names.sql

-- Если есть только lowercase es_ef2 — оставляем как есть (API уже мапит на "es_ef2").
-- Если синк создал "ES_EF2", а "es_ef2" тоже есть — ничего не трогаем.
-- Если есть "ES_EF2", а код ждёт "es_ef2" — создаём совместимый VIEW.

DO $$
BEGIN
  IF to_regclass('public."ES_EF2"') IS NOT NULL
     AND to_regclass('public."es_ef2"') IS NULL THEN
    EXECUTE 'CREATE VIEW "es_ef2" AS SELECT * FROM "ES_EF2"';
  END IF;

  IF to_regclass('public."es_producer"') IS NOT NULL
     AND to_regclass('public."ES_PRODUCER"') IS NULL THEN
    EXECUTE 'ALTER TABLE "es_producer" RENAME TO "ES_PRODUCER"';
  END IF;

  IF to_regclass('public."ES_PRODUCER"') IS NOT NULL
     AND to_regclass('public."es_producer"') IS NULL THEN
    -- уже каноническое имя
    NULL;
  END IF;
END $$;

-- Быстрая проверка
SELECT c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND lower(c.relname) IN ('es_ef2', 'es_producer')
ORDER BY 1;
