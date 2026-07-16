/* =====================================================================
   PharmData — БЕЗОПАСНЫЙ набор индексов (MS SQL Server)
   ---------------------------------------------------------------------
   Отличие от perf_indexes.sql: НЕ содержит ни одного индекса на таблицах,
   в которые импорт прайса пишет массово (SupplierPrice, es_ef2,
   SupplierItemMapping). Поэтому этот набор НЕ может замедлить импорт и
   вызвать таймауты, как это случилось с широкими индексами SupplierPrice.

   Что ускоряет: списки заказов, позиции заказов, страницы прайс-листов
   (снятие вложенных TOP-1 по InvoiceImport), справочники, покупателей.
   Это как раз те страницы, где вы заметили ускорение.

   Свойства: идемпотентный (по имени), устойчивый (TRY/CATCH), только
   CREATE INDEX. Родные индексы базы не трогает.
   Можно запускать в рабочее время — таблицы лёгкие на запись.
   ===================================================================== */

SET NOCOUNT ON;
GO
IF OBJECT_ID('tempdb..#mkidx') IS NOT NULL DROP PROCEDURE #mkidx;
GO
CREATE PROCEDURE #mkidx @name SYSNAME, @object NVARCHAR(300), @ddl NVARCHAR(MAX)
AS
BEGIN
    IF OBJECT_ID(@object) IS NULL
    BEGIN PRINT 'SKIP (нет таблицы ' + @object + '): ' + @name; RETURN; END
    IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = @name AND object_id = OBJECT_ID(@object))
    BEGIN PRINT 'EXISTS: ' + @name; RETURN; END
    BEGIN TRY
        EXEC sp_executesql @ddl;
        PRINT 'CREATED: ' + @name;
    END TRY
    BEGIN CATCH
        PRINT 'ERROR ' + @name + ': ' + ERROR_MESSAGE();
    END CATCH
END
GO

/* ---------- Заказы (пишутся при оформлении заказа, не при импорте) ---------- */
EXEC #mkidx 'IX_Order_BuyerUser_CreatedAt', 'dbo.[Order]',
 'CREATE NONCLUSTERED INDEX IX_Order_BuyerUser_CreatedAt ON dbo.[Order](BuyerUserID, CreatedAt DESC)
  INCLUDE(OrderStatusID, BuyerLocationID, TotalAmount, PlacedAt, Comment)';

EXEC #mkidx 'IX_OrderItem_OrderID', 'dbo.OrderItem',
 'CREATE NONCLUSTERED INDEX IX_OrderItem_OrderID ON dbo.OrderItem(OrderID)
  INCLUDE(SupplierID, ProductID, RegionID, Qty, UnitPrice, PriceListID, CreatedAt)';

EXEC #mkidx 'IX_OrderItem_Supplier_Order', 'dbo.OrderItem',
 'CREATE NONCLUSTERED INDEX IX_OrderItem_Supplier_Order ON dbo.OrderItem(SupplierID, OrderID)
  INCLUDE(Qty, UnitPrice)';
GO

/* ---------- Импорт-метаданные: 1 строка на импорт (запись ничтожна) ---------- */
-- Снимает повторные вложенные TOP-1 ... ORDER BY CompletedAt DESC на страницах прайс-листов.
EXEC #mkidx 'IX_InvoiceImport_Point_Status_Completed', 'dbo.InvoiceImport',
 'CREATE NONCLUSTERED INDEX IX_InvoiceImport_Point_Status_Completed ON dbo.InvoiceImport(ImportPointID, ImportStatus, CompletedAt DESC)
  INCLUDE(InvoiceImportID)';
GO

/* ---------- Конфигурация прайс-листов и наценок (низкий объём записи) ---------- */
EXEC #mkidx 'IX_PriceListRegion_PriceList_Region', 'dbo.PriceListRegion',
 'CREATE NONCLUSTERED INDEX IX_PriceListRegion_PriceList_Region ON dbo.PriceListRegion(PriceListID, RegionID)
  INCLUDE(IsActive, MarkupPct)';
GO

/* ---------- Справочник производителей (пишется при синхронизации, не при импорте) ---------- */
-- Убирает построчный OUTER APPLY (... TOP 1 PRODUCER_NAME ...) в гриде прайсов.
EXEC #mkidx 'IX_es_producer_Kod', 'dbo.es_producer',
 'CREATE NONCLUSTERED INDEX IX_es_producer_Kod ON dbo.es_producer(KOD_PRODUCER)
  INCLUDE(PRODUCER_NAME)';
GO

/* ---------- Статусы заказов (крошечная таблица) ---------- */
EXEC #mkidx 'IX_OrderStatus_Name', 'dbo.OrderStatus',
 'CREATE NONCLUSTERED INDEX IX_OrderStatus_Name ON dbo.OrderStatus(Name) INCLUDE(OrderStatusID)';
GO

/* ---------- Покупатели и точки доставки (низкий объём записи) ---------- */
EXEC #mkidx 'IX_BuyerUser_Buyer_Active', 'dbo.BuyerUser',
 'CREATE NONCLUSTERED INDEX IX_BuyerUser_Buyer_Active ON dbo.BuyerUser(BuyerID, IsActive)
  INCLUDE(FullName, Email)';

EXEC #mkidx 'IX_BuyerUser_Email', 'dbo.BuyerUser',
 'CREATE NONCLUSTERED INDEX IX_BuyerUser_Email ON dbo.BuyerUser(Email)';

EXEC #mkidx 'IX_BuyerLocation_Buyer_Default', 'dbo.BuyerLocation',
 'CREATE NONCLUSTERED INDEX IX_BuyerLocation_Buyer_Default ON dbo.BuyerLocation(BuyerID, IsDefault)
  INCLUDE(Address, RegionID)';

EXEC #mkidx 'IX_Buyer_Active_Name', 'dbo.Buyer',
 'CREATE NONCLUSTERED INDEX IX_Buyer_Active_Name ON dbo.Buyer(IsActive, Name) INCLUDE(RegionID)';
GO

DROP PROCEDURE #mkidx;
GO
PRINT '=== Готово. Безопасный набор индексов применён (без таблиц импорта). ===';
GO
