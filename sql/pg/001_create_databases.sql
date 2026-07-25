-- Создать базы и пользователя для es_api_service (запускать от postgres)
-- psql -U postgres -f 001_create_databases.sql

CREATE USER es_api WITH PASSWORD 'CHANGE_ME_STRONG_PASSWORD';

CREATE DATABASE elfisa OWNER es_api ENCODING 'UTF8';
CREATE DATABASE eplus_work OWNER es_api ENCODING 'UTF8';

\c elfisa
CREATE EXTENSION IF NOT EXISTS pgcrypto;
GRANT ALL ON SCHEMA public TO es_api;

\c eplus_work
CREATE EXTENSION IF NOT EXISTS pgcrypto;
GRANT ALL ON SCHEMA public TO es_api;
