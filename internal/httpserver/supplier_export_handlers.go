package httpserver

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"path"
	"sort"
	"strings"
	"time"

	"es_api_service/internal/db"
	"es_api_service/internal/models"
	"es_api_service/internal/orderexport"

	"github.com/google/uuid"
	"github.com/jlaffaye/ftp"
	"gorm.io/gorm"
)

// handleSCExportConfig — GET/PUT /api/sc/export-config
// Настройки выгрузки заказов поставщика (куда и как отправлять).
func (s *Server) handleSCExportConfig(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		var cfg models.SupplierExportConfig
		err := s.database.GORMWith(ctx).
			Where("SupplierID = ?", db.UUIDParam(sid)).
			Take(&cfg).Error
		if err == gorm.ErrRecordNotFound {
			writeJSON(w, http.StatusOK, models.SupplierExportConfig{
				SupplierID: sid,
				Method:     "none",
				Format:     "DBF",
				FtpPort:    21,
				SmtpPort:   587,
				IsActive:   true,
			})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)

	case http.MethodPut:
		var req models.SupplierExportConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный формат запроса"})
			return
		}

		switch req.Method {
		case "ftp", "email", "both", "none":
		default:
			req.Method = "none"
		}
		req.Format = "DBF"
		if req.FtpPort <= 0 {
			req.FtpPort = 21
		}
		if req.SmtpPort <= 0 {
			req.SmtpPort = 587
		}
		req.SupplierID = sid
		req.UpdatedAt = time.Now().UTC()

		err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.Exec(`DELETE FROM SupplierExportConfig WHERE SupplierID = ?`, db.UUIDParam(sid)).Error; e != nil {
				return e
			}
			req.SupplierExportConfigID = uuid.New().String()
			return tx.Create(&req).Error
		})
		if err != nil {
			if s.logger != nil {
				s.logger.Error("Ошибка сохранения настроек выгрузки поставщика %s: %v", sid, err)
			}
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка сохранения настроек"})
			return
		}
		writeJSON(w, http.StatusOK, req)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Метод не поддерживается"})
	}
}

