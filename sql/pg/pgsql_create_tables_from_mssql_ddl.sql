-- =============================================================================
-- PostgreSQL: создать таблицы из DDL, который сгенерировал MSSQL-скрипт
-- =============================================================================
-- Как пользоваться:
-- 1) В SSMS запусти mssql_export_schema_for_pg.sql на базе elfisa
-- 2) Результат вкладки с колонкой PG_DDL скопируй ЦЕЛИКОМ
-- 3) Вставь ниже между метками BEGIN_DDL и END_DDL (вместо примера)
-- 4) Выполни этот файл в pgAdmin / psql, подключившись к базе elfisa
-- 5) Повтори для eplus_work (другая база)
--
-- Важно:
-- - Имена таблиц/колонок в кавычках ("OrderItem") — как в MSSQL PascalCase
-- - Это только структура (CREATE TABLE). Данные отдельно (pgloader / копирование)
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- >>> ВСТАВЬ СЮДА ВЫВОД PG_DDL ИЗ MSSQL (пример ниже можно удалить) >>>
-- BEGIN_DDL

-- Пример (удали и вставь свой вывод):
-- CREATE TABLE IF NOT EXISTS "ExampleTable" (
--     "ExampleID" UUID NOT NULL,
--     "Name" TEXT,
--     PRIMARY KEY ("ExampleID")
-- );

-- END_DDL
-- <<< КОНЕЦ ВСТАВКИ <<<

-- После создания таблиц можно проверить:
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY table_name;
