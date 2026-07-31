package httpserver

import (
	"strings"
	"testing"
)

func TestSqlAdditiveFinalPriceExpr_SumsThreeMarkups(t *testing.T) {
	pc := pricingContext{
		BuyerID:  "11111111-1111-1111-1111-111111111111",
		RegionID: "22222222-2222-2222-2222-222222222222",
	}
	expr := sqlAdditiveFinalPriceExpr("sp", pc)
	for _, want := range []string{
		`sp."Price"`,
		`sp."MarkupPct"`,
		`"PriceListRegion"`,
		`"BuyerPriceList"`,
		`11111111-1111-1111-1111-111111111111`,
		`22222222-2222-2222-2222-222222222222`,
		`/ 100.0`,
		`CASE WHEN`,
		`0::numeric`,
	} {
		if !strings.Contains(expr, want) {
			t.Fatalf("expr missing %q:\n%s", want, expr)
		}
	}
}

func TestSqlUUIDOrNull(t *testing.T) {
	if got := sqlUUIDOrNull(""); got != "NULL" {
		t.Fatalf("empty => NULL, got %s", got)
	}
	if got := sqlUUIDOrNull("not-a-uuid"); got != "NULL" {
		t.Fatalf("bad uuid => NULL, got %s", got)
	}
	got := sqlUUIDOrNull("11111111-1111-1111-1111-111111111111")
	if !strings.Contains(got, "CAST('11111111-1111-1111-1111-111111111111' AS UUID)") {
		t.Fatalf("unexpected: %s", got)
	}
}
