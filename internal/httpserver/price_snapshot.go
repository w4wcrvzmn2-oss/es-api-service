package httpserver

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Дисковый снимок полного прайса (gzip JSON) — главный рывок к пику 45–80 загрузок.
var (
	priceCacheMu   sync.Mutex
	priceCacheBusy = map[string]bool{}
)

func priceCacheDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "price_cache"
	}
	return filepath.Join(filepath.Dir(exe), "price_cache")
}

func priceCachePaths(supplierID string) (gzPath, metaPath string) {
	base := filepath.Join(priceCacheDir(), supplierID+"_latest")
	return base + ".json.gz", base + ".meta.json"
}

type priceCacheMeta struct {
	ETag      string    `json:"etag"`
	Total     int       `json:"total"`
	UpdatedAt time.Time `json:"updated_at"`
	BuiltAt   time.Time `json:"built_at"`
}

func readPriceCacheMeta(supplierID string) (*priceCacheMeta, error) {
	_, metaPath := priceCachePaths(supplierID)
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var m priceCacheMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func writePriceCacheMeta(supplierID string, m priceCacheMeta) error {
	_, metaPath := priceCachePaths(supplierID)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0755); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, b, 0644)
}

func invalidatePriceCache(supplierID string) {
	gz, meta := priceCachePaths(supplierID)
	_ = os.Remove(gz)
	_ = os.Remove(meta)
}

// tryServePriceCache отдаёт готовый .json.gz если ETag совпадает с БД.
// Только для «полного» запроса без limit/delta/region/inactive.
func (s *Server) tryServePriceCache(w http.ResponseWriter, r *http.Request, supplierID, etag string, totalInDB int) bool {
	if r.URL.Query().Get("limit") != "" || r.URL.Query().Get("offset") != "" {
		return false
	}
	if r.URL.Query().Get("updated_since") != "" {
		return false
	}
	if r.URL.Query().Get("region_id") != "" {
		return false
	}
	if r.URL.Query().Get("include_inactive") == "true" {
		return false
	}
	if r.URL.Query().Get("latest_only") == "false" {
		return false
	}
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}

	meta, err := readPriceCacheMeta(supplierID)
	if err != nil || meta.ETag != etag {
		return false
	}
	gzPath, _ := priceCachePaths(supplierID)
	f, err := os.Open(gzPath)
	if err != nil {
		return false
	}
	defer f.Close()

	skipGzipCompression(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, must-revalidate")
	w.Header().Set("X-Price-Cache", "HIT")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
	flushWriter(w)

	if s.logger != nil {
		s.logger.Info("price cache HIT supplier=%s total=%d", supplierID, totalInDB)
	}
	return true
}

func skipGzipCompression(w http.ResponseWriter) {
	for w != nil {
		if s, ok := w.(interface{ SkipCompression() }); ok {
			s.SkipCompression()
			return
		}
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = u.Unwrap()
			continue
		}
		return
	}
}

// openPriceCacheWriter создаёт временный .json.gz для записи снимка параллельно с ответом.
func openPriceCacheWriter(supplierID string) (io.WriteCloser, string, error) {
	if err := os.MkdirAll(priceCacheDir(), 0755); err != nil {
		return nil, "", err
	}
	gzPath, _ := priceCachePaths(supplierID)
	tmp := gzPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, "", err
	}
	gz := gzip.NewWriter(f)
	return &priceCacheWriteCloser{gz: gz, f: f, tmp: tmp, final: gzPath}, tmp, nil
}

type priceCacheWriteCloser struct {
	gz    *gzip.Writer
	f     *os.File
	tmp   string
	final string
}

func (p *priceCacheWriteCloser) Write(b []byte) (int, error) {
	return p.gz.Write(b)
}

func (p *priceCacheWriteCloser) Close() error {
	err1 := p.gz.Close()
	err2 := p.f.Close()
	if err1 != nil {
		_ = os.Remove(p.tmp)
		return err1
	}
	if err2 != nil {
		_ = os.Remove(p.tmp)
		return err2
	}
	_ = os.Remove(p.final)
	return os.Rename(p.tmp, p.final)
}

func (p *priceCacheWriteCloser) Abort() {
	_ = p.gz.Close()
	_ = p.f.Close()
	_ = os.Remove(p.tmp)
}

