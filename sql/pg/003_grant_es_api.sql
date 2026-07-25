-- Права для пользователя es_api.
-- Запускать от postgres ДВАЖДЫ: в базе elfisa и в базе eplus_work.
--
-- pgAdmin: Query Tool → выбрать базу elfisa → выполнить весь скрипт.
-- потом то же самое в базе eplus_work.
--
-- psql:
--   psql -U postgres -d elfisa -f 003_grant_es_api.sql
--   psql -U postgres -d eplus_work -f 003_grant_es_api.sql

GRANT USAGE, CREATE ON SCHEMA public TO es_api;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO es_api;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO es_api;
GRANT ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public TO es_api;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO es_api;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO es_api;

-- Таблицы создавал postgres → передаём владельца es_api
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT tablename FROM pg_tables WHERE schemaname = 'public'
    LOOP
        EXECUTE format('ALTER TABLE public.%I OWNER TO es_api', r.tablename);
    END LOOP;
    FOR r IN
        SELECT c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind = 'S'
    LOOP
        EXECUTE format('ALTER SEQUENCE public.%I OWNER TO es_api', r.relname);
    END LOOP;
END $$;
