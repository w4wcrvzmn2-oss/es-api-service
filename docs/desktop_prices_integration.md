# Десктоп: как читать прайсы и наценки клиента

Модель наценок:
- **Базовая цена** позиции — `SupplierPrice.Price`.
- **Наценка прайса** (общая, задаётся на странице прайса) — `PriceList.DefaultMarkupPct`.
- **Наценка клиента** (индивидуальная, задаётся в карточке покупателя) — `BuyerPriceList.MarkupPct`.

Итоговая цена (десктоп считает сам):
```
Final = Price * (1 + (PriceListMarkupPct + ClientMarkupPct) / 100)
```

Все запросы — от лица покупателя, под которым вошли. Сначала берём `BuyerID` по логину:
```sql
-- @login — то, что ввели в поле «Логин (e-mail)» при входе
SELECT CAST(BuyerID AS NVARCHAR(50)) AS BuyerID
FROM BuyerUser
WHERE Email = @login AND IsActive = 1;
```

---

## Шаг 1. Подключён ли клиент к прайсам?

```sql
SELECT COUNT(*) AS Cnt
FROM BuyerPriceList
WHERE BuyerID = @BuyerID AND IsActive = 1;
```

- `Cnt = 0` → **НЕ подключён** → идём по сценарию A (по региону).
- `Cnt > 0` → **подключён** → идём по сценарию B (только назначенные прайсы).

---

## Сценарий A — клиент НЕ подключён ни к одному прайсу

Показываем всё активное по региону клиента (как работает сайт-каталог сейчас).
Индивидуальной наценки нет (`ClientMarkupPct = 0`), применяется только наценка прайса.

```sql
-- Регион клиента
DECLARE @RegionID uniqueidentifier =
    (SELECT RegionID FROM Buyer WHERE BuyerID = @BuyerID);

SELECT
    CAST(sp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
    sp.ItemName,
    sp.Price                                   AS BasePrice,
    CAST(ISNULL(plr.MarkupPct, 0) AS FLOAT)    AS PriceListMarkupPct,  -- наценка прайса на регион
    0                                          AS ClientMarkupPct
FROM SupplierPrice sp
LEFT JOIN PriceListRegion plr
       ON plr.PriceListID = sp.PriceListID
      AND plr.RegionID   = sp.RegionID
      AND plr.IsActive   = 1
WHERE sp.IsActive = 1
  AND (@RegionID IS NULL OR sp.RegionID = @RegionID);
```

Пример результата (JSON):
```json
[
  { "SupplierPriceID": "A1B2...", "ItemName": "Парацетамол 500мг", "BasePrice": 100.00, "PriceListMarkupPct": 10, "ClientMarkupPct": 0 },
  { "SupplierPriceID": "C3D4...", "ItemName": "Ибупрофен 200мг",   "BasePrice": 80.00,  "PriceListMarkupPct": 10, "ClientMarkupPct": 0 }
]
```
Цена к показу: `100 * (1 + (10 + 0)/100) = 110.00`.

---

## Сценарий B — клиент подключён к прайсам

### Шаг 2. Какие прайсы подключены и с какой наценкой

```sql
SELECT
    CAST(bpl.PriceListID AS NVARCHAR(50))       AS PriceListID,
    pl.Name                                     AS PriceListName,
    CAST(ISNULL(pl.DefaultMarkupPct,0) AS FLOAT) AS PriceListMarkupPct,  -- наценка прайса
    CAST(ISNULL(bpl.MarkupPct,0)       AS FLOAT) AS ClientMarkupPct       -- наценка клиента
FROM BuyerPriceList bpl
INNER JOIN PriceList pl ON pl.PriceListID = bpl.PriceListID
WHERE bpl.BuyerID = @BuyerID AND bpl.IsActive = 1;
```

Пример результата (JSON):
```json
[
  { "PriceListID": "E5F6...", "PriceListName": "Прайс поставщика А", "PriceListMarkupPct": 10, "ClientMarkupPct": 5 }
]
```

### Шаг 3. Позиции конкретного подключённого прайса

Связь строки прайса с прайс-листом: прямой `PriceListID` ИЛИ цепочка
`ImportPoint → InvoiceImport` (та же логика, что на сайте).

```sql
SELECT
    CAST(sp.SupplierPriceID AS NVARCHAR(50)) AS SupplierPriceID,
    sp.ItemName,
    sp.Price AS BasePrice
FROM SupplierPrice sp
LEFT  JOIN InvoiceImport ii ON ii.InvoiceImportID = sp.InvoiceImportID
INNER JOIN PriceList     pl ON pl.PriceListID     = @PriceListID
WHERE sp.IsActive = 1
  AND (
        sp.PriceListID = pl.PriceListID
        OR (pl.ImportPointID IS NOT NULL
            AND ii.ImportPointID = pl.ImportPointID
            AND pl.SupplierID = sp.SupplierID)
      );
```

Итог: берёте `BasePrice` из шага 3, `PriceListMarkupPct` и `ClientMarkupPct` из шага 2 (по этому же `PriceListID`), считаете:
```
Final = BasePrice * (1 + (PriceListMarkupPct + ClientMarkupPct) / 100)
```
Пример: `100 * (1 + (10 + 5)/100) = 115.00`.

---

## Кратко (алгоритм десктопа)

1. По логину → `BuyerID`.
2. `COUNT(*)` в `BuyerPriceList` по `BuyerID`.
3. Если `0` → сценарий A (по региону, наценка клиента = 0).
4. Если `> 0` → для каждого подключённого прайса: берём его наценку прайса + наценку клиента (шаг 2) и позиции (шаг 3), считаем `Final`.

> Примечание. Сайт-каталог использует `ISNULL(sp.FinalPrice, sp.Price)` и наценку по региону
> (`PriceListRegion.MarkupPct`). Если нужно, чтобы десктоп показывал ровно те же цифры,
> что и сайт, берите базой `ISNULL(sp.FinalPrice, sp.Price)` и сверху добавляйте только
> `ClientMarkupPct`. Формула выше (Price + наценка прайса + наценка клиента) — под вашу
> модель «база плюс/минус наценка».
