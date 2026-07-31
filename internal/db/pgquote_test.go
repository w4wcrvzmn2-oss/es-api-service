package db

import (
	"database/sql"
	"strings"
	"testing"
)

func TestQuotePascalSQL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			`UPDATE InvoiceImport SET ImportStatus = 'FAILED' WHERE ImportStatus = 'PROCESSING'`,
			`UPDATE "InvoiceImport" SET "ImportStatus" = 'FAILED' WHERE "ImportStatus" = 'PROCESSING'`,
		},
		{
			`SELECT pl.PriceListID FROM PriceList pl INNER JOIN ImportPoint ip ON pl.ImportPointID = ip.ImportPointID`,
			`SELECT pl."PriceListID" FROM "PriceList" pl INNER JOIN "ImportPoint" ip ON pl."ImportPointID" = ip."ImportPointID"`,
		},
		{
			`SELECT "PriceListID" FROM "PriceList"`,
			`SELECT "PriceListID" FROM "PriceList"`,
		},
		{
			`SELECT COUNT(*) FROM OrderStatus WHERE Name = 'Draft'`,
			`SELECT COUNT(*) FROM "OrderStatus" WHERE "Name" = 'Draft'`,
		},
		{
			`CAST(@priceListID AS UUID)`,
			`CAST(@priceListID AS UUID)`,
		},
		{
			`(NOW() AT TIME ZONE 'utc')`,
			`(NOW() AT TIME ZONE 'utc')`,
		},
		{
			`CAST(COALESCE(pl.DefaultMarkupPct, 0) AS FLOAT) AS DefaultMarkupPct`,
			`CAST(COALESCE(pl."DefaultMarkupPct", 0) AS FLOAT) AS "DefaultMarkupPct"`,
		},
		{
			`LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES LEFT JOIN es_producer ep ON ep.KOD_PRODUCER = ef2.PRODUCER_COD`,
			`LEFT JOIN "es_ef2" ef2 ON sp."GUID_ES" = ef2."GUID_ES" LEFT JOIN "ES_PRODUCER" ep ON ep."KOD_PRODUCER" = ef2."PRODUCER_COD"`,
		},
		{
			`INSERT INTO Supplier (SupplierID, Name, INN, GUID_ES, IsActive) VALUES (1, 'a', @inn, @g, 1)`,
			`INSERT INTO "Supplier" ("SupplierID", "Name", "INN", "GUID_ES", "IsActive") VALUES (1, 'a', @inn, @g, 1)`,
		},
		{
			`SELECT GUID_ES, INN FROM ES_DRUG WHERE GUID_ES IS NOT NULL`,
			`SELECT "GUID_ES", "INN" FROM "ES_DRUG" WHERE "GUID_ES" IS NOT NULL`,
		},
	}
	for _, c := range cases {
		got := QuotePascalSQL(c.in)
		if got != c.want {
			t.Errorf("\nin:   %s\ngot:  %s\nwant: %s", c.in, got, c.want)
		}
	}
}

func TestPreparePGSQL_Bool(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			`WHERE pl.IsActive = 1 AND ip.IsActive = 1`,
			`WHERE pl."IsActive" = TRUE AND ip."IsActive" = TRUE`,
		},
		{
			`UPDATE PriceList SET IsActive = 0`,
			`UPDATE "PriceList" SET "IsActive" = FALSE`,
		},
		{
			`SET IsActive = CASE WHEN IsActive = 1 THEN 0 ELSE 1 END`,
			`SET "IsActive" = NOT "IsActive"`,
		},
		{
			`WHERE is_active = 1 AND IsProcessed = 0`,
			`WHERE is_active = TRUE AND "IsProcessed" = FALSE`,
		},
		{
			`AND IsActive=1`,
			`AND "IsActive" = TRUE`,
		},
		{
			`VALUES (@itemCode, COALESCE(@price, 0), COALESCE(@IsActive, 0))`,
			`VALUES (@itemCode, COALESCE(@price, 0), COALESCE(@"IsActive", FALSE))`,
		},
	}
	for _, c := range cases {
		got := PreparePGSQL(c.in)
		if got != c.want {
			t.Errorf("\nin:   %s\ngot:  %s\nwant: %s", c.in, got, c.want)
		}
	}
}

func TestPreparePGSQL_MSSQL(t *testing.T) {
	in := `UPDATE PriceList SET UpdatedAt=GETDATE(), IsActive=1 WHERE CAST(x AS NVARCHAR(50)) LIKE @q FROM [Order]`
	got := PreparePGSQL(in)
	if !strings.Contains(got, `(NOW() AT TIME ZONE 'utc')`) {
		t.Fatalf("GETDATE not replaced: %s", got)
	}
	if strings.Contains(strings.ToUpper(got), "GETDATE") {
		t.Fatalf("GETDATE still present: %s", got)
	}
	if strings.Contains(strings.ToUpper(got), "NVARCHAR") {
		t.Fatalf("NVARCHAR still present: %s", got)
	}
	if strings.Contains(got, "[Order]") {
		t.Fatalf("[Order] still present: %s", got)
	}
	if !strings.Contains(got, `= TRUE`) {
		t.Fatalf("bool not normalized: %s", got)
	}
}

func TestPreparePGSQL_DateAddAndLen(t *testing.T) {
	in := `WHERE StartedAt <= DATEADD(minute, -30, (NOW() AT TIME ZONE 'utc')) AND LEN(BARCODE) > 0 AND name LIKE '%'+@search+'%'`
	got := PreparePGSQL(in)
	if strings.Contains(strings.ToUpper(got), "DATEADD") {
		t.Fatalf("DATEADD still present: %s", got)
	}
	if !strings.Contains(got, `INTERVAL '-30 minutes'`) {
		t.Fatalf("expected interval rewrite: %s", got)
	}
	if strings.Contains(got, "LEN(") && !strings.Contains(got, "LENGTH(") {
		t.Fatalf("LEN not rewritten: %s", got)
	}
	if strings.Contains(got, `'+@`) || strings.Contains(got, `+'%'`) {
		t.Fatalf("plus-concat not rewritten: %s", got)
	}
}

func TestExecRawPrepare(t *testing.T) {
	q, args := prepareRawSQL(
		`DELETE FROM DBFFieldMapping WHERE ImportPointID = CAST(@pointID AS UUID)`,
		sql.Named("pointID", "11111111-1111-1111-1111-111111111111"),
	)
	if !strings.Contains(q, `$1`) {
		t.Fatalf("expected $1, got %s", q)
	}
	if !strings.Contains(q, `"DBFFieldMapping"`) {
		t.Fatalf("expected quoted table, got %s", q)
	}
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
}
