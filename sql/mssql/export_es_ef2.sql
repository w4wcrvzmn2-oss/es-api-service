/*
  Экспорт справочника es_ef2 в текстовый файл (MSSQL / SSMS).

  ВАРИАНТ A — через bcp (рекомендуется, быстро):
    Запустите рядом лежащий export_es_ef2.bat
    или в cmd:
      cd D:\es_api_service\sql\mssql
      export_es_ef2.bat

  ВАРИАНТ B — через SSMS (если bcp недоступен):
    1. Откройте этот файл в SSMS, выберите базу elfisa
    2. Меню: Запрос -> Результаты в -> Результаты в файл (Ctrl+Shift+F)
    3. Выполните только блок SELECT ниже (без bcp)
    4. Сохраните как es_ef2.txt (разделитель — табуляция)

  Файл es_ef2.txt положите на сервер PG:
    D:\es_api_service\data\es_ef2.txt
  Затем на сервере PG:
    D:\es_api_service\scripts\import-es-ef2-from-txt.ps1
*/

USE elfisa;
GO

SET NOCOUNT ON;

-- Проверка: сколько строк выгрузим
SELECT COUNT(*) AS rows_to_export
FROM dbo.es_ef2
WHERE DELETED IS NULL;

-- === Этот SELECT — для SSMS «Результаты в файл» ===
SELECT
    CAST(GUID_ES AS varchar(36))          AS GUID_ES,
    REPLACE(REPLACE(REPLACE(ISNULL(NAME, ''), CHAR(9), ' '), CHAR(10), ' '), CHAR(13), ' ') AS NAME,
    REPLACE(REPLACE(ISNULL(BARCODE, ''), CHAR(9), ' '), CHAR(10), ' ') AS BARCODE,
    ISNULL(CUREFORM_COD, '')              AS CUREFORM_COD,
    REPLACE(REPLACE(ISNULL(CUREFORM_NAME, ''), CHAR(9), ' '), CHAR(10), ' ') AS CUREFORM_NAME,
    REPLACE(REPLACE(ISNULL(INN_NAME_RUS, ''), CHAR(9), ' '), CHAR(10), ' ') AS INN_NAME_RUS,
    REPLACE(REPLACE(ISNULL(INN_NAME_LAT, ''), CHAR(9), ' '), CHAR(10), ' ') AS INN_NAME_LAT,
    ISNULL(CAST(PRODUCER_COD AS varchar(20)), '') AS PRODUCER_COD,
    REPLACE(REPLACE(ISNULL(TRN_NAME_RUS, ''), CHAR(9), ' '), CHAR(10), ' ') AS TRN_NAME_RUS,
    REPLACE(REPLACE(ISNULL(TRN_NAME_LAT, ''), CHAR(9), ' '), CHAR(10), ' ') AS TRN_NAME_LAT,
    ISNULL(CAST(UPAK_COD AS varchar(20)), '') AS UPAK_COD,
    CONVERT(varchar(30), DATA_AN, 126)    AS DATA_AN,
    CONVERT(varchar(30), DATA_REG, 126)   AS DATA_REG,
    REPLACE(REPLACE(ISNULL(DOSAGE, ''), CHAR(9), ' '), CHAR(10), ' ') AS DOSAGE,
    ISNULL(CAST(KOD_ES AS varchar(20)), '0') AS KOD_ES,
    ISNULL(CAST(NDS_RATE AS varchar(20)), '0') AS NDS_RATE,
    ISNULL(CAST(ID_ES AS varchar(20)), '0') AS ID_ES,
    REPLACE(REPLACE(ISNULL(DISCRIBE, ''), CHAR(9), ' '), CHAR(10), ' ') AS DISCRIBE,
    ISNULL(CAST(RATING AS varchar(10)), '0') AS RATING,
    CONVERT(varchar(30), ISNULL(UPDATED, GETDATE()), 126) AS UPDATED
FROM dbo.es_ef2
WHERE DELETED IS NULL
ORDER BY KOD_ES;
