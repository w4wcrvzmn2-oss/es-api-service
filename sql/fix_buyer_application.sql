/* =====================================================================
   PharmData — починка оформления заказа покупателем
   ---------------------------------------------------------------------
   Причина ошибки «Покупатель не найден» при заказе из десктопа:
   оформление требует активной записи BuyerApplication у пользователя
   (resolveBuyerUser делает INNER JOIN BuyerApplication ... IsActive=1),
   а логины, созданные через кабинет управления, её не имеют.

   Этот скрипт создаёт недостающие BuyerApplication для всех активных
   пользователей-покупателей. Идемпотентный: заводит только тем, у кого
   активной заявки ещё нет. Деплой сервиса не требуется.
   ===================================================================== */

SET NOCOUNT ON;

-- 1) Диагностика: у кого нет активной заявки (перед вставкой).
SELECT bu.BuyerUserID, bu.Email, bu.FullName
FROM BuyerUser bu
WHERE bu.IsActive = 1
  AND NOT EXISTS (
      SELECT 1 FROM BuyerApplication ba
      WHERE ba.BuyerUserID = bu.BuyerUserID AND ba.IsActive = 1
  );

-- 2) Создаём недостающие заявки.
INSERT INTO BuyerApplication (BuyerApplicationID, BuyerID, BuyerUserID, IsActive, CreatedAt)
SELECT NEWID(), bu.BuyerID, bu.BuyerUserID, 1, GETUTCDATE()
FROM BuyerUser bu
WHERE bu.IsActive = 1
  AND NOT EXISTS (
      SELECT 1 FROM BuyerApplication ba
      WHERE ba.BuyerUserID = bu.BuyerUserID AND ba.IsActive = 1
  );

PRINT CONCAT('Создано заявок: ', @@ROWCOUNT);
