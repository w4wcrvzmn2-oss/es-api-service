package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FieldsConfig представляет конфигурацию полей для таблиц
type FieldsConfig struct {
	Tables map[string]TableConfig `yaml:"tables"`
	Defaults DefaultsConfig       `yaml:"defaults"`
}

// TableConfig представляет конфигурацию для конкретной таблицы
type TableConfig struct {
	DefaultFields     []string          `yaml:"default_fields"`
	AllFields         []string          `yaml:"all_fields"`
	Description       string            `yaml:"description"`
	FieldDescriptions map[string]string `yaml:"field_descriptions"`
}

// DefaultsConfig представляет настройки по умолчанию
type DefaultsConfig struct {
	Limit           int  `yaml:"limit"`
	MaxLimit        int  `yaml:"max_limit"`
	DefaultColumns  bool `yaml:"default_columns"`
}

// LoadFieldsConfig загружает конфигурацию полей из YAML файла
func LoadFieldsConfig() (*FieldsConfig, error) {
	// Получаем путь к исполняемому файлу
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить путь к исполняемому файлу: %w", err)
	}
	dir := filepath.Dir(exePath)
	
	// Путь к файлу конфигурации полей
	configPath := filepath.Join(dir, "api_fields_config.yaml")
	
	// Проверяем существование файла
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Если файл не существует, возвращаем конфигурацию по умолчанию
		return getDefaultFieldsConfig(), nil
	}
	
	// Читаем файл
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл конфигурации полей: %w", err)
	}
	
	// Парсим YAML
	var config FieldsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("ошибка парсинга YAML конфигурации полей: %w", err)
	}
	
	// Устанавливаем значения по умолчанию если они не заданы
	if config.Defaults.Limit == 0 {
		config.Defaults.Limit = 100
	}
	if config.Defaults.MaxLimit == 0 {
		config.Defaults.MaxLimit = 10000
	}
	
	return &config, nil
}

// getDefaultFieldsConfig возвращает конфигурацию по умолчанию
func getDefaultFieldsConfig() *FieldsConfig {
	return &FieldsConfig{
		Tables: map[string]TableConfig{
			"Region": {
				DefaultFields: []string{"Code", "Name", "Capital", "FederalDistrict"},
				AllFields:     []string{"RegionID", "Code", "Name", "FederalDistrict", "Capital", "IsActive", "CreatedAt", "UpdatedAt"},
				Description:   "Регионы России",
				FieldDescriptions: map[string]string{
					"RegionID":       "Уникальный идентификатор региона (GUID)",
					"Code":           "Код региона (01-92)",
					"Name":           "Название региона",
					"FederalDistrict": "Федеральный округ",
					"Capital":        "Столица/административный центр",
					"IsActive":       "Признак активности (true/false)",
					"CreatedAt":      "Время создания (DATETIME2)",
					"UpdatedAt":      "Время обновления (DATETIME2)",
				},
			},
		},
		Defaults: DefaultsConfig{
			Limit:          100,
			MaxLimit:       10000,
			DefaultColumns: true,
		},
	}
}

// GetTableConfig возвращает конфигурацию для указанной таблицы
func (fc *FieldsConfig) GetTableConfig(tableName string) (TableConfig, bool) {
	config, exists := fc.Tables[tableName]
	return config, exists
}

// GetDefaultFields возвращает поля по умолчанию для таблицы
func (fc *FieldsConfig) GetDefaultFields(tableName string) []string {
	if config, exists := fc.GetTableConfig(tableName); exists {
		return config.DefaultFields
	}
	return []string{}
}

// GetAllFields возвращает все доступные поля для таблицы
func (fc *FieldsConfig) GetAllFields(tableName string) []string {
	if config, exists := fc.GetTableConfig(tableName); exists {
		return config.AllFields
	}
	return []string{}
}

// ShouldUseDefaultFields проверяет, нужно ли использовать поля по умолчанию
func (fc *FieldsConfig) ShouldUseDefaultFields() bool {
	return fc.Defaults.DefaultColumns
}
