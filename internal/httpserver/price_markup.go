package httpserver

import (
	"context"
	"fmt"
	"net/http"

	"es_api_service/internal/db"

	"github.com/google/uuid"
)

// pricingContext — покупатель и регион для расчёта итоговой цены.
// Пустые поля = соответствующая наценка 0.
type pricingContext struct {
	BuyerID  string
	RegionID string
}

// resolvePricingContext определяет BuyerID / RegionID из JWT и query ?region_id=.
// region_id из запроса имеет приоритет над регионом покупателя.
func (s *Server) resolvePricingContext(ctx context.Context, r *http.Request) pricingContext {
	out := pricingContext{}
	if r != nil {
		if rid := r.URL.Query().Get("region_id"); rid != "" {
			if _, err := uuid.Parse(rid); err == nil {
				out.RegionID = rid
			}
		}
	}

	buyerUserID, _, ok := s.resolveBuyerUser(ctx, r)
	if !ok || buyerUserID == "" || s.database == nil {
		return out
	}

	type row struct {
		BuyerID  string
		RegionID *string
	}
	var res row
	err := s.database.GORMWith(ctx).Raw(`
		SELECT
			CAST(bu."BuyerID" AS TEXT) AS "BuyerID",
			CASE WHEN b."RegionID" IS NULL THEN NULL ELSE CAST(b."RegionID" AS TEXT) END AS "RegionID"
		FROM "BuyerUser" bu
		INNER JOIN "Buyer" b ON b."BuyerID" = bu."BuyerID"
		WHERE bu."BuyerUserID" = CAST(? AS UUID)
		LIMIT 1
	`, db.UUIDParam(buyerUserID)).Scan(&res).Error
	if err != nil || res.BuyerID == "" {
		return out
	}
	out.BuyerID = res.BuyerID
	if out.RegionID == "" && res.RegionID != nil {
		if _, err := uuid.Parse(*res.RegionID); err == nil {
			out.RegionID = *res.RegionID
		}
	}
	return out
}

func sqlUUIDOrNull(id string) string {
	if id == "" {
		return "NULL"
	}
	if _, err := uuid.Parse(id); err != nil {
		return "NULL"
	}
	return fmt.Sprintf("CAST('%s' AS UUID)", id)
}

// sqlAdditiveFinalPriceExpr — итоговая цена от базовой:
//
//	Price * (1 + (наценка_прайса + наценка_региона + наценка_клиента) / 100)
//
// Наценки могут быть отрицательными (скидка) и положительными.
// IDs встраиваются только после uuid.Parse.
func sqlAdditiveFinalPriceExpr(alias string, pc pricingContext) string {
	if alias == "" {
		alias = "sp"
	}
	buyerLit := sqlUUIDOrNull(pc.BuyerID)
	regionLit := sqlUUIDOrNull(pc.RegionID)
	// Не используем GREATEST: PreparePGSQL/QuotePascalSQL может закавычить
	// ALL_CAPS имя и PostgreSQL перестанет видеть встроенную функцию.
	raw := `(` + alias + `."Price" * (1 + (
		COALESCE(` + alias + `."MarkupPct", 0)
		+ COALESCE((
			SELECT plr."MarkupPct"
			FROM "PriceListRegion" plr
			WHERE plr."PriceListID" = ` + alias + `."PriceListID"
			  AND ` + regionLit + ` IS NOT NULL
			  AND plr."RegionID" = ` + regionLit + `
			  AND plr."IsActive" = TRUE
			LIMIT 1
		), 0)
		+ COALESCE((
			SELECT bpl."MarkupPct"
			FROM "BuyerPriceList" bpl
			WHERE ` + buyerLit + ` IS NOT NULL
			  AND bpl."BuyerID" = ` + buyerLit + `
			  AND bpl."IsActive" = TRUE
			  AND (
				bpl."PriceListID" = ` + alias + `."PriceListID"
				OR EXISTS (
					SELECT 1
					FROM "PriceList" pl
					INNER JOIN "InvoiceImport" ii ON ii."InvoiceImportID" = ` + alias + `."InvoiceImportID"
					WHERE pl."PriceListID" = bpl."PriceListID"
					  AND pl."ImportPointID" IS NOT NULL
					  AND ii."ImportPointID" = pl."ImportPointID"
					  AND pl."SupplierID" = ` + alias + `."SupplierID"
				)
			  )
			LIMIT 1
		), 0)
	) / 100.0))`
	return `(CASE WHEN ` + raw + ` < 0 THEN 0::numeric ELSE ` + raw + ` END)`
}