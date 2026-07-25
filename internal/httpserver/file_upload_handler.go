package httpserver

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"es_api_service/internal/dbfimport"
)

// handleUploadDBF обрабатывает загрузку DBF/ZIP файла на сервер.
func (s *Server) handleUploadDBF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	// До 512 МБ в multipart (крупные прайсы вроде Katren ~70МБ).
	err := r.ParseMultipartForm(512 << 20)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка парсинга multipart /api/dbf/upload: %v", err)
		}
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка загрузки файла (возможно таймаут или файл слишком большой): %v", err))
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		// запасные имена полей
		file, handler, err = r.FormFile("dbf")
	}
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка получения файла: %v", err))
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(handler.Filename))
	if ext != ".dbf" && ext != ".zip" {
		s.writeError(w, http.StatusBadRequest, "Поддерживаются файлы .dbf и .zip")
		return
	}

	uploadDir := filepath.Join(".", "uploads")
	if exe, e := os.Executable(); e == nil {
		uploadDir = filepath.Join(filepath.Dir(exe), "uploads")
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания директории uploads: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания директории: %v", err))
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	safeName := strings.ReplaceAll(filepath.Base(handler.Filename), "..", "_")
	safeFilename := fmt.Sprintf("%s_%s", timestamp, safeName)
	filePath := filepath.Join(uploadDir, safeFilename)

	dst, err := os.Create(filePath)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания файла: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания файла: %v", err))
		return
	}

	written, err := io.Copy(dst, file)
	_ = dst.Close()
	if err != nil {
		_ = os.Remove(filePath)
		if s.logger != nil {
			s.logger.Error("Ошибка сохранения файла: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка сохранения файла: %v", err))
		return
	}

	resultPath := filePath
	if ext == ".zip" {
		extracted, err := dbfimport.ExtractArchive(filePath, s.logger)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Не удалось извлечь DBF из ZIP: %v", err))
			return
		}
		resultPath = extracted
	}

	absPath, err := filepath.Abs(resultPath)
	if err != nil {
		absPath = resultPath
	}

	if s.logger != nil {
		s.logger.Info("Файл загружен: %s -> %s (%d байт)", handler.Filename, absPath, written)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":   "Файл успешно загружен",
		"file_path": absPath,
		"filename":  handler.Filename,
		"size":      written,
	})
}
