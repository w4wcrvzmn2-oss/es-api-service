/* =====================================================================
   PharmData — таблицы «Выгрузка заказов» поставщика
   ---------------------------------------------------------------------
   SupplierExportConfig — куда и как выгружать заказы (FTP и/или почта,
   каталог, формат). Одна запись на поставщика.
   OrderExportLog — журнал выгрузок (что, когда, успешно ли).

   Идемпотентный: повторный запуск ничего не ломает. Данные не трогает.
   Примечание: пароли FTP/SMTP хранятся как есть (сервер должен ими
   подключаться) — на стадии теста это допустимо, позже можно шифровать.
   ===================================================================== */

SET NOCOUNT ON;

IF OBJECT_ID('dbo.SupplierExportConfig') IS NULL
BEGIN
    CREATE TABLE dbo.SupplierExportConfig (
        SupplierExportConfigID uniqueidentifier NOT NULL CONSTRAINT DF_SupplierExportConfig_Id DEFAULT NEWID(),
        SupplierID   uniqueidentifier NOT NULL,
        Method       nvarchar(10)  NOT NULL CONSTRAINT DF_SupplierExportConfig_Method DEFAULT N'none', -- none|ftp|email|both
        Format       nvarchar(10)  NOT NULL CONSTRAINT DF_SupplierExportConfig_Format DEFAULT N'DBF',
        -- FTP
        FtpHost      nvarchar(255) NULL,
        FtpPort      int           NOT NULL CONSTRAINT DF_SupplierExportConfig_FtpPort DEFAULT 21,
        FtpUser      nvarchar(255) NULL,
        FtpPassword  nvarchar(255) NULL,
        FtpDir       nvarchar(500) NULL,
        -- Почта (получатель + параметры отправки)
        EmailTo      nvarchar(255) NULL,
        SmtpHost     nvarchar(255) NULL,
        SmtpPort     int           NOT NULL CONSTRAINT DF_SupplierExportConfig_SmtpPort DEFAULT 587,
        SmtpUser     nvarchar(255) NULL,
        SmtpPassword nvarchar(255) NULL,
        SmtpFrom     nvarchar(255) NULL,
        IsActive     bit           NOT NULL CONSTRAINT DF_SupplierExportConfig_IsActive DEFAULT 1,
        UpdatedAt    datetime2(3)  NOT NULL CONSTRAINT DF_SupplierExportConfig_UpdatedAt DEFAULT GETUTCDATE(),
        CONSTRAINT PK_SupplierExportConfig PRIMARY KEY (SupplierExportConfigID)
    );
    CREATE UNIQUE INDEX UX_SupplierExportConfig_Supplier ON dbo.SupplierExportConfig(SupplierID);
    PRINT 'SupplierExportConfig: таблица создана';
END
ELSE
    PRINT 'SupplierExportConfig: уже существует, пропущено';

IF OBJECT_ID('dbo.OrderExportLog') IS NULL
BEGIN
    CREATE TABLE dbo.OrderExportLog (
        OrderExportLogID uniqueidentifier NOT NULL CONSTRAINT DF_OrderExportLog_Id DEFAULT NEWID(),
        SupplierID   uniqueidentifier NOT NULL,
        Method       nvarchar(10)  NULL,
        FileName     nvarchar(255) NULL,
        OrdersCount  int           NOT NULL CONSTRAINT DF_OrderExportLog_Count DEFAULT 0,
        Status       nvarchar(20)  NOT NULL CONSTRAINT DF_OrderExportLog_Status DEFAULT N'ok', -- ok|error
        Message      nvarchar(max) NULL,
        CreatedAt    datetime2(3)  NOT NULL CONSTRAINT DF_OrderExportLog_CreatedAt DEFAULT GETUTCDATE(),
        CONSTRAINT PK_OrderExportLog PRIMARY KEY (OrderExportLogID)
    );
    CREATE INDEX IX_OrderExportLog_Supplier_Created ON dbo.OrderExportLog(SupplierID, CreatedAt DESC);
    PRINT 'OrderExportLog: таблица создана';
END
ELSE
    PRINT 'OrderExportLog: уже существует, пропущено';
