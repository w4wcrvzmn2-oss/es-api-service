/* =====================================================================
   PharmData — индексы производительности (MS SQL Server)
   ---------------------------------------------------------------------
   Что делает: добавляет недостающие некластерные индексы под тяжёлые
   запросы (списки заказов, сводный прайс, грид прайсов, поиск, импорт).
   Схему данных НЕ меняет — только индексы (это чистый тюнинг MSSQL).

   Свойства:
     • Идемпотентный — повторный запуск ничего не ломает (проверка по имени).
     • Устойчивый — если колонки/таблицы нет, конкретный индекс пропускается
       (TRY/CATCH), остальные создаются. Смотрите сообщения в выводе.
     • Безопасный — только CREATE INDEX, никаких DROP/ALTER данных.

   Как запускать: SSMS → база elfisa → выполнить весь скрипт (F5).
   Рекомендуется в период низкой нагрузки: создание индекса на больших
   таблицах кратковременно их блокирует (на редакции Standard — без ONLINE).
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

/* ---------- [Order]: списки заказов, ORDER BY CreatedAt DESC ---------- */
EXEC #mkidx 'IX_Order_CreatedAt', 'dbo.[Order]',
 'CREATE NONCLUSTERED INDEX IX_Order_CreatedAt ON dbo.[Order](CreatedAt DESC)
  INCLUDE(BuyerUserID, BuyerApplicationID, BuyerLocationID, OrderStatusID, PlacedAt, TotalAmount, Comment)';

EXEC #mkidx 'IX_Order_BuyerUser_CreatedAt', 'dbo.[Order]',
 'CREATE NONCLUSTERED INDEX IX_Order_BuyerUser_CreatedAt ON dbo.[Order](BuyerUserID, CreatedAt DESC)
  INCLUDE(OrderStatusID, BuyerLocationID, TotalAmount, PlacedAt, Comment)';

EXEC #mkidx 'IX_Order_OrderStatusID', 'dbo.[Order]',
 'CREATE NONCLUSTERED INDEX IX_Order_OrderStatusID ON dbo.[Order](OrderStatusID)';
GO

/* ---------- OrderItem: позиции заказа, COUNT по OrderID, отчёт поставщика ---------- */
EXEC #mkidx 'IX_OrderItem_OrderID', 'dbo.OrderItem',
 'CREATE NONCLUSTERED INDEX IX_OrderItem_OrderID ON dbo.OrderItem(OrderID)
  INCLUDE(SupplierID, ProductID, RegionID, Qty, UnitPrice, PriceListID, CreatedAt)';

EXEC #mkidx 'IX_OrderItem_Supplier_Order', 'dbo.OrderItem',
 'CREATE NONCLUSTERED INDEX IX_OrderItem_Supplier_Order ON dbo.OrderItem(SupplierID, OrderID)
  INCLUDE(Qty, UnitPrice)';
GO

/* ---------- SupplierPrice: самая горячая таблица (грид, сводный прайс) ---------- */
-- Грид прайсов поставщика и главный фильтр по импорту.
EXEC #mkidx 'IX_SupplierPrice_Supplier_Active', 'dbo.SupplierPrice',
 'CREATE NONCLUSTERED INDEX IX_SupplierPrice_Supplier_Active ON dbo.SupplierPrice(SupplierID, IsActive)
  INCLUDE(GUID_ES, InvoiceImportID, RegionID, PriceListID, Price, FinalPrice, MarkupPct, InvoiceDate,
          ItemName, BatchNumber, ExpiryDate, Manufacturer, Country, Quantity, MatchMethod, MatchConfidence)';

-- Фильтр WHERE sp.InvoiceImportID IN (...) в основном гриде и подсчётах прайс-листов.
EXEC #mkidx 'IX_SupplierPrice_InvoiceImport', 'dbo.SupplierPrice',
 'CREATE NONCLUSTERED INDEX IX_SupplierPrice_InvoiceImport ON dbo.SupplierPrice(InvoiceImportID, IsActive)
  INCLUDE(GUID_ES)';

-- Сводный прайс: только сопоставленные активные записи (фильтрованный индекс — компактный).
EXEC #mkidx 'IX_SupplierPrice_Matched', 'dbo.SupplierPrice',
 'CREATE NONCLUSTERED INDEX IX_SupplierPrice_Matched ON dbo.SupplierPrice(SupplierID, GUID_ES, InvoiceDate DESC)
  INCLUDE(Price, FinalPrice, MarkupPct, Quantity, BatchNumber, ExpiryDate, Manufacturer, Country, RegionID, MatchMethod, MatchConfidence)
  WHERE IsActive = 1 AND GUID_ES IS NOT NULL';

-- Резолв позиции корзины и апдейты по прайс-листу.
EXEC #mkidx 'IX_SupplierPrice_PriceList', 'dbo.SupplierPrice',
 'CREATE NONCLUSTERED INDEX IX_SupplierPrice_PriceList ON dbo.SupplierPrice(PriceListID, IsActive)
  INCLUDE(GUID_ES, RegionID, Price, FinalPrice)';
