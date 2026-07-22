-- Полный бэкап пользовательских БД на инстансе SQL Server.
-- Запуск в SSMS: подключитесь к инстансу → New Query → выполните весь скрипт (F5).
--
-- Перед запуском:
--   1) Создайте папку бэкапов (по умолчанию C:\SQLBackups) или смените @BackupRoot.
--   2) Убедитесь, что служба SQL Server имеет права WRITE на эту папку.
--   3) Для одной конкретной БД — раскомментируйте фильтр WHERE name = N'ИмяБД'.

SET NOCOUNT ON;

DECLARE @BackupRoot NVARCHAR(400) = N'C:\SQLBackups';  -- <-- папка бэкапов
DECLARE @Stamp      NVARCHAR(20)  = CONVERT(NVARCHAR(8), GETDATE(), 112)
                                 + N'_'
                                 + REPLACE(CONVERT(NVARCHAR(8), GETDATE(), 108), N':', N'');
DECLARE @DbName     SYSNAME;
DECLARE @FileName   NVARCHAR(500);
DECLARE @Sql        NVARCHAR(MAX);

-- Папка должна уже существовать. Если нет — создайте вручную или через PowerShell-скрипт.
IF LEFT(@BackupRoot, 2) = N'\\'
    PRINT N'UNC-путь: ' + @BackupRoot;
ELSE
    PRINT N'Локальный путь: ' + @BackupRoot;

DECLARE db_cursor CURSOR LOCAL FAST_FORWARD FOR
SELECT name
FROM sys.databases
WHERE state_desc = N'ONLINE'
  AND name NOT IN (N'master', N'model', N'msdb', N'tempdb')
  -- AND name = N'ИмяВашейБД'   -- раскомментируйте для одной БД
ORDER BY name;

OPEN db_cursor;
FETCH NEXT FROM db_cursor INTO @DbName;

WHILE @@FETCH_STATUS = 0
BEGIN
    SET @FileName = @BackupRoot + N'\' + @DbName + N'_FULL_' + @Stamp + N'.bak';
    SET @Sql = N'
BACKUP DATABASE ' + QUOTENAME(@DbName) + N'
TO DISK = N''' + REPLACE(@FileName, N'''', N'''''') + N'''
WITH
    COMPRESSION,
    CHECKSUM,
    INIT,
    STATS = 10,
    NAME = N''' + REPLACE(@DbName, N'''', N'''''') + N'-Full Database Backup'',
    DESCRIPTION = N''Full backup before migration / maintenance'';
';

    PRINT N'=== FULL BACKUP: ' + @DbName + N' → ' + @FileName + N' ===';
    BEGIN TRY
        EXEC sys.sp_executesql @Sql;
        PRINT N'OK: ' + @DbName;
    END TRY
    BEGIN CATCH
        PRINT N'ERROR: ' + @DbName + N' — ' + ERROR_MESSAGE();
    END CATCH;

    FETCH NEXT FROM db_cursor INTO @DbName;
END;

CLOSE db_cursor;
DEALLOCATE db_cursor;

PRINT N'Готово. Проверка целостности (опционально): RESTORE VERIFYONLY FROM DISK = ''...bak'' WITH CHECKSUM;';
GO
