package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config представляет конфигурацию сервиса
type Config struct {
	HTTP struct {
		Addr            string `yaml:"addr"`
		Port            int    `yaml:"port"`
		ReadTimeoutSec  int    `yaml:"read_timeout_sec"`
		WriteTimeoutSec int    `yaml:"write_timeout_sec"`
		IdleTimeoutSec  int    `yaml:"idle_timeout_sec"`
		StaticDir       string `yaml:"static_dir"` // ClientWeb; пусто = ./ClientWeb
	} `yaml:"http"`

	// Второй HTTP сервер для ClienElf2
	HTTP2 struct {
		Addr      string `yaml:"addr"`
		Port      int    `yaml:"port"`
		StaticDir string `yaml:"static_dir"`
	} `yaml:"http2"`

	Auth struct {
		Issuer          string `yaml:"issuer"`
		JWTSecret       string `yaml:"jwt_secret"`
		TokenTTLMinutes int    `yaml:"token_ttl_minutes"`
		Users           []User `yaml:"users"`
	} `yaml:"auth"`

	DB struct {
		Server       string `yaml:"server"`
		Port         int    `yaml:"port"`
		User         string `yaml:"user"`
		Password     string `yaml:"password"`
		Database     string `yaml:"database"`
		SSLMode      string `yaml:"sslmode"`
		MaxOpenConns int    `yaml:"max_open_conns"` // пул PG; 0 → 100
		MaxIdleConns int    `yaml:"max_idle_conns"` // 0 → max(25, MaxOpen/4)
	} `yaml:"db"`

	// База данных для синхронизации справочника препаратов (PostgreSQL)
	SourceDB struct {
		Server       string `yaml:"server"`
		Port         int    `yaml:"port"`
		User         string `yaml:"user"`
		Password     string `yaml:"password"`
		Database     string `yaml:"database"`
		SSLMode      string `yaml:"sslmode"`
		MaxOpenConns int    `yaml:"max_open_conns"` // 0 → 20
		MaxIdleConns int    `yaml:"max_idle_conns"` // 0 → 5
	} `yaml:"source_db"`

	Logging struct {
		Level   string `yaml:"level"`
		LogDir  string `yaml:"log_dir"`
		Enabled bool   `yaml:"enabled"`
	} `yaml:"logging"`

	// Security — анти-брутфорс и HTTP flood (in-process).
	Security struct {
		TrustProxyHeaders bool `yaml:"trust_proxy_headers"`
		LoginMaxAttempts  int  `yaml:"login_max_attempts"`
		LoginWindowSec    int  `yaml:"login_window_sec"`
		LoginLockoutSec   int  `yaml:"login_lockout_sec"`
		LoginRatePerMin   int  `yaml:"login_rate_per_min"`
		APIRatePerMin     int  `yaml:"api_rate_per_min"`
		StaticRatePerMin  int  `yaml:"static_rate_per_min"`
	} `yaml:"security"`

	// FieldsConfig загружается отдельно из api_fields_config.yaml
	FieldsConfig *FieldsConfig `yaml:"-"`
}

