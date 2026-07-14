package httpserver

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"net/http"
)

// handleUploadDBF обрабатывает загрузку DBF файла на сервер
func (s *Server) handleUploadDBF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}

	// Парсим multipart form
	err := r.ParseMultipartForm(32 << 20) // 32 MB max
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка парсинга формы: %v", err))
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("Ошибка получения файла: %v", err))
		return
	}
	defer file.Close()

	// Проверяем расширение файла
	if filepath.Ext(handler.Filename) != ".dbf" && filepath.Ext(handler.Filename) != ".DBF" {
		s.writeError(w, http.StatusBadRequest, "Поддерживаются только DBF файлы")
		return
	}

	// Создаем директорию для загруженных файлов, если её нет
	uploadDir := filepath.Join(".", "uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания директории uploads: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания директории: %v", err))
		return
	}

	// Генерируем уникальное имя файла
	timestamp := time.Now().Format("20060102_150405")
	safeFilename := fmt.Sprintf("%s_%s", timestamp, handler.Filename)
	filePath := filepath.Join(uploadDir, safeFilename)

	// Создаем файл на диске
	dst, err := os.Create(filePath)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка создания файла: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка создания файла: %v", err))
		return
	}
	defer dst.Close()

	// Копируем содержимое загруженного файла
	_, err = io.Copy(dst, file)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("Ошибка сохранения файла: %v", err)
		}
		s.writeError(w, http.StatusInternalServerError, fmt.Sprintf("Ошибка сохранения файла: %v", err))
		return
	}

	// Получаем абсолютный путь
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	if s.logger != nil {
		s.logger.Info("Файл загружен: %s -> %s", handler.Filename, absPath)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Файл успешно загружен",
		"file_path": absPath,
		"filename":  handler.Filename,
		"size":      handler.Size,
	})
}

