/* =====================================================================
   PharmData — снятие тяжёлых индексов с таблицы импорта SupplierPrice
   ---------------------------------------------------------------------
   Причина: SupplierPrice — таблица, куда импорт прайсов пишет массово
   (десятки тысяч INSERT/UPDATE, 32+ параллельных воркеров). Широкие
   покрывающие индексы резко замедляют эту запись и приводят к
   'context deadline exceeded' у воркеров + таймаутам чтения по всей базе.

   Этот скрипт удаляет ТОЛЬКО 4 индекса SupplierPrice. Остальные 15
   индексов (Order, OrderItem, InvoiceImport, PriceList, справочники и т.д.)
   НЕ трогаются — они ускоряют чтение и таблицу импорта не затрагивают.

   ВАЖНО: выполнять, когда импорт НЕ идёт (иначе DROP будет ждать блокировку).
   Идемпотентный: чего нет — пропускает.
   ===================================================================== */

SET NOCOUNT ON;
GO
IF OBJECT_ID('tempdb..#dropidx') IS NOT NULL DROP PROCEDURE #dropidx;
GO
CREATE PROCEDURE #dropidx @name SYSNAME
AS
BEGIN
    IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = @name AND object_id = OBJECT_ID('dbo.SupplierPrice'))
    BEGIN PRINT 'НЕТ: ' + @name; RETURN; END
    BEGIN TRY
        EXEC sp_executesql N'DROP INDEX ' + QUOTENAME(@name) + N' ON dbo.SupplierPrice';
        PRINT 'DROPPED: ' + @name;
    END TRY
    BEGIN CATCH
        PRINT 'ERROR ' + @name + ': ' + ERROR_MESSAGE();
    END CATCH
END
GO

EXEC #dropidx 'IX_SupplierPrice_Supplier_Active';   -- самый тяжёлый: 16 колонок в INCLUDE
EXEC #dropidx 'IX_SupplierPrice_Matched';           -- фильтрованный, переписывается при смене GUID_ES
EXEC #dropidx 'IX_SupplierPrice_InvoiceImport';
EXEC #dropidx 'IX_SupplierPrice_PriceList';
GO

DROP PROCEDURE #dropidx;
GO
PRINT '=== Тяжёлые индексы SupplierPrice сняты. Импорт снова быстрый. ===';
GO
