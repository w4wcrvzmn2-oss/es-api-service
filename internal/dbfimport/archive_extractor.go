package dbfimport

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ExtractArchive распаковывает архив и возвращает путь к первому найденному
// поддерживаемому файлу данных (DBF/Excel).
// Публичная функция для использования из других пакетов
func ExtractArchive(archivePath string, logger Logger) (string, error) {
	return extractArchiveInternal(archivePath, logger)
}

// extractArchiveInternal внутренняя функция распаковки архива
func extractArchiveInternal(archivePath string, logger Logger) (string, error) {
	ext := strings.ToLower(filepath.Ext(archivePath))

	switch ext {
	case ".zip":
		return extractZipInternal(archivePath, logger)
	case ".rar", ".7z", ".tar", ".gz":
		// Для этих форматов нужны внешние программы или библиотеки
		if logger != nil {
			logger.Warn("Формат архива %s требует внешних программ. Используйте ZIP для автоматической распаковки.", ext)
		}
		return "", fmt.Errorf("формат архива %s не поддерживается для автоматической распаковки. Используйте ZIP", ext)
	default:
		return "", fmt.Errorf("неизвестный формат архива: %s", ext)
	}
}

// extractZipInternal распаковывает ZIP архив и возвращает путь к первому
// найденному поддерживаемому файлу данных.
func extractZipInternal(zipPath string, logger Logger) (string, error) {
	if logger != nil {
		logger.Info("Распаковка ZIP архива: %s", zipPath)
	}

	// Открываем ZIP архив
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть ZIP архив: %w", err)
	}
	defer r.Close()

	// Создаем временную директорию для распаковки
	tempDir := filepath.Join(filepath.Dir(zipPath), "extracted_"+filepath.Base(zipPath)+"_tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("не удалось создать временную директорию: %w", err)
	}
	tempDirAbs, err := filepath.Abs(tempDir)
	if err != nil {
		return "", fmt.Errorf("не удалось определить временную директорию: %w", err)
	}

	// Ищем первый поддерживаемый файл данных в архиве.
	var dataFilePath string

	// Распаковываем все файлы
	for _, f := range r.File {
		if IsSupportedDataFile(f.Name) {
			extractedPath, err := safeZipExtractPath(tempDirAbs, f.Name)
			if err != nil {
				if logger != nil {
					logger.Warn("Небезопасный путь в ZIP архиве %s: %v", f.Name, err)
				}
				continue
			}

			// Создаем директорию, если нужно
			if err := os.MkdirAll(filepath.Dir(extractedPath), 0755); err != nil {
				if logger != nil {
					logger.Warn("Не удалось создать директорию для %s: %v", f.Name, err)
				}
				continue
			}

			// Открываем файл из архива
			rc, err := f.Open()
			if err != nil {
				if logger != nil {
					logger.Warn("Не удалось открыть файл %s из архива: %v", f.Name, err)
				}
				continue
			}

			// Создаем файл на диске
			outFile, err := os.Create(extractedPath)
			if err != nil {
				rc.Close()
				if logger != nil {
					logger.Warn("Не удалось создать файл %s: %v", extractedPath, err)
				}
				continue
			}

			// Копируем содержимое
			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()

			if err != nil {
				if logger != nil {
					logger.Warn("Не удалось скопировать файл %s: %v", f.Name, err)
				}
				os.Remove(extractedPath)
				continue
			}

			// Сохраняем путь к первому найденному поддерживаемому файлу данных.
			if dataFilePath == "" {
				dataFilePath = extractedPath
				if logger != nil {
					logger.Info("Найден файл данных в архиве: %s", f.Name)
				}
			}
		}
	}

	if dataFilePath == "" {
		// Очищаем временную директорию
		os.RemoveAll(tempDir)
		return "", fmt.Errorf("поддерживаемый файл данных не найден в ZIP архиве")
	}

	if logger != nil {
		logger.Info("Архив распакован. Файл данных: %s", dataFilePath)
	}

	return dataFilePath, nil
}

func safeZipExtractPath(tempDir, entryName string) (string, error) {
	if filepath.IsAbs(entryName) {
		return "", fmt.Errorf("абсолютный путь запрещён")
	}

	cleanName := filepath.Clean(entryName)
	if cleanName == "." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) || cleanName == ".." {
		return "", fmt.Errorf("выход за каталог распаковки запрещён")
	}

	target := filepath.Join(tempDir, cleanName)
	rel, err := filepath.Rel(tempDir, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("выход за каталог распаковки запрещён")
	}

	return target, nil
}

// isArchive проверяет, является ли файл архивом
func isArchive(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	archiveExts := []string{".zip", ".rar", ".7z", ".tar", ".gz", ".bz2"}
	for _, archiveExt := range archiveExts {
		if ext == archiveExt {
			return true
		}
	}
	return false
}

// CleanupExtractedFiles удаляет временные распакованные файлы
// Публичная функция для использования из других пакетов
func CleanupExtractedFiles(filePath string) {
	if strings.Contains(filepath.Dir(filePath), "_tmp") {
		// Удаляем временную директорию
		tempDir := filepath.Dir(filePath)
		os.RemoveAll(tempDir)
	}
}

// Logger интерфейс для логирования (чтобы не создавать циклическую зависимость)
type Logger interface {
	Info(format string, args ...interface{})
	Warn(format string, args ...interface{})
	Error(format string, args ...interface{})
}
