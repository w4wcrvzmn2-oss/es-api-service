-- UPDATE 0 был нормален: PROCESSING уже сброшены при рестартах службы.
-- Пауза автообновления Katren на 7 дней, пока крутится ручной импорт с новым exe.

UPDATE "PriceList"
SET "NextUpdateAt" = ((NOW() AT TIME ZONE 'utc') + INTERVAL '7 days')
WHERE "PriceListID" = '21cdafd8-3a15-45f4-94a7-4e44159445f6'::uuid;

SELECT "PriceListID", "Name", "NextUpdateAt", "LastUpdateAt", "ScheduleCron"
FROM "PriceList"
WHERE "PriceListID" = '21cdafd8-3a15-45f4-94a7-4e44159445f6'::uuid;
