// Package config предоставляет управление конфигурацией приложения
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

// Server содержит конфигурацию сервера
type Server struct {
	Host                   string `json:"host" yaml:"host"`
	Port                   int    `json:"port" yaml:"port"`
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds" yaml:"shutdown_timeout_seconds"`
}

// TelegramAPIServer содержит конфигурацию одного сервера Telegram API
type TelegramAPIServer struct {
	APIID       int    `json:"api_id" yaml:"api_id"`
	APIHash     string `json:"api_hash" yaml:"api_hash"`
	PhoneNumber string `json:"phone_number" yaml:"phone_number"`
	SessionFile string `json:"session_file" yaml:"session_file"`
}

// TelegramAPI содержит конфигурацию Telegram API
type TelegramAPI struct {
	// Новый формат для нескольких серверов
	Servers []TelegramAPIServer `json:"servers" yaml:"servers"`

	HealthCheckIntervalSeconds int `json:"health_check_interval_seconds" yaml:"health_check_interval_seconds"`
}

// Processing содержит конфигурацию обработки
type Processing struct {
	TaskTimeoutSeconds int `json:"task_timeout_seconds" yaml:"task_timeout_seconds"` // 0 - без ограничений
	CacheTTLMinutes    int `json:"cache_ttl_minutes" yaml:"cache_ttl_minutes"`
}

// Enrichment содержит конфигурацию сервиса обогащения данных
type Enrichment struct {
	PoolSize                int `json:"pool_size" yaml:"pool_size"`
	ClientRetryPauseSeconds int `json:"client_retry_pause_seconds" yaml:"client_retry_pause_seconds"`
}

// Logging содержит конфигурацию логирования
type Logging struct {
	Level string `json:"level" yaml:"level"` // debug, info, warn, error
}

// Config содержит конфигурацию приложения
type Config struct {
	Server      Server      `json:"server" yaml:"server"`
	TelegramAPI TelegramAPI `json:"telegram_api" yaml:"telegram_api"`
	Processing  Processing  `json:"processing" yaml:"processing"`
	Enrichment  Enrichment  `json:"enrichment" yaml:"enrichment"`
	Logging     Logging     `json:"logging" yaml:"logging"`
}

// GetTelegramServers возвращает список конфигураций серверов Telegram.
func (c *Config) GetTelegramServers() []TelegramAPIServer {
	return c.TelegramAPI.Servers
}

// LoadConfig загружает конфигурацию приложения из переменных окружения, .env файла или config.yml
func LoadConfig() (*Config, error) {
	// Загрузка переменных окружения из .env файла, если он существует
	// Загрузка .env файла игнорируется, если он не найден.
	_ = godotenv.Load()

	// Загрузка конфигурации из YAML-файла является единственным поддерживаемым способом.
	cfg, err := loadFromYAML("config.yml")
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить конфигурацию: %w", err)
	}

	return cfg, nil
}

// loadFromYAML загружает конфигурацию из YAML-файла
func loadFromYAML(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл конфигурации %s: %w", filename, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("не удалось разобрать YAML конфигурацию: %w", err)
	}

	return &cfg, nil
}

// Address возвращает адрес сервера в формате "host:port"
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// Validate проверяет, являются ли значения конфигурации допустимыми
func (c *Config) Validate() error {
	// Валидация Telegram API
	servers := c.GetTelegramServers()
	if len(servers) == 0 {
		return fmt.Errorf("конфигурация telegram_api не найдена или пуста")
	}

	for i, s := range servers {
		if s.APIID <= 0 {
			return fmt.Errorf("telegram_api.servers[%d].api_id должно быть положительным целым числом", i)
		}
		if s.APIHash == "" {
			return fmt.Errorf("telegram_api.servers[%d].api_hash не может быть пустым", i)
		}
		if s.PhoneNumber == "" {
			return fmt.Errorf("telegram_api.servers[%d].phone_number не может быть пустым", i)
		}
	}

	// Валидация остальных полей
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port должен быть действительным номером порта (1-65535)")
	}

	if c.Server.ShutdownTimeoutSeconds <= 0 {
		return fmt.Errorf("server.shutdown_timeout_seconds должно быть положительным")
	}

	if c.Processing.TaskTimeoutSeconds < 0 {
		return fmt.Errorf("processing.task_timeout_seconds должно быть неотрицательным (0 для отсутствия ограничений)")
	}

	if c.Processing.CacheTTLMinutes <= 0 {
		return fmt.Errorf("processing.cache_ttl_minutes должно быть положительным целым числом")
	}

	if c.TelegramAPI.HealthCheckIntervalSeconds <= 0 {
		return fmt.Errorf("telegram_api.health_check_interval_seconds должно быть положительным")
	}

	if c.Enrichment.PoolSize <= 0 {
		return fmt.Errorf("enrichment.pool_size должно быть положительным")
	}

	if c.Enrichment.ClientRetryPauseSeconds <= 0 {
		return fmt.Errorf("enrichment.client_retry_pause_seconds должно быть положительным")
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		// all good
	default:
		return fmt.Errorf("logging.level должен быть одним из: debug, info, warn, error")
	}

	return nil
}

// getEnv извлекает значение переменной окружения или возвращает значение по умолчанию, если она не установлена
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
