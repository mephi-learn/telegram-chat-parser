package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// ColumnWidths определяет ширину колонок для текстового вывода.
type ColumnWidths struct {
	User    int `yaml:"user"`
	Name    int `yaml:"name"`
	Bio     int `yaml:"bio"`
	Channel int `yaml:"channel"`
}

// BotConfig содержит конфигурацию для Telegram-бота
type BotConfig struct {
	Token                  string       `yaml:"token"`
	BackendURL             string       `yaml:"backend_url"`
	PollingIntervalSeconds int          `yaml:"polling_interval_seconds"`
	ExcelThreshold         int          `yaml:"excel_threshold"`
	MaxFilesPerMessage     int          `yaml:"max_files_per_message"`
	HTTPTimeoutSeconds     int          `yaml:"http_timeout_seconds"`
	Render                 ColumnWidths `yaml:"render"`
}

// Logging содержит конфигурацию логирования
type Logging struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
}

// Config является оберткой для соответствия структуре YAML файла.
type Config struct {
	Bot     BotConfig `yaml:"bot"`
	Logging Logging   `yaml:"logging"`
}

// LoadBotConfig загружает конфигурацию бота из указанного файла.
func LoadBotConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read bot config file %s: %w", filename, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bot config: %w", err)
	}

	// Устанавливаем значения по умолчанию
	botCfg := &cfg.Bot
	if botCfg.MaxFilesPerMessage == 0 {
		botCfg.MaxFilesPerMessage = DefaultMaxFilesPerMessage
	}
	if botCfg.HTTPTimeoutSeconds == 0 {
		botCfg.HTTPTimeoutSeconds = DefaultHTTPTimeoutSeconds
	}
	if botCfg.Render.User == 0 {
		botCfg.Render.User = DefaultUserColumnWidth
	}
	if botCfg.Render.Name == 0 {
		botCfg.Render.Name = DefaultNameColumnWidth
	}
	if botCfg.Render.Bio == 0 {
		botCfg.Render.Bio = DefaultBioColumnWidth
	}
	if botCfg.Render.Channel == 0 {
		botCfg.Render.Channel = DefaultChannelColumnWidth
	}

	// Устанавливаем значения по умолчанию для логирования
	logging := &cfg.Logging
	if logging.Level == "" {
		logging.Level = DefaultLogLevel
	}
	if logging.Format == "" {
		logging.Format = DefaultLogFormat
	}

	return &cfg, nil
}

// Validate проверяет корректность конфигурации бота.
func (c *BotConfig) Validate() error {
	if c.Token == "" || c.Token == "YOUR_TELEGRAM_BOT_TOKEN" {
		return fmt.Errorf("bot.token is not configured")
	}
	if c.BackendURL == "" {
		return fmt.Errorf("bot.backend_url cannot be empty")
	}
	if c.PollingIntervalSeconds <= 0 {
		return fmt.Errorf("bot.polling_interval_seconds must be positive")
	}
	if c.ExcelThreshold <= 0 {
		return fmt.Errorf("bot.excel_threshold must be positive")
	}
	if c.MaxFilesPerMessage <= 0 {
		return fmt.Errorf("bot.max_files_per_message must be positive")
	}
	return nil
}

// ValidateFull проверяет корректность всей конфигурации, включая логирование.
func (c *Config) ValidateFull() error {
	if err := c.Bot.Validate(); err != nil {
		return err
	}

	if err := c.Logging.ValidateLogging(); err != nil {
		return err
	}

	return nil
}

// ValidateLogging проверяет корректность конфигурации логирования.
func (l *Logging) ValidateLogging() error {
	switch l.Level {
	case "debug", "info", "warn", "error", "":
		// all good
	default:
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}

	switch l.Format {
	case "json", "text", "":
		// all good
	default:
		return fmt.Errorf("logging.format must be one of: json, text")
	}

	return nil
}
