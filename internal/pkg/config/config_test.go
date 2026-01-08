package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiServerYAML представляет современный формат конфигурации с несколькими серверами.
const multiServerYAML = `
server:
  host: "127.0.0.1"
  port: 8081
  shutdown_timeout: 15s
telegram_api:
  servers:
    - api_id: 12345
      api_hash: "hash1"
      phone_number: "+111"
      session_file: "tg1.session"
    - api_id: 67890
      api_hash: "hash2"
      phone_number: "+222"
      session_file: "tg2.session"
  health_check_interval: 60s
processing:
  task_timeout: 120s
  cache_ttl: 30m
enrichment:
  pool_size: 5
  client_retry_pause: 10s
logging:
  level: "info"
`

// legacyYAML представляет устаревший формат для проверки обратной совместимости.
const legacyYAML = `
server:
  host: "0.0.0.0"
  port: 8080
  shutdown_timeout_seconds: 5
telegram_api:
  api_id: 98765
  api_hash: "legacy_hash"
  phone_number: "+333"
  session_file: "legacy.session"
  health_check_interval_seconds: 30
processing:
  task_timeout_seconds: 0
  cache_ttl_minutes: 60
enrichment:
  pool_size: 1
  client_retry_pause_seconds: 1
logging:
  level: "debug"
`

func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

func TestLoadFromYAML(t *testing.T) {
	t.Run("success with multi-server format", func(t *testing.T) {
		path := createTempConfigFile(t, multiServerYAML)
		cfg := defaultConfig()
		err := loadFromYAML(path, cfg)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "127.0.0.1", cfg.Server.Host)
		assert.Equal(t, 8081, cfg.Server.Port)
		assert.Equal(t, 15*time.Second, cfg.Server.ShutdownTimeout)
		assert.Equal(t, "127.0.0.1:8081", cfg.Address())

		require.Len(t, cfg.TelegramAPI.Servers, 2)
		assert.Equal(t, 12345, cfg.TelegramAPI.Servers[0].APIID)
		assert.Equal(t, "hash1", cfg.TelegramAPI.Servers[0].APIHash)
		assert.Equal(t, 67890, cfg.TelegramAPI.Servers[1].APIID)
		assert.Equal(t, "hash2", cfg.TelegramAPI.Servers[1].APIHash)
		assert.Equal(t, 60*time.Second, cfg.TelegramAPI.HealthCheckInterval)

		assert.Equal(t, 120*time.Second, cfg.Processing.TaskTimeout)
		assert.Equal(t, 30*time.Minute, cfg.Processing.CacheTTL)
		assert.Equal(t, 5, cfg.Enrichment.PoolSize)
		assert.Equal(t, 10*time.Second, cfg.Enrichment.ClientRetryPause)
		assert.Equal(t, "info", cfg.Logging.Level)
	})

	t.Run("file not found is not an error", func(t *testing.T) {
		cfg := defaultConfig()
		err := loadFromYAML("non_existent_file.yml", cfg)
		assert.NoError(t, err)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := createTempConfigFile(t, "invalid yaml: {")
		cfg := defaultConfig()
		err := loadFromYAML(path, cfg)
		assert.Error(t, err)
	})
}

func TestGetTelegramServers(t *testing.T) {
	t.Run("from modern config", func(t *testing.T) {
		cfg := defaultConfig()
		err := loadFromYAML(createTempConfigFile(t, multiServerYAML), cfg)
		require.NoError(t, err)
		servers := cfg.GetTelegramServers()
		require.Len(t, servers, 2)
		assert.Equal(t, 12345, servers[0].APIID)
		assert.Equal(t, 67890, servers[1].APIID)
	})

	t.Run("empty config returns nil", func(t *testing.T) {
		cfg := &Config{}
		servers := cfg.GetTelegramServers()
		assert.Nil(t, servers)
	})
}

func TestValidate(t *testing.T) {
	validConfig := func(t *testing.T) *Config {
		cfg := defaultConfig()
		err := loadFromYAML(createTempConfigFile(t, multiServerYAML), cfg)
		require.NoError(t, err)
		return cfg
	}

	testCases := []struct {
		name    string
		mutator func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"no servers", func(c *Config) { c.TelegramAPI.Servers = nil }, true},
		{"invalid server api_id", func(c *Config) { c.TelegramAPI.Servers[0].APIID = 0 }, true},
		{"empty server api_hash", func(c *Config) { c.TelegramAPI.Servers[0].APIHash = "" }, true},
		{"empty server phone", func(c *Config) { c.TelegramAPI.Servers[0].PhoneNumber = "" }, true},
		{"invalid port", func(c *Config) { c.Server.Port = 0 }, true},
		{"invalid shutdown timeout", func(c *Config) { c.Server.ShutdownTimeout = 0 }, true},
		{"invalid task_timeout", func(c *Config) { c.Processing.TaskTimeout = -1 }, true},
		{"invalid cache_ttl", func(c *Config) { c.Processing.CacheTTL = 0 }, true},
		{"invalid health_check", func(c *Config) { c.TelegramAPI.HealthCheckInterval = 0 }, true},
		{"invalid pool_size", func(c *Config) { c.Enrichment.PoolSize = 0 }, true},
		{"invalid retry_pause", func(c *Config) { c.Enrichment.ClientRetryPause = 0 }, true},
		{"invalid logging level", func(c *Config) { c.Logging.Level = "wrong" }, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig(t)
			tc.mutator(cfg)
			err := cfg.Validate()
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
