package httpserver

import (
	"fmt"
	"net/http"
	"time"
)

func flushWriter(w http.ResponseWriter) {
	for w != nil {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			return
		}
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = u.Unwrap()
			continue
		}
		return
	}
}

func unwrapGzip(w http.ResponseWriter) (*GzipResponseWriter, bool) {
	for w != nil {
		if g, ok := w.(*GzipResponseWriter); ok {
			return g, true
		}
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = u.Unwrap()
			continue
		}
		return nil, false
	}
	return nil, false
}

func priceListETag(supplierID string, total int, maxUpdated time.Time) string {
	ts := int64(0)
	if !maxUpdated.IsZero() {
		ts = maxUpdated.UTC().Unix()
	}
	short := supplierID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf(`"sp-%s-%d-%d"`, short, total, ts)
}