GO

/* ---------- InvoiceImport: вложенные TOP 1 ... ORDER BY CompletedAt DESC ---------- */
-- Один из самых крупных выигрышей: снимает повторные корр. подзапросы в списках прайсов.
EXEC #mkidx 'IX_InvoiceImport_Point_Status_Completed', 'dbo.InvoiceImport',
 'CREATE NONCLUSTERED INDEX IX_InvoiceImport_Point_Status_Completed ON dbo.InvoiceImport(ImportPointID, ImportStatus, CompletedAt DESC)
  INCLUDE(InvoiceImportID)';
GO

/* ---------- PriceList / PriceListRegion ---------- */
EXEC #mkidx 'IX_PriceList_SupplierID', 'dbo.PriceList',
 'CREATE NONCLUSTERED INDEX IX_PriceList_SupplierID ON dbo.PriceList(SupplierID)
  INCLUDE(ImportPointID, IsActive, Name)';

EXEC #mkidx 'IX_PriceList_ImportPointID', 'dbo.PriceList',
 'CREATE NONCLUSTERED INDEX IX_PriceList_ImportPointID ON dbo.PriceList(ImportPointID)
  INCLUDE(SupplierID, IsActive, Name)';

EXEC #mkidx 'IX_PriceListRegion_PriceList_Region', 'dbo.PriceListRegion',
 'CREATE NONCLUSTERED INDEX IX_PriceListRegion_PriceList_Region ON dbo.PriceListRegion(PriceListID, RegionID)
  INCLUDE(IsActive, MarkupPct)';
GO

/* ---------- Справочники под JOIN/поиск ---------- */
-- OUTER APPLY (... TOP 1 PRODUCER_NAME ...) выполнялся на каждую строку грида/сводного прайса.
EXEC #mkidx 'IX_es_producer_Kod', 'dbo.es_producer',
 'CREATE NONCLUSTERED INDEX IX_es_producer_Kod ON dbo.es_producer(KOD_PRODUCER)
  INCLUDE(PRODUCER_NAME)';

-- ensureOrderStatusID ищет статус по имени.
EXEC #mkidx 'IX_OrderStatus_Name', 'dbo.OrderStatus',
 'CREATE NONCLUSTERED INDEX IX_OrderStatus_Name ON dbo.OrderStatus(Name) INCLUDE(OrderStatusID)';

-- Полная загрузка справочника ЛС в память при импорте + фильтр активных.
EXEC #mkidx 'IX_es_ef2_Active', 'dbo.es_ef2',
 'CREATE NONCLUSTERED INDEX IX_es_ef2_Active ON dbo.es_ef2(is_active, DELETED)
  INCLUDE(GUID_ES, NAME, BARCODE, KOD_ES)';

EXEC #mkidx 'IX_es_ef2_INN', 'dbo.es_ef2',
 'CREATE NONCLUSTERED INDEX IX_es_ef2_INN ON dbo.es_ef2(INN_NAME_RUS)';

-- Кэш сопоставлений: MERGE и подбор по (SupplierID, ItemCode).
EXEC #mkidx 'IX_SupplierItemMapping_Supplier_ItemCode', 'dbo.SupplierItemMapping',
 'CREATE NONCLUSTERED INDEX IX_SupplierItemMapping_Supplier_ItemCode ON dbo.SupplierItemMapping(SupplierID, ItemCode)
  INCLUDE(GUID_ES, MatchMethod, MatchConfidence, UseCount, LastUsedAt)';

-- Проверка прав поставщика на регион.
EXEC #mkidx 'IX_SupplierRegion_Supplier_Region', 'dbo.SupplierRegion',
 'CREATE NONCLUSTERED INDEX IX_SupplierRegion_Supplier_Region ON dbo.SupplierRegion(SupplierID, RegionID)
  INCLUDE(IsActive)';
GO

/* ---------- Покупатели / точки доставки ---------- */
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

/* ---------- Журнал аудита ---------- */
EXEC #mkidx 'IX_AuditLog_CreatedAt', 'dbo.AuditLog',
 'CREATE NONCLUSTERED INDEX IX_AuditLog_CreatedAt ON dbo.AuditLog(CreatedAt DESC)
  INCLUDE(LogLevel, Category, Action, Username)';
GO

/* Обновляем статистику по затронутым таблицам (быстро, помогает планировщику). */
BEGIN TRY EXEC('UPDATE STATISTICS dbo.[Order]');        END TRY BEGIN CATCH END CATCH
BEGIN TRY EXEC('UPDATE STATISTICS dbo.OrderItem');       END TRY BEGIN CATCH END CATCH
BEGIN TRY EXEC('UPDATE STATISTICS dbo.SupplierPrice');   END TRY BEGIN CATCH END CATCH
BEGIN TRY EXEC('UPDATE STATISTICS dbo.InvoiceImport');   END TRY BEGIN CATCH END CATCH
GO

DROP PROCEDURE #mkidx;
GO
PRINT '=== Готово. Проверьте сообщения выше: CREATED / EXISTS / SKIP / ERROR ===';
GO
