package httpserver

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"time"
)

// aiBaseURL — адрес ИИ-сервиса на Mac mini (FastAPI). По умолчанию LAN-адрес Мака.
// Go-клиент шлёт корректный Content-Length (как .NET-десктоп), поэтому проксируем
// чат напрямую в Мак, минуя Caddy /ai (там POST-тело от браузера не парсится).
func aiBaseURL() string {
	if v := os.Getenv("ELF_AI_BASE"); v != "" {
		return v
	}
	return "http://192.168.95.116:8808"
}

var aiHTTPClient = &http.Client{Timeout: 90 * time.Second}

// handleAIChat — POST /api/ai/chat: проксирует чат в ИИ-сервис (ExestAI).
// Тело как есть {message, history} уходит на Мак, ответ {reply} возвращается клиенту.
func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Не удалось прочитать запрос")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aiBaseURL()+"/chat", bytes.NewReader(body))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Ошибка запроса к ИИ")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := aiHTTPClient.Do(req)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("ИИ-чат недоступен: %v", err)
		}
		s.writeError(w, http.StatusBadGateway, "ИИ-сервис недоступен")
		return
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)
}