// User представляет пользователя для аутентификации
type User struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// LoadConfig загружает конфигурацию из файла .cfg рядом с исполняемым файлом
func LoadConfig() (*Config, error) {
	execPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить путь к исполняемому файлу: %w", err)
	}

	// Получаем имя файла без расширения и добавляем .cfg
	dir := filepath.Dir(execPath)
	baseName := strings.TrimSuffix(filepath.Base(execPath), filepath.Ext(execPath))
	configPath := filepath.Join(dir, baseName+".cfg")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать конфигурационный файл %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("не удалось разобрать конфигурационный файл: %w", err)
	}

	// Устанавливаем значения по умолчанию
	if config.HTTP.Addr == "" {
		config.HTTP.Addr = "0.0.0.0"
	}
	if config.HTTP.Port == 0 {
		config.HTTP.Port = 8080
	}
	if config.HTTP.ReadTimeoutSec == 0 {
		// Крупные DBF (десятки МБ) через браузер не укладываются в 15с.
		config.HTTP.ReadTimeoutSec = 300
	}
	if config.HTTP.WriteTimeoutSec == 0 {
		config.HTTP.WriteTimeoutSec = 600
	}
	if config.HTTP.IdleTimeoutSec == 0 {
		config.HTTP.IdleTimeoutSec = 120
	}
	if config.Auth.Issuer == "" {
		config.Auth.Issuer = "es-api"
	}
	if config.Auth.TokenTTLMinutes == 0 {
		config.Auth.TokenTTLMinutes = 1440
	}
	if config.DB.Port == 0 {
		config.DB.Port = 5432
	}
	if config.DB.SSLMode == "" {
		config.DB.SSLMode = "disable"
	}
	if config.DB.MaxOpenConns <= 0 {
		// Для локального PostgreSQL на сервере держим консервативный пул,
		// чтобы массовый импорт не съедал все соединения.
		config.DB.MaxOpenConns = 20
	}
	if config.DB.MaxIdleConns <= 0 {
		idle := config.DB.MaxOpenConns / 4
		if idle < 5 {
			idle = 5
		}
		if idle > config.DB.MaxOpenConns {
			idle = config.DB.MaxOpenConns
		}
		config.DB.MaxIdleConns = idle
	}
	if config.SourceDB.Port == 0 {
		config.SourceDB.Port = 5432
	}
	if config.SourceDB.SSLMode == "" {
		config.SourceDB.SSLMode = "disable"
	}
	if config.SourceDB.MaxOpenConns <= 0 {
		config.SourceDB.MaxOpenConns = 5
	}
	if config.SourceDB.MaxIdleConns <= 0 {
		config.SourceDB.MaxIdleConns = 2
	}
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.LogDir == "" {
		config.Logging.LogDir = "logs"
	}

	// Делаем путь к логам абсолютным относительно исполняемого файла
	if !filepath.IsAbs(config.Logging.LogDir) {
		config.Logging.LogDir = filepath.Join(dir, config.Logging.LogDir)
	}

	if !config.Logging.Enabled {
		config.Logging.Enabled = true // по умолчанию логирование включено
	}

	// Security defaults (behind Caddy trust X-Real-IP / X-Forwarded-For for per-IP limits)
	config.Security.TrustProxyHeaders = true
	if config.Security.LoginMaxAttempts == 0 {
		config.Security.LoginMaxAttempts = 5
	}
	if config.Security.LoginWindowSec == 0 {
		config.Security.LoginWindowSec = 900
	}
	if config.Security.LoginLockoutSec == 0 {
		config.Security.LoginLockoutSec = 900
	}
	if config.Security.LoginRatePerMin == 0 {
		config.Security.LoginRatePerMin = 20
	}
	if config.Security.APIRatePerMin == 0 {
		config.Security.APIRatePerMin = 180
	}
	if config.Security.StaticRatePerMin == 0 {
		config.Security.StaticRatePerMin = 600
	}

	// Загружаем конфигурацию полей
	fieldsConfig, err := LoadFieldsConfig()
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки конфигурации полей: %w", err)
	}
	config.FieldsConfig = fieldsConfig

	return &config, nil
}

// GetAddress возвращает полный адрес для HTTP сервера
func (c *Config) GetAddress() string {
	return fmt.Sprintf("%s:%d", c.HTTP.Addr, c.HTTP.Port)
}

// GetAddress2 возвращает полный адрес для второго HTTP сервера (ClienElf2)
func (c *Config) GetAddress2() string {
	if c.HTTP2.Port == 0 {
		return ""
	}
	addr := c.HTTP2.Addr
	if addr == "" {
		addr = "0.0.0.0"
	}
	return fmt.Sprintf("%s:%d", addr, c.HTTP2.Port)
}

// HasHTTP2 возвращает true если настроен второй HTTP сервер
func (c *Config) HasHTTP2() bool {
	return c.HTTP2.Port > 0
}

// GetReadTimeout возвращает таймаут чтения
func (c *Config) GetReadTimeout() time.Duration {
	return time.Duration(c.HTTP.ReadTimeoutSec) * time.Second
}

// GetWriteTimeout возвращает таймаут записи
func (c *Config) GetWriteTimeout() time.Duration {
	return time.Duration(c.HTTP.WriteTimeoutSec) * time.Second
}

// GetIdleTimeout возвращает таймаут простоя
func (c *Config) GetIdleTimeout() time.Duration {
	return time.Duration(c.HTTP.IdleTimeoutSec) * time.Second
}

// GetTokenTTL возвращает время жизни токена
func (c *Config) GetTokenTTL() time.Duration {
	return time.Duration(c.Auth.TokenTTLMinutes) * time.Minute
}

// FindUser ищет пользователя по имени
func (c *Config) FindUser(username string) *User {
	for _, user := range c.Auth.Users {
		if user.Username == username {
			return &user
		}
	}
	return nil
}
