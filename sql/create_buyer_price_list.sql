/* =====================================================================
   PharmData — таблица назначения прайс-листов покупателям
   ---------------------------------------------------------------------
   Связка BuyerPriceList: какой management-прайс (PriceList) подключён
   какому покупателю (Buyer). Если у покупателя есть активные записи —
   каталог показывает товары только из этих прайсов; если записей нет —
   работает как раньше, по региону (страховка).

   Идемпотентный: повторный запуск ничего не ломает. Данные не трогает.
   Деплой exe нужен отдельно (новые эндпоинты и фильтр каталога в коде).
   ===================================================================== */

SET NOCOUNT ON;

IF OBJECT_ID('dbo.BuyerPriceList') IS NULL
BEGIN
    CREATE TABLE dbo.BuyerPriceList (
        BuyerPriceListID uniqueidentifier NOT NULL CONSTRAINT DF_BuyerPriceList_Id DEFAULT NEWID(),
        BuyerID          uniqueidentifier NOT NULL,
        PriceListID      uniqueidentifier NOT NULL,
        MarkupPct        decimal(6,2)     NOT NULL CONSTRAINT DF_BuyerPriceList_Markup DEFAULT 0,
        IsActive         bit              NOT NULL CONSTRAINT DF_BuyerPriceList_IsActive DEFAULT 1,
        CreatedAt        datetime2(3)     NOT NULL CONSTRAINT DF_BuyerPriceList_CreatedAt DEFAULT GETUTCDATE(),
        CONSTRAINT PK_BuyerPriceList PRIMARY KEY (BuyerPriceListID)
    );
    CREATE UNIQUE INDEX UX_BuyerPriceList_Buyer_Price ON dbo.BuyerPriceList(BuyerID, PriceListID);
    CREATE INDEX IX_BuyerPriceList_PriceList ON dbo.BuyerPriceList(PriceListID) INCLUDE(BuyerID, IsActive);
    PRINT 'BuyerPriceList: таблица создана';
END
ELSE
BEGIN
    -- Таблица уже есть — добавляем колонку индивидуальной наценки клиента, если её нет.
    IF COL_LENGTH('dbo.BuyerPriceList', 'MarkupPct') IS NULL
    BEGIN
        ALTER TABLE dbo.BuyerPriceList
            ADD MarkupPct decimal(6,2) NOT NULL CONSTRAINT DF_BuyerPriceList_Markup DEFAULT 0;
        PRINT 'BuyerPriceList: добавлена колонка MarkupPct';
    END
    ELSE
        PRINT 'BuyerPriceList: уже существует (с MarkupPct), пропущено';
END
