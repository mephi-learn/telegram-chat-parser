// Package config предоставляет управление конфигурацией приложения
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

// Server содержит конфигурацию сервера
type Server struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	MaxUploadSizeMB int64         `yaml:"max_upload_size_mb"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// TelegramAPIServer содержит конфигурацию одного сервера Telegram API
type TelegramAPIServer struct {
	APIID        int           `yaml:"api_id"`
	APIHash      string        `yaml:"api_hash"`
	PhoneNumber  string        `yaml:"phone_number"`
	SessionFile  string        `yaml:"session_file"`
	RequestDelay time.Duration `yaml:"request_delay"`
}

// TelegramAPI содержит конфигурацию Telegram API
type TelegramAPI struct {
	Servers             []TelegramAPIServer `yaml:"servers"`
	HealthCheckInterval time.Duration       `yaml:"health_check_interval"`
}

// Processing содержит конфигурацию обработки
type Processing struct {
	TaskTimeout time.Duration `yaml:"task_timeout"` // 0 - без ограничений
	CacheTTL    time.Duration `yaml:"cache_ttl"`
}

// Enrichment содержит конфигурацию сервиса обогащения данных
type Enrichment struct {
	PoolSize         int           `yaml:"pool_size"`
	ClientRetryPause time.Duration `yaml:"client_retry_pause"`
	OperationTimeout time.Duration `yaml:"operation_timeout"`
}

// Logging содержит конфигурацию логирования
type Logging struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
}

// Config содержит конфигурацию приложения
type Config struct {
	Server      Server      `yaml:"server"`
	TelegramAPI TelegramAPI `yaml:"telegram_api"`
	Processing  Processing  `yaml:"processing"`
	Enrichment  Enrichment  `yaml:"enrichment"`
	Logging     Logging     `yaml:"logging"`
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

	cfg := defaultConfig()

	// Загрузка конфигурации из YAML-файла является единственным поддерживаемым способом.
	if err := loadFromYAML("config.yml", cfg); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	cfg.SetDefaults()
	return cfg, nil
}

// loadFromYAML загружает конфигурацию из YAML-файла
func loadFromYAML(filename string, cfg *Config) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		// Если файл не найден, это не ошибка, мы просто используем значения по умолчанию.
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse YAML configuration: %w", err)
	}

	return nil
}

// SetDefaults устанавливает значения по умолчанию для конфигурации
func (c *Config) SetDefaults() {
	if c.Logging.Format == "" {
		c.Logging.Format = DefaultLogFormat
	}
}

func defaultConfig() *Config {
	return &Config{
		Server: Server{
			Host:            DefaultServerHost,
			Port:            DefaultServerPort,
			ReadTimeout:     DefaultReadTimeout,
			WriteTimeout:    DefaultWriteTimeout,
			IdleTimeout:     DefaultIdleTimeout,
			ShutdownTimeout: DefaultShutdownTimeout,
			MaxUploadSizeMB: DefaultMaxUploadSizeMB,
			CleanupInterval: DefaultCleanupInterval,
		},
		TelegramAPI: TelegramAPI{
			HealthCheckInterval: DefaultHealthCheckInterval,
		},
		Processing: Processing{
			TaskTimeout: DefaultTaskTimeout,
			CacheTTL:    DefaultCacheTTL,
		},
		Enrichment: Enrichment{
			PoolSize:         DefaultEnrichmentPoolSize,
			ClientRetryPause: DefaultEnrichmentClientRetryPause,
			OperationTimeout: DefaultEnrichmentOperationTimeout,
		},
		Logging: Logging{
			Level:  DefaultLogLevel,
			Format: DefaultLogFormat,
		},
	}
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
		return fmt.Errorf("telegram_api configuration not found or empty")
	}

	for i, s := range servers {
		if s.APIID <= 0 {
			return fmt.Errorf("telegram_api.servers[%d].api_id must be a positive integer", i)
		}
		if s.APIHash == "" {
			return fmt.Errorf("telegram_api.servers[%d].api_hash cannot be empty", i)
		}
		if s.PhoneNumber == "" {
			return fmt.Errorf("telegram_api.servers[%d].phone_number cannot be empty", i)
		}
	}

	// Валидация остальных полей
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be a valid port number (1-65535)")
	}

	if c.Server.ShutdownTimeout <= 0 {
		return fmt.Errorf("server.shutdown_timeout must be positive")
	}

	if c.Processing.TaskTimeout < 0 {
		return fmt.Errorf("processing.task_timeout must be non-negative (0 for no limits)")
	}

	if c.Processing.CacheTTL <= 0 {
		return fmt.Errorf("processing.cache_ttl must be positive")
	}

	if c.TelegramAPI.HealthCheckInterval <= 0 {
		return fmt.Errorf("telegram_api.health_check_interval must be positive")
	}

	if c.Enrichment.PoolSize <= 0 {
		return fmt.Errorf("enrichment.pool_size must be positive")
	}

	if c.Enrichment.ClientRetryPause <= 0 {
		return fmt.Errorf("enrichment.client_retry_pause must be positive")
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
		// all good
	default:
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}

	switch c.Logging.Format {
	case "", "text", "json":
		// all good
	default:
		return fmt.Errorf("logging.format must be one of: text, json (empty means text)")
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