// scheduleRebuildPriceCache строит снимок в фоне (после импорта/матчинга).
func (s *Server) scheduleRebuildPriceCache(supplierID string) {
	if supplierID == "" {
		return
	}
	priceCacheMu.Lock()
	if priceCacheBusy[supplierID] {
		priceCacheMu.Unlock()
		return
	}
	priceCacheBusy[supplierID] = true
	priceCacheMu.Unlock()

	go func() {
		defer func() {
			priceCacheMu.Lock()
			delete(priceCacheBusy, supplierID)
			priceCacheMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.buildPriceCache(ctx, supplierID); err != nil {
			if s.logger != nil {
				s.logger.Warn("price cache rebuild failed supplier=%s: %v", supplierID, err)
			}
			return
		}
		if s.logger != nil {
			s.logger.Info("price cache rebuilt supplier=%s", supplierID)
		}
	}()
}

func (s *Server) scheduleRebuildPriceCacheByImportPoint(importPointID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var supplierID sql.NullString
	err := s.database.GORMWith(ctx).Raw(`
		SELECT CAST(COALESCE(pl.SupplierID, ip.SupplierID) AS TEXT)
		FROM ImportPoint ip
		LEFT JOIN PriceList pl ON pl.ImportPointID = ip.ImportPointID AND pl.IsActive = 1
		WHERE ip.ImportPointID = CAST(@id AS UUID)
		LIMIT 1
	`, sql.Named("id", importPointID)).Row().Scan(&supplierID)
	if err != nil || !supplierID.Valid || supplierID.String == "" {
		return
	}
	invalidatePriceCache(supplierID.String)
	s.scheduleRebuildPriceCache(supplierID.String)
}

func (s *Server) buildPriceCache(ctx context.Context, supplierID string) error {
	// Переиспользуем тот же SQL, что и handleGetSupplierPrices (latest, active).
	supplierWhere := `sp.InvoiceImportID IN (
			SELECT lii.InvoiceImportID FROM InvoiceImport lii
			WHERE lii.ImportPointID IN (
				SELECT pl.ImportPointID FROM PriceList pl
				WHERE pl.SupplierID = CAST(@supplierID AS UUID) AND pl.IsActive = 1
			)
			AND lii.ImportStatus = 'COMPLETED'
			AND lii.CompletedAt = (
				SELECT MAX(lii2.CompletedAt) FROM InvoiceImport lii2
				WHERE lii2.ImportPointID = lii.ImportPointID AND lii2.ImportStatus = 'COMPLETED'
			)
		)`

	var totalInDB, totalNullGuid, totalEmptyGuid int
	var maxUpdated sql.NullTime
	statsQuery := fmt.Sprintf(`
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN sp.GUID_ES IS NOT NULL AND CAST(sp.GUID_ES AS TEXT) = '' THEN 1 ELSE 0 END), 0),
			MAX(sp.UpdatedAt)
		FROM SupplierPrice sp
		WHERE %s AND sp.IsActive = 1
	`, supplierWhere)
	if err := s.database.GORMWith(ctx).Raw(statsQuery, sql.Named("supplierID", supplierID)).Row().Scan(
		&totalInDB, &totalNullGuid, &totalEmptyGuid, &maxUpdated); err != nil {
		return err
	}
	maxUp := time.Time{}
	if maxUpdated.Valid {
		maxUp = maxUpdated.Time
	}
	etag := priceListETag(supplierID, totalInDB, maxUp)

	query := fmt.Sprintf(`
		SELECT
			CAST(sp.SupplierPriceID AS TEXT),
			CAST(sp.SupplierID AS TEXT),
			CASE WHEN sp.GUID_ES IS NULL THEN NULL ELSE CAST(sp.GUID_ES AS TEXT) END,
			ef2.NAME, ef2.INN_NAME_RUS, ef2.CUREFORM_NAME, ef2.BARCODE,
			sp.ItemCode, sp.ItemName, sp.Price, sp.Quantity,
			sp.InvoiceNumber, sp.InvoiceDate, sp.BatchNumber, sp.ExpiryDate,
			sp.MatchMethod, sp.MatchConfidence, sp.InvoiceDate,
			sp.IsActive, CAST(sp.RegionID AS TEXT), r.Name,
			COALESCE(sp.MarkupPct, 0),
			sp.Price * (1 + COALESCE(sp.MarkupPct, 0) / 100.0),
			sp.CreatedAt, CAST(NULL AS TEXT)
		FROM SupplierPrice sp
		LEFT JOIN es_ef2 ef2 ON sp.GUID_ES = ef2.GUID_ES
		LEFT JOIN Region r ON sp.RegionID = r.RegionID
		WHERE %s AND sp.IsActive = 1
		ORDER BY CASE WHEN ef2.NAME IS NULL THEN 1 ELSE 0 END, ef2.NAME, sp.ItemName, sp.InvoiceDate DESC, sp.Price DESC
	`, supplierWhere)

	rows, err := s.database.GORMWith(ctx).Raw(query, sql.Named("supplierID", supplierID)).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	wc, _, err := openPriceCacheWriter(supplierID)
	if err != nil {
		return err
	}
	pcw := wc.(*priceCacheWriteCloser)

	dbUnmatched := totalNullGuid + totalEmptyGuid
	dbMatched := totalInDB - dbUnmatched
	if _, err := fmt.Fprintf(pcw, `{"supplier_id":%q,"total_prices":%d,"stats":{"total_in_db":%d,"matched_final":%d,"unmatched_final":%d,"prices_returned":%d},"prices":`,
		supplierID, totalInDB, totalInDB, dbMatched, dbUnmatched, totalInDB); err != nil {
		pcw.Abort()
		return err
	}

	streamer := NewJSONStreamer(pcw)
	if err := streamer.WriteArrayStart(); err != nil {
		pcw.Abort()
		return err
	}

	type rowView struct {
		SupplierPriceID  string   `json:"supplier_price_id"`
		SupplierID       string   `json:"supplier_id"`
		GUID_ES          *string  `json:"guid_es,omitempty"`
		DrugName         *string  `json:"drug_name,omitempty"`
		INN              *string  `json:"inn,omitempty"`
		CureForm         *string  `json:"cure_form,omitempty"`
		Barcode          *string  `json:"barcode,omitempty"`
		ProducerName     *string  `json:"producer_name,omitempty"`
		SupplierItemCode *string  `json:"supplier_item_code,omitempty"`
		SupplierItemName *string  `json:"supplier_item_name,omitempty"`
		Quantity         *float64 `json:"quantity,omitempty"`
		InvoiceNumber    *string  `json:"invoice_number,omitempty"`
		InvoiceDate      *string  `json:"invoice_date,omitempty"`
		BatchNumber      *string  `json:"batch_number,omitempty"`
		ExpiryDate       *string  `json:"expiry_date,omitempty"`
		MatchMethod      *string  `json:"match_method,omitempty"`
		MatchConfidence  *float64 `json:"match_confidence,omitempty"`
		IsActive         bool     `json:"is_active"`
		RegionID         *string  `json:"region_id,omitempty"`
		RegionName       *string  `json:"region_name,omitempty"`
		FinalPrice       *float64 `json:"price"`
		LastPriceDate    *string  `json:"last_price_date,omitempty"`
		CreatedAt        string   `json:"created_at"`
	}

	for rows.Next() {
		var sp rowView
		var guidES, drugName, inn, cureForm, barcode, producerName, itemCode, itemName sql.NullString
		var invoiceNumber, batchNumber, matchMethod, regionID, regionName sql.NullString
		var invoiceDate, expiryDate, lastPriceDate sql.NullTime
		var quantity, matchConfidence, basePrice, finalPrice sql.NullFloat64
		if err := rows.Scan(
			&sp.SupplierPriceID, &sp.SupplierID, &guidES,
			&drugName, &inn, &cureForm, &barcode,
			&itemCode, &itemName, &basePrice, &quantity,
			&invoiceNumber, &invoiceDate, &batchNumber, &expiryDate,
			&matchMethod, &matchConfidence, &lastPriceDate,
			&sp.IsActive, &regionID, &regionName,
			&sql.NullFloat64{}, &finalPrice, &sp.CreatedAt, &producerName,
		); err != nil {
			continue
		}
		if guidES.Valid && strings.TrimSpace(guidES.String) != "" {
			sp.GUID_ES = &guidES.String
		}
		if drugName.Valid {
			sp.DrugName = &drugName.String
		}
		if inn.Valid {
			sp.INN = &inn.String
		}
		if cureForm.Valid {
			sp.CureForm = &cureForm.String
		}
		if barcode.Valid {
			sp.Barcode = &barcode.String
		}
		if producerName.Valid {
			sp.ProducerName = &producerName.String
		}
		if itemCode.Valid {
			sp.SupplierItemCode = &itemCode.String
		}
		if itemName.Valid {
			sp.SupplierItemName = &itemName.String
		}
		if quantity.Valid {
			q := quantity.Float64
			sp.Quantity = &q
		}
		if invoiceNumber.Valid {
			sp.InvoiceNumber = &invoiceNumber.String
		}
		if invoiceDate.Valid {
			sp.InvoiceDate = stringPtr(invoiceDate.Time.Format(time.RFC3339))
		}
		if batchNumber.Valid {
			sp.BatchNumber = &batchNumber.String
		}
		if expiryDate.Valid {
			sp.ExpiryDate = stringPtr(expiryDate.Time.Format(time.RFC3339))
		}
		if matchMethod.Valid {
			sp.MatchMethod = &matchMethod.String
		}
		if matchConfidence.Valid {
			sp.MatchConfidence = &matchConfidence.Float64
		}
		if lastPriceDate.Valid {
			sp.LastPriceDate = stringPtr(lastPriceDate.Time.Format(time.RFC3339))
		}
		if regionID.Valid {
			sp.RegionID = &regionID.String
		}
		if regionName.Valid {
			sp.RegionName = &regionName.String
		}
		if finalPrice.Valid {
			sp.FinalPrice = &finalPrice.Float64
		}
		if err := streamer.WriteItem(sp); err != nil {
			pcw.Abort()
			return err
		}
	}
	if err := streamer.WriteArrayEnd(); err != nil {
		pcw.Abort()
		return err
	}
	if _, err := pcw.Write([]byte("}")); err != nil {
		pcw.Abort()
		return err
	}
	if err := pcw.Close(); err != nil {
		return err
	}
	return writePriceCacheMeta(supplierID, priceCacheMeta{
		ETag:      etag,
		Total:     totalInDB,
		UpdatedAt: maxUp,
		BuiltAt:   time.Now().UTC(),
	})
}
