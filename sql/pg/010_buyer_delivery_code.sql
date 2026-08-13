-- 010: Код доставки покупателя. Попадает в выгрузку заказов и доступен в маппинге шаблона поставщика.
ALTER TABLE "Buyer" ADD COLUMN IF NOT EXISTS "DeliveryCode" text;
