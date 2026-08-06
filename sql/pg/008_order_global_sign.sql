-- Global order numbers: EX-0000001 … never reused across all pharmacies.
-- Applied on production: elfisa DB.

ALTER TABLE "Order"
  ADD COLUMN IF NOT EXISTS "GlobalSign" varchar(16);

CREATE UNIQUE INDEX IF NOT EXISTS "ux_order_global_sign"
  ON "Order" ("GlobalSign")
  WHERE "GlobalSign" IS NOT NULL;

CREATE TABLE IF NOT EXISTS "GlobalSignCounter" (
  "ID" integer PRIMARY KEY CHECK ("ID" = 1),
  "NextValue" bigint NOT NULL DEFAULT 1
);

INSERT INTO "GlobalSignCounter" ("ID", "NextValue")
VALUES (1, 1)
ON CONFLICT ("ID") DO NOTHING;

-- Backfill existing orders that have no GlobalSign (stable by CreatedAt).
WITH numbered AS (
  SELECT o."OrderID",
         row_number() OVER (ORDER BY o."CreatedAt", o."OrderID") AS n
  FROM "Order" o
  WHERE o."GlobalSign" IS NULL OR TRIM(o."GlobalSign") = ''
)
UPDATE "Order" o
SET "GlobalSign" = 'EX-' || lpad(numbered.n::text, 7, '0')
FROM numbered
WHERE o."OrderID" = numbered."OrderID";

-- Counter continues after max existing EX-####### (and after backfill).
UPDATE "GlobalSignCounter" c
SET "NextValue" = GREATEST(
  c."NextValue",
  COALESCE((
    SELECT MAX(SUBSTRING(o."GlobalSign" FROM 4)::bigint) + 1
    FROM "Order" o
    WHERE o."GlobalSign" ~ '^EX-[0-9]{7}$'
  ), 1)
)
WHERE c."ID" = 1;
