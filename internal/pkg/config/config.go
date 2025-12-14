// Package config предоставляет управление конфигурацией приложения
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v2"
)

// Server содержит конфигурацию сервера
type Server struct {
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port" yaml:"port"`
}

// TelegramAPI содержит конфигурацию Telegram API
type TelegramAPI struct {
	APIID       int    `json:"api_id" yaml:"api_id"`
	APIHash     string `json:"api_hash" yaml:"api_hash"`
	PhoneNumber string `json:"phone_number" yaml:"phone_number"`
	SessionFile string `json:"session_file" yaml:"session_file"`
}

// Processing содержит конфигурацию обработки
type Processing struct {
	TaskTimeoutSeconds int `json:"task_timeout_seconds" yaml:"task_timeout_seconds"` // 0 - без ограничений
	CacheTTLMinutes    int `json:"cache_ttl_minutes" yaml:"cache_ttl_minutes"`
}

// RateLimiter содержит конфигурацию ограничителя частоты
type RateLimiter struct {
	Strategy                string `json:"strategy" yaml:"strategy"`                                   // 'always_delay' или 'adaptive'
	RequestDelayMs          int    `json:"request_delay_ms" yaml:"request_delay_ms"`                   // для 'always_delay' и 'adaptive' (ограниченное состояние)
	ThrottledRetries        int    `json:"throttled_retries" yaml:"throttled_retries"`                 // для 'adaptive'
	CooldownDurationSeconds int    `json:"cooldown_duration_seconds" yaml:"cooldown_duration_seconds"` // для 'adaptive'
}

// Service содержит конфигурацию сервиса
type Service struct {
	PIDFilePath string `json:"pid_file_path" yaml:"pid_file_path"`
}

// Config содержит конфигурацию приложения
type Config struct {
	Server      Server      `json:"server" yaml:"server"`
	TelegramAPI TelegramAPI `json:"telegram_api" yaml:"telegram_api"`
	Processing  Processing  `json:"processing" yaml:"processing"`
	RateLimiter RateLimiter `json:"rate_limiter" yaml:"rate_limiter"`
	Service     Service     `json:"service" yaml:"service"`
}

// PIDFilePath возвращает путь к PID-файлу
func (c *Config) PIDFilePath() string {
	return c.Service.PIDFilePath
}

// LoadConfig загружает конфигурацию приложения из переменных окружения, .env файла или config.yml
func LoadConfig() (*Config, error) {
	// Загрузка переменных окружения из .env файла, если он существует
	if err := godotenv.Load(); err != nil {
		// Если .env файла не существует, это нормально, мы будем полагаться на переменные окружения или config.yml
	}

	// Попытка загрузки из config.yml сначала
	cfg, err := loadFromYAML("config.yml")
	if err != nil {
		// Если загрузка YAML не удалась, используем переменные окружения
		cfg, err = loadFromEnv()
		if err != nil {
			return nil, fmt.Errorf("не удалось загрузить конфигурацию из env: %w", err)
		}
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

// loadFromEnv загружает конфигурацию из переменных окружения (обратная совместимость)
func loadFromEnv() (*Config, error) {
	apiIDStr := getEnv("API_ID", "")
	apiHash := getEnv("API_HASH", "")
	phoneNumber := getEnv("PHONE_NUMBER", "")
	sessionFile := getEnv("SESSION_FILE", "tg.session")
	host := getEnv("SERVER_HOST", "0.0.0")
	portStr := getEnv("SERVER_PORT", "8080")
	taskTimeoutStr := getEnv("TASK_TIMEOUT_SECONDS", "30")
	cacheTTLStr := getEnv("CACHE_TTL_MINUTES", "60")
	requestDelayStr := getEnv("REQUEST_DELAY", "1000")
	rateLimiterStrategy := getEnv("RATE_LIMITER_STRATEGY", "always_delay")
	throttledRetriesStr := getEnv("THROTTLED_RETRIES", "5")
	cooldownDurationStr := getEnv("COOLDOWN_DURATION_SECONDS", "60")

	if apiIDStr == "" || apiHash == "" || phoneNumber == "" {
		return nil, fmt.Errorf("API_ID, API_HASH и PHONE_NUMBER должны быть установлены в переменных окружения")
	}

	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый API_ID: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый SERVER_PORT: %w", err)
	}

	taskTimeout, err := strconv.Atoi(taskTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый TASK_TIMEOUT_SECONDS: %w", err)
	}

	cacheTL, err := strconv.Atoi(cacheTTLStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый CACHE_TTL_MINUTES: %w", err)
	}

	requestDelay, err := strconv.Atoi(requestDelayStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый REQUEST_DELAY: %w", err)
	}

	throttledRetries, err := strconv.Atoi(throttledRetriesStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый THROTTLED_RETRIES: %w", err)
	}

	cooldownDuration, err := strconv.Atoi(cooldownDurationStr)
	if err != nil {
		return nil, fmt.Errorf("недопустимый COOLDOWN_DURATION_SECONDS: %w", err)
	}

	return &Config{
		Server: Server{
			Host: host,
			Port: port,
		},
		TelegramAPI: TelegramAPI{
			APIID:       apiID,
			APIHash:     apiHash,
			PhoneNumber: phoneNumber,
			SessionFile: sessionFile,
		},
		Processing: Processing{
			TaskTimeoutSeconds: taskTimeout,
			CacheTTLMinutes:    cacheTL,
		},
		RateLimiter: RateLimiter{
			Strategy:                rateLimiterStrategy,
			RequestDelayMs:          requestDelay,
			ThrottledRetries:        throttledRetries,
			CooldownDurationSeconds: cooldownDuration,
		},
	}, nil
}

// Address возвращает адрес сервера в формате "host:port"
func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// Validate проверяет, являются ли значения конфигурации допустимыми
func (c *Config) Validate() error {
	if c.TelegramAPI.APIID <= 0 {
		return fmt.Errorf("telegram_api.api_id должно быть положительным целым числом")
	}

	if c.TelegramAPI.APIHash == "" {
		return fmt.Errorf("telegram_api.api_hash не может быть пустым")
	}

	if c.TelegramAPI.PhoneNumber == "" {
		return fmt.Errorf("telegram_api.phone_number не может быть пустым")
	}

	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port должен быть действительным номером порта (1-65535)")
	}

	if c.Processing.TaskTimeoutSeconds < 0 {
		return fmt.Errorf("processing.task_timeout_seconds должно быть неотрицательным (0 для отсутствия ограничений)")
	}

	if c.Processing.CacheTTLMinutes <= 0 {
		return fmt.Errorf("processing.cache_ttl_minutes должно быть положительным целым числом")
	}

	if c.RateLimiter.RequestDelayMs <= 0 {
		return fmt.Errorf("rate_limiter.request_delay_ms должно быть положительным целым числом")
	}

	if c.RateLimiter.ThrottledRetries <= 0 {
		return fmt.Errorf("rate_limiter.throttled_retries должно быть положительным целым числом")
	}

	if c.RateLimiter.CooldownDurationSeconds <= 0 {
		return fmt.Errorf("rate_limiter.cooldown_duration_seconds должно быть положительным целым числом")
	}

	if c.RateLimiter.Strategy != "always_delay" && c.RateLimiter.Strategy != "adaptive" {
		return fmt.Errorf("rate_limiter.strategy должно быть равно 'always_delay' или 'adaptive'")
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
