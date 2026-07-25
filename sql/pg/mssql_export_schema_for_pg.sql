-- =============================================================================
-- MSSQL → список таблиц + генерация CREATE TABLE для PostgreSQL
-- Запускать в SSMS на базе elfisa (и отдельно на eplus_work).
-- Результат вкладки "PG_DDL" скопировать и выполнить в PostgreSQL.
-- =============================================================================

SET NOCOUNT ON;

PRINT N'=== База: ' + DB_NAME() + N' ===';
PRINT N'';

-- ---------------------------------------------------------------------------
-- 1) Список всех пользовательских таблиц
-- ---------------------------------------------------------------------------
SELECT
    s.name AS [Schema],
    t.name AS [TableName],
    SUM(p.rows) AS [ApproxRows]
FROM sys.tables t
INNER JOIN sys.schemas s ON s.schema_id = t.schema_id
INNER JOIN sys.partitions p ON p.object_id = t.object_id AND p.index_id IN (0, 1)
WHERE t.is_ms_shipped = 0
GROUP BY s.name, t.name
ORDER BY t.name;

-- ---------------------------------------------------------------------------
-- 2) Генерация PostgreSQL DDL (PascalCase в кавычках)
-- ---------------------------------------------------------------------------
;WITH cols AS (
    SELECT
        s.name AS schema_name,
        t.name AS table_name,
        c.column_id,
        c.name AS column_name,
        ty.name AS type_name,
        c.max_length,
        c.precision,
        c.scale,
        c.is_nullable,
        c.is_identity,
        CASE
            WHEN ty.name IN ('uniqueidentifier') THEN 'UUID'
            WHEN ty.name IN ('nvarchar', 'varchar', 'nchar', 'char', 'sysname', 'xml', 'text', 'ntext') THEN 'TEXT'
            WHEN ty.name IN ('bit') THEN 'BOOLEAN'
            WHEN ty.name IN ('tinyint', 'smallint') THEN 'SMALLINT'
            WHEN ty.name IN ('int') THEN 'INTEGER'
            WHEN ty.name IN ('bigint') THEN 'BIGINT'
            WHEN ty.name IN ('decimal', 'numeric', 'money', 'smallmoney') THEN
                'NUMERIC(' + CAST(c.precision AS varchar(10)) + ',' + CAST(c.scale AS varchar(10)) + ')'
            WHEN ty.name IN ('float') THEN 'DOUBLE PRECISION'
            WHEN ty.name IN ('real') THEN 'REAL'
            WHEN ty.name IN ('datetime', 'datetime2', 'smalldatetime', 'datetimeoffset') THEN 'TIMESTAMPTZ'
            WHEN ty.name IN ('date') THEN 'DATE'
            WHEN ty.name IN ('time') THEN 'TIME'
            WHEN ty.name IN ('varbinary', 'binary', 'image', 'timestamp', 'rowversion') THEN 'BYTEA'
            WHEN ty.name IN ('hierarchyid', 'geometry', 'geography', 'sql_variant') THEN 'TEXT'
            ELSE 'TEXT'
        END AS pg_type
    FROM sys.tables t
    INNER JOIN sys.schemas s ON s.schema_id = t.schema_id
    INNER JOIN sys.columns c ON c.object_id = t.object_id
    INNER JOIN sys.types ty ON ty.user_type_id = c.user_type_id
    WHERE t.is_ms_shipped = 0
),
pk AS (
    SELECT
        t.name AS table_name,
        c.name AS column_name,
        ic.key_ordinal
    FROM sys.tables t
    INNER JOIN sys.indexes i ON i.object_id = t.object_id AND i.is_primary_key = 1
    INNER JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
    INNER JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
),
agg AS (
    SELECT
        c.table_name,
        STRING_AGG(
            '    "' + c.column_name + '" ' + c.pg_type +
            CASE WHEN c.is_nullable = 0 THEN ' NOT NULL' ELSE '' END,
            ',' + CHAR(13) + CHAR(10)
        ) WITHIN GROUP (ORDER BY c.column_id) AS col_defs,
        (
            SELECT STRING_AGG('"' + p.column_name + '"', ', ') WITHIN GROUP (ORDER BY p.key_ordinal)
            FROM pk p
            WHERE p.table_name = c.table_name
        ) AS pk_cols
    FROM cols c
    GROUP BY c.table_name
)
SELECT
    'CREATE TABLE IF NOT EXISTS "' + a.table_name + '" (' + CHAR(13) + CHAR(10) +
    a.col_defs +
    CASE
        WHEN a.pk_cols IS NOT NULL THEN ',' + CHAR(13) + CHAR(10) + '    PRIMARY KEY (' + a.pk_cols + ')'
        ELSE ''
    END +
    CHAR(13) + CHAR(10) + ');' + CHAR(13) + CHAR(10) AS PG_DDL
FROM agg a
ORDER BY a.table_name;

PRINT N'';
PRINT N'Готово. Скопируй колонку PG_DDL (все строки) → выполни в PostgreSQL в нужной базе.';
PRINT N'Повтори этот скрипт отдельно для elfisa и для eplus_work.';
GO
