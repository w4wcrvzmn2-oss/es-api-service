package httpserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

type ftpTestRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Path     string `json:"path"`
}

type ftpTestResponse struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	Stage     string `json:"stage,omitempty"` // dial|login|chdir|list
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// handleFTPTest — POST /api/ftp/test и /api/sc/ftp-test
// Проверяет TCP, логин и (опционально) доступ к каталогу.
func (s *Server) handleFTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "Метод не поддерживается"})
		return
	}

	var req ftpTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Неверный формат запроса"})
		return
	}

	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	req.Path = strings.TrimSpace(req.Path)
	if req.Host == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Укажите FTP-хост"})
		return
	}
	if req.Port <= 0 {
		req.Port = 21
	}
	if req.Port > 65535 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "Некорректный порт FTP"})
		return
	}

	writeJSON(w, http.StatusOK, testFTPConnection(req))
}

func testFTPConnection(req ftpTestRequest) ftpTestResponse {
	started := time.Now()
	addr := net.JoinHostPort(req.Host, fmt.Sprintf("%d", req.Port))

	conn, err := ftp.Dial(addr, ftp.DialWithTimeout(15*time.Second))
	if err != nil {
		return ftpTestResponse{
			OK: false, Stage: "dial", LatencyMS: time.Since(started).Milliseconds(),
			Message: "Не удалось подключиться к FTP-серверу",
			Error:   classifyFTPError("dial", err),
		}
	}
	defer conn.Quit()

	user := req.User
	pass := req.Password
	if user == "" {
		user = "anonymous"
	}
	if user == "anonymous" && pass == "" {
		pass = "anonymous@"
	}

	if err := conn.Login(user, pass); err != nil {
		return ftpTestResponse{
			OK: false, Stage: "login", LatencyMS: time.Since(started).Milliseconds(),
			Message: "Ошибка авторизации FTP",
			Error:   classifyFTPError("login", err),
		}
	}

	path := req.Path
	if path == "" {
		path = "/"
	}
	if path != "/" {
		path = strings.TrimRight(path, "/")
		if path == "" {
			path = "/"
		}
	}

	if path != "/" {
		if err := conn.ChangeDir(path); err != nil {
			return ftpTestResponse{
				OK: false, Stage: "chdir", LatencyMS: time.Since(started).Milliseconds(),
				Message: "Подключение и логин успешны, но каталог недоступен",
				Error:   classifyFTPError("chdir", err),
			}
		}
	}

	if _, err := conn.List("."); err != nil {
		// некоторые серверы запрещают LIST корня — не считаем это фатальным после успешного CWD/Login
		if path == "/" {
			return ftpTestResponse{
				OK: true, Stage: "list", LatencyMS: time.Since(started).Milliseconds(),
				Message: "Соединение успешно. Логин принят (листинг корня ограничен сервером).",
			}
		}
		return ftpTestResponse{
			OK: false, Stage: "list", LatencyMS: time.Since(started).Milliseconds(),
			Message: "Логин успешен, но не удалось прочитать содержимое каталога",
			Error:   classifyFTPError("list", err),
		}
	}

	msg := "Соединение успешно. Хост доступен, логин и пароль верны"
	if path != "/" {
		msg += ", каталог доступен"
	}
	msg += "."

	return ftpTestResponse{
		OK: true, Stage: "ok", LatencyMS: time.Since(started).Milliseconds(),
		Message: msg,
	}
}

func classifyFTPError(stage string, err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)

	switch stage {
	case "dial":
		switch {
		case strings.Contains(low, "timeout") || strings.Contains(low, "i/o timeout"):
			return "Таймаут: сервер не отвечает (проверьте хост, порт и сеть)"
		case strings.Contains(low, "refused"):
			return "Соединение отклонено (порт закрыт или FTP не запущен)"
		case strings.Contains(low, "no such host") || strings.Contains(low, "not known"):
			return "Хост не найден (проверьте имя сервера)"
		default:
			return msg
		}
	case "login":
		if strings.Contains(low, "530") || strings.Contains(low, "login incorrect") ||
			strings.Contains(low, "authentication") || strings.Contains(low, "password") ||
			strings.Contains(low, "not logged in") {
			return "Неверный логин или пароль"
		}
		return msg
	case "chdir":
		if strings.Contains(low, "550") || strings.Contains(low, "not found") || strings.Contains(low, "no such") {
			return "Каталог не найден или нет прав доступа: " + msg
		}
		return msg
	default:
		return msg
	}
}
