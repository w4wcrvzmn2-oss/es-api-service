package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Logger представляет логгер с ротацией файлов
type Logger struct {
	logDir      string
	serviceName string
	level       LogLevel
	currentFile *os.File
	currentDate string
}

// LogLevel представляет уровень логирования
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String возвращает строковое представление уровня
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel парсит уровень логирования из строки
func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// NewLogger создаёт новый логгер с ротацией файлов
func NewLogger(serviceName, logDir, level string) (*Logger, error) {
	// Создаём директорию для логов если её нет
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать директорию логов %s: %w", logDir, err)
	}

	logger := &Logger{
		logDir:      logDir,
		serviceName: serviceName,
		level:       ParseLogLevel(level),
	}

	// Открываем файл лога для текущей даты
	if err := logger.rotateIfNeeded(); err != nil {
		return nil, err
	}

	// Очищаем старые файлы логов
	go logger.cleanupOldLogs()

	return logger, nil
}

// rotateIfNeeded проверяет нужно ли ротировать лог файл
func (l *Logger) rotateIfNeeded() error {
	currentDate := time.Now().Format("2006-01-02")

	if l.currentDate == currentDate && l.currentFile != nil {
		return nil // Файл уже открыт для текущей даты
	}

	// Закрываем старый файл если он открыт
	if l.currentFile != nil {
		l.currentFile.Close()
	}

	// Создаём новый файл для текущей даты
	filename := fmt.Sprintf("%s_%s.log", l.serviceName, currentDate)
	filepath := filepath.Join(l.logDir, filename)

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть лог файл %s: %w", filepath, err)
	}

	l.currentFile = file
	l.currentDate = currentDate

	// Настраиваем стандартный логгер для записи в файл
	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	return nil
}

// cleanupOldLogs удаляет файлы логов старше 7 дней
func (l *Logger) cleanupOldLogs() {
	ticker := time.NewTicker(24 * time.Hour) // Проверяем раз в день
	defer ticker.Stop()

	// Выполняем очистку сразу при запуске
	l.performCleanup()

	for range ticker.C {
		l.performCleanup()
	}
}

// performCleanup выполняет очистку старых файлов
func (l *Logger) performCleanup() {
	files, err := filepath.Glob(filepath.Join(l.logDir, l.serviceName+"_*.log"))
	if err != nil {
		l.Error("Ошибка при поиске файлов логов для очистки: %v", err)
		return
	}

	cutoffDate := time.Now().AddDate(0, 0, -7) // 7 дней назад

	for _, file := range files {
		// Извлекаем дату из имени файла
		basename := filepath.Base(file)
		parts := strings.Split(basename, "_")
		if len(parts) < 2 {
			continue
		}

		dateStr := strings.TrimSuffix(parts[len(parts)-1], ".log")
		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		// Удаляем файл если он старше 7 дней
		if fileDate.Before(cutoffDate) {
			if err := os.Remove(file); err != nil {
				l.Error("Не удалось удалить старый лог файл %s: %v", file, err)
			} else {
				l.Info("Удалён старый лог файл: %s", file)
			}
		}
	}
}

// shouldLog проверяет нужно ли логировать сообщение данного уровня
func (l *Logger) shouldLog(level LogLevel) bool {
	return level >= l.level
}

// writeLog записывает сообщение в лог
func (l *Logger) writeLog(level LogLevel, format string, args ...interface{}) {
	if !l.shouldLog(level) {
		return
	}

	// Проверяем нужна ли ротация
	if err := l.rotateIfNeeded(); err != nil {
		// Если не удалось ротировать, пишем в stdout
		fmt.Printf("Ошибка ротации лога: %v\n", err)
	}

	message := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	logLine := fmt.Sprintf("[%s] %s: %s", timestamp, level.String(), message)

	// Записываем в лог напрямую в файл
	if l.currentFile != nil {
		fmt.Fprintln(l.currentFile, logLine)
		l.currentFile.Sync() // Принудительно записываем на диск
	}
	// Также выводим в консоль
	fmt.Println(logLine)
}

// Debug записывает отладочное сообщение
func (l *Logger) Debug(format string, args ...interface{}) {
	l.writeLog(LevelDebug, format, args...)
}

// Info записывает информационное сообщение
func (l *Logger) Info(format string, args ...interface{}) {
	l.writeLog(LevelInfo, format, args...)
}

// Warn записывает предупреждение
func (l *Logger) Warn(format string, args ...interface{}) {
	l.writeLog(LevelWarn, format, args...)
}

// Error записывает ошибку
func (l *Logger) Error(format string, args ...interface{}) {
	l.writeLog(LevelError, format, args...)
}

// Close закрывает логгер
func (l *Logger) Close() error {
	if l.currentFile != nil {
		return l.currentFile.Close()
	}
	return nil
}

// GetLogFiles возвращает список файлов логов, отсортированный по дате
func (l *Logger) GetLogFiles() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(l.logDir, l.serviceName+"_*.log"))
	if err != nil {
		return nil, err
	}

	// Сортируем файлы по имени (дата в имени обеспечивает правильную сортировку)
	sort.Strings(files)
	return files, nil
}
