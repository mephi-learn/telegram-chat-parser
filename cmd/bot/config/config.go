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
	Render                 ColumnWidths `yaml:"render"`
}

// Config является оберткой для соответствия структуре YAML файла.
type Config struct {
	Bot BotConfig `yaml:"bot"`
}

// LoadBotConfig загружает конфигурацию бота из указанного файла.
func LoadBotConfig(filename string) (*BotConfig, error) {
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

	return botCfg, nil
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
	return nil
}