// handleSCOrderDelivery — совместимость со старой страницей order-delivery.html.
// Читает/пишет те же настройки, что /api/sc/export-config, в старом JSON-формате.
func (s *Server) handleSCOrderDelivery(w http.ResponseWriter, r *http.Request) {
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	switch r.Method {
	case http.MethodGet:
		var cfg models.SupplierExportConfig
		err := s.database.GORMWith(ctx).Where("SupplierID = ?", db.UUIDParam(sid)).Take(&cfg).Error
		if err == gorm.ErrRecordNotFound {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"format": "DBF", "method": "Email",
				"ftp_host": "", "ftp_path": "", "ftp_user": "", "ftp_password": "",
				"ftp_passive": true, "email": "",
			})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		method := "Email"
		switch cfg.Method {
		case "ftp":
			method = "FTP"
		case "both":
			method = "FTP"
		case "email":
			method = "Email"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"format":       "DBF",
			"method":       method,
			"ftp_host":     derefStr(cfg.FtpHost),
			"ftp_path":     derefStr(cfg.FtpDir),
			"ftp_user":     derefStr(cfg.FtpUser),
			"ftp_password": derefStr(cfg.FtpPassword),
			"ftp_passive":  true,
			"email":        derefStr(cfg.EmailTo),
		})

	case http.MethodPut:
		var body struct {
			Format      string `json:"format"`
			Method      string `json:"method"`
			FtpHost     string `json:"ftp_host"`
			FtpPath     string `json:"ftp_path"`
			FtpUser     string `json:"ftp_user"`
			FtpPassword string `json:"ftp_password"`
			Email       string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный формат запроса"})
			return
		}
		method := "none"
		switch strings.ToUpper(body.Method) {
		case "FTP", "FTP_ELFISA":
			method = "ftp"
		case "EMAIL":
			method = "email"
		}
		cfg := models.SupplierExportConfig{
			SupplierExportConfigID: uuid.New().String(),
			SupplierID:             sid,
			Method:                 method,
			Format:                 "DBF",
			FtpPort:                21,
			SmtpPort:               587,
			IsActive:               true,
			UpdatedAt:              time.Now().UTC(),
		}
		if body.FtpHost != "" {
			cfg.FtpHost = &body.FtpHost
		}
		if body.FtpPath != "" {
			cfg.FtpDir = &body.FtpPath
		}
		if body.FtpUser != "" {
			cfg.FtpUser = &body.FtpUser
		}
		if body.FtpPassword != "" {
			cfg.FtpPassword = &body.FtpPassword
		}
		if body.Email != "" {
			cfg.EmailTo = &body.Email
		}
		err := s.database.GORMWith(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.Exec(`DELETE FROM SupplierExportConfig WHERE SupplierID = ?`, db.UUIDParam(sid)).Error; e != nil {
				return e
			}
			return tx.Create(&cfg).Error
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка сохранения настроек"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Метод не поддерживается"})
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// handleSCOrdersExport — POST /api/sc/orders/export
// Формирует отдельный DBF на каждый заказ за период; при нескольких — ZIP.
// Опционально шлёт на FTP (каждый DBF отдельно) / почту (ZIP или один DBF), всегда отдаёт файл на скачивание.
func (s *Server) handleSCOrdersExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "POST only"})
		return
	}
	sid := s.supplierIDFromClaims(r)
	if sid == "" {
		writeJSON(w, http.StatusForbidden, ErrorResponse{Error: "supplier_id не найден"})
		return
	}

	from := r.URL.Query().Get("date_from")
	to := r.URL.Query().Get("date_to")
	if from == "" {
		from = time.Now().UTC().AddDate(0, -1, 0).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	type row struct {
		OrderID      string
		OrderDate    time.Time
		Buyer        string
		Address      string
		Code         sql.NullString
		Name         sql.NullString
		SuppName     sql.NullString
		Barcode      sql.NullString
		Manufacturer sql.NullString
		Country      sql.NullString
		Series       sql.NullString
		Batch        sql.NullString
		Expiry       sql.NullTime
		Qty          float64
		Price        float64
	}
	var rows []row
	err := s.database.GORMWith(ctx).Raw(`
		SELECT
			CAST(o.OrderID AS TEXT) AS OrderID,
			o.CreatedAt AS OrderDate,
			b.Name AS Buyer,
			COALESCE(bl.Address, '') AS Address,
			COALESCE(NULLIF(LTRIM(RTRIM(oi.ItemCode)), ''), sp.ItemCode, spfb.ItemCode) AS Code,
			COALESCE(
				NULLIF(LTRIM(RTRIM(oi.ItemName)), ''),
				NULLIF(LTRIM(RTRIM(sp.ItemName)), ''),
				NULLIF(LTRIM(RTRIM(sp.SupplierItemName)), ''),
				NULLIF(LTRIM(RTRIM(spfb.ItemName)), ''),
				NULLIF(LTRIM(RTRIM(spfb.SupplierItemName)), ''),
				p.Name,
				''
			) AS Name,
			COALESCE(sp.SupplierItemName, spfb.SupplierItemName) AS SuppName,
			COALESCE(NULLIF(LTRIM(RTRIM(oi.Barcode)), ''), sp.Barcode, spfb.Barcode) AS Barcode,
			COALESCE(sp.Manufacturer, spfb.Manufacturer) AS Manufacturer,
			COALESCE(sp.Country, spfb.Country) AS Country,
			COALESCE(sp.Series, spfb.Series) AS Series,
			COALESCE(sp.BatchNumber, spfb.BatchNumber) AS Batch,
			COALESCE(sp.ExpiryDate, spfb.ExpiryDate) AS Expiry,
			oi.Qty AS Qty,
			oi.UnitPrice AS Price
		FROM OrderItem oi
		INNER JOIN "Order" o ON o.OrderID = oi.OrderID
		INNER JOIN BuyerUser bu ON bu.BuyerUserID = o.BuyerUserID
		INNER JOIN Buyer b ON b.BuyerID = bu.BuyerID
		LEFT JOIN BuyerLocation bl ON bl.BuyerLocationID = o.BuyerLocationID
		LEFT JOIN Product p ON p.ProductID = oi.ProductID
		LEFT JOIN SupplierPrice sp ON sp.SupplierPriceID = oi.SupplierPriceID
		LEFT JOIN LATERAL (
			SELECT spx.ItemCode, spx.ItemName, spx.SupplierItemName, spx.Barcode,
				spx.Manufacturer, spx.Country, spx.Series, spx.BatchNumber, spx.ExpiryDate
			FROM SupplierPrice spx
			WHERE sp.SupplierPriceID IS NULL
			  AND spx.SupplierID = oi.SupplierID
			  AND (
			    (oi.ProductID IS NOT NULL AND spx.GUID_ES = oi.ProductID)
			    OR ABS(COALESCE(spx.FinalPrice, spx.Price) - oi.UnitPrice) < 0.05
			  )
			ORDER BY
			  CASE WHEN oi.ProductID IS NOT NULL AND spx.GUID_ES = oi.ProductID THEN 0 ELSE 1 END,
			  ABS(COALESCE(spx.FinalPrice, spx.Price) - oi.UnitPrice),
			  spx.UpdatedAt DESC
		) spfb
		WHERE oi.SupplierID = CAST(@sid AS UUID)
		  AND o.CreatedAt >= @from
		  AND o.CreatedAt < DATEADD(day, 1, CAST(@to AS DATE))
		ORDER BY o.CreatedAt, o.OrderID
LIMIT 1
`, sql.Named("sid", sid), sql.Named("from", from), sql.Named("to", to)).Scan(&rows).Error
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка выборки заказов для выгрузки: %v", err)
		}
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка выборки заказов"})
		return
	}
	if len(rows) == 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Нет заказов за указанный период"})
		return
	}

	byOrder := map[string][]orderexport.OrderLine{}
	orderDates := map[string]time.Time{}
	for _, rw := range rows {
		var expiry *time.Time
		if rw.Expiry.Valid {
			t := rw.Expiry.Time
			expiry = &t
		}
		line := orderexport.OrderLine{
			OrderID:      rw.OrderID,
			OrderDate:    rw.OrderDate,
			Buyer:        rw.Buyer,
			Address:      rw.Address,
			Code:         nullStr(rw.Code),
			Name:         nullStr(rw.Name),
			SuppName:     nullStr(rw.SuppName),
			Barcode:      nullStr(rw.Barcode),
			Manufacturer: nullStr(rw.Manufacturer),
			Country:      nullStr(rw.Country),
			Series:       nullStr(rw.Series),
			Batch:        nullStr(rw.Batch),
			Expiry:       expiry,
			Qty:          rw.Qty,
			Price:        rw.Price,
			Sum:          rw.Qty * rw.Price,
		}
		byOrder[rw.OrderID] = append(byOrder[rw.OrderID], line)
		orderDates[rw.OrderID] = rw.OrderDate
	}

	orderFiles := make(map[string][]byte, len(byOrder))
	totalLines := 0
	for orderID, lines := range byOrder {
		dbfBytes, err := orderexport.BuildOrdersDBF(lines)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка формирования DBF для заказа " + orderID})
			return
		}
		orderFiles[orderexport.OrderDBFFileName(orderID, orderDates[orderID])] = dbfBytes
		totalLines += len(lines)
	}

	var payload []byte
	var fileName string
	if len(orderFiles) == 1 {
		for name, data := range orderFiles {
			fileName = name
			payload = data
			break
		}
	} else {
		zipBytes, err := orderexport.BuildZip(orderFiles)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "Ошибка формирования ZIP"})
			return
		}
		fileName = fmt.Sprintf("orders_%s_%s.zip", time.Now().UTC().Format("20060102_150405"), sid[:8])
		payload = zipBytes
	}

	var cfg models.SupplierExportConfig
	cfgErr := s.database.GORMWith(ctx).Where("SupplierID = ?", db.UUIDParam(sid)).Take(&cfg).Error
	if cfgErr == gorm.ErrRecordNotFound {
		cfg.Method = "none"
	} else if cfgErr != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка чтения настроек выгрузки: %v", cfgErr)
		}
		cfg.Method = "none"
	}

	ftpStatus := "skip"
	emailStatus := "skip"
	var ftpErr, emailErr string

	wantFTP := cfg.Method == "ftp" || cfg.Method == "both"
	wantEmail := cfg.Method == "email" || cfg.Method == "both"

	if wantFTP {
		ftpOK := 0
		ftpFail := 0
		for name, data := range orderFiles {
			if err := uploadOrdersFTP(cfg, name, data); err != nil {
				ftpFail++
				if ftpErr == "" {
					ftpErr = name + ": " + err.Error()
				}
				if s.logger != nil {
					s.logger.Error("FTP выгрузка %s для поставщика %s: %v", name, sid, err)
				}
			} else {
				ftpOK++
			}
		}
		if ftpFail == 0 {
			ftpStatus = "ok"
		} else if ftpOK > 0 {
			ftpStatus = "partial"
		} else {
			ftpStatus = "error"
		}
	}
	if wantEmail {
		if err := sendOrdersEmail(cfg, fileName, payload, len(byOrder)); err != nil {
			emailStatus = "error"
			emailErr = err.Error()
			if s.logger != nil {
				s.logger.Error("Email выгрузка заказов %s: %v", sid, err)
			}
		} else {
			emailStatus = "ok"
		}
	}

	status := "ok"
	msgParts := []string{
		fmt.Sprintf("orders=%d files=%d lines=%d", len(byOrder), len(orderFiles), totalLines),
		"ftp=" + ftpStatus,
		"email=" + emailStatus,
	}
	if ftpErr != "" {
		msgParts = append(msgParts, "ftp_err="+ftpErr)
		status = "error"
	}
	if emailErr != "" {
		msgParts = append(msgParts, "email_err="+emailErr)
		status = "error"
	}
	// Если метод none — всё равно ok (только скачивание).
	if cfg.Method == "none" || cfg.Method == "" {
		status = "ok"
	}
	// Частичный успех: файл сформирован; если хоть один канал ок при both — ok с пометкой.
	if (wantFTP || wantEmail) && (ftpStatus == "ok" || emailStatus == "ok") && status == "error" {
		status = "ok"
	}

	msg := strings.Join(msgParts, "; ")
	logRow := models.OrderExportLog{
		OrderExportLogID: uuid.New().String(),
		SupplierID:       sid,
		Method:           &cfg.Method,
		FileName:         &fileName,
		OrdersCount:      len(byOrder),
		Status:           status,
		Message:          &msg,
		CreatedAt:        time.Now().UTC(),
	}
	_ = s.database.GORMWith(ctx).Create(&logRow)

	fileList := make([]string, 0, len(orderFiles))
	for name := range orderFiles {
		fileList = append(fileList, name)
	}
	sort.Strings(fileList)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"file_name":    fileName,
		"files":        fileList,
		"orders_count": len(byOrder),
		"lines_count":  totalLines,
		"method":       cfg.Method,
		"ftp":          map[string]string{"status": ftpStatus, "error": ftpErr},
		"email":        map[string]string{"status": emailStatus, "error": emailErr},
		"file_base64":  base64.StdEncoding.EncodeToString(payload),
		"log_id":       logRow.OrderExportLogID,
	})
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func uploadOrdersFTP(cfg models.SupplierExportConfig, fileName string, data []byte) error {
	if cfg.FtpHost == nil || *cfg.FtpHost == "" {
		return fmt.Errorf("не указан FTP-хост")
	}
	port := cfg.FtpPort
	if port <= 0 {
		port = 21
	}
	addr := net.JoinHostPort(*cfg.FtpHost, fmt.Sprintf("%d", port))
	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(30*time.Second))
	if err != nil {
		return fmt.Errorf("подключение: %w", err)
	}
	defer conn.Quit()

	user := "anonymous"
	pass := "anonymous@"
	if cfg.FtpUser != nil && *cfg.FtpUser != "" {
		user = *cfg.FtpUser
	}
	if cfg.FtpPassword != nil {
		pass = *cfg.FtpPassword
	}
	if err := conn.Login(user, pass); err != nil {
		return fmt.Errorf("логин: %w", err)
	}

	dir := ""
	if cfg.FtpDir != nil {
		dir = strings.TrimSpace(*cfg.FtpDir)
	}
	if dir != "" && dir != "/" {
		dir = strings.TrimRight(dir, "/")
		if err := conn.ChangeDir(dir); err != nil {
			// пробуем создать путь по частям
			parts := strings.Split(strings.Trim(dir, "/"), "/")
			cur := ""
			for _, p := range parts {
				if p == "" {
					continue
				}
				cur = path.Join(cur, p)
				_ = conn.MakeDir(cur)
			}
			if err := conn.ChangeDir(dir); err != nil {
				return fmt.Errorf("каталог %s: %w", dir, err)
			}
		}
	}

	if err := conn.Stor(fileName, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("загрузка файла: %w", err)
	}
	return nil
}

func sendOrdersEmail(cfg models.SupplierExportConfig, fileName string, data []byte, ordersCount int) error {
	if cfg.EmailTo == nil || *cfg.EmailTo == "" {
		return fmt.Errorf("не указан получатель EmailTo")
	}
	if cfg.SmtpHost == nil || *cfg.SmtpHost == "" {
		return fmt.Errorf("не указан SMTP-хост")
	}
	port := cfg.SmtpPort
	if port <= 0 {
		port = 587
	}
	from := "noreply@pharmdata.local"
	if cfg.SmtpFrom != nil && *cfg.SmtpFrom != "" {
		from = *cfg.SmtpFrom
	}

	boundary := "pharmdata_boundary_" + uuid.New().String()
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", *cfg.EmailTo)
	fmt.Fprintf(&msg, "Subject: Выгрузка заказов PharmData (%d)\r\n", ordersCount)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary)

	fmt.Fprintf(&msg, "--%s\r\n", boundary)
	fmt.Fprintf(&msg, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprintf(&msg, "Во вложении %s с заказами (%d шт., по одному DBF на заказ).\r\n\r\n", attachmentLabel(fileName), ordersCount)

	fmt.Fprintf(&msg, "--%s\r\n", boundary)
	fmt.Fprintf(&msg, "Content-Type: application/octet-stream; name=\"%s\"\r\n", fileName)
	fmt.Fprintf(&msg, "Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&msg, "Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", fileName)

	b64 := base64.StdEncoding.EncodeToString(data)
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		msg.WriteString(b64[i:end])
		msg.WriteString("\r\n")
	}
	fmt.Fprintf(&msg, "\r\n--%s--\r\n", boundary)

	addr := net.JoinHostPort(*cfg.SmtpHost, fmt.Sprintf("%d", port))
	var auth smtp.Auth
	if cfg.SmtpUser != nil && *cfg.SmtpUser != "" {
		pass := ""
		if cfg.SmtpPassword != nil {
			pass = *cfg.SmtpPassword
		}
		auth = smtp.PlainAuth("", *cfg.SmtpUser, pass, *cfg.SmtpHost)
	}

	recipients := splitEmails(*cfg.EmailTo)
	return smtp.SendMail(addr, auth, from, recipients, msg.Bytes())
}

func attachmentLabel(fileName string) string {
	if strings.HasSuffix(strings.ToLower(fileName), ".zip") {
		return "ZIP-архив"
	}
	return "DBF-файл"
}

func splitEmails(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
