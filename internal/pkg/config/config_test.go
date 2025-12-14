package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multiServerYAML представляет современный формат конфигурации с несколькими серверами.
const multiServerYAML = `
server:
  host: "127.0.0.1"
  port: 8081
  shutdown_timeout_seconds: 15
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
  health_check_interval_seconds: 60
processing:
  task_timeout_seconds: 120
  cache_ttl_minutes: 30
enrichment:
  pool_size: 5
  client_retry_pause_seconds: 10
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
		cfg, err := loadFromYAML(path)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "127.0.0.1", cfg.Server.Host)
		assert.Equal(t, 8081, cfg.Server.Port)
		assert.Equal(t, 15, cfg.Server.ShutdownTimeoutSeconds)
		assert.Equal(t, "127.0.0.1:8081", cfg.Address())

		require.Len(t, cfg.TelegramAPI.Servers, 2)
		assert.Equal(t, 12345, cfg.TelegramAPI.Servers[0].APIID)
		assert.Equal(t, "hash1", cfg.TelegramAPI.Servers[0].APIHash)
		assert.Equal(t, 67890, cfg.TelegramAPI.Servers[1].APIID)
		assert.Equal(t, "hash2", cfg.TelegramAPI.Servers[1].APIHash)
		assert.Equal(t, 60, cfg.TelegramAPI.HealthCheckIntervalSeconds)

		assert.Equal(t, 120, cfg.Processing.TaskTimeoutSeconds)
		assert.Equal(t, 30, cfg.Processing.CacheTTLMinutes)
		assert.Equal(t, 5, cfg.Enrichment.PoolSize)
		assert.Equal(t, 10, cfg.Enrichment.ClientRetryPauseSeconds)
		assert.Equal(t, "info", cfg.Logging.Level)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := loadFromYAML("non_existent_file.yml")
		assert.Error(t, err)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := createTempConfigFile(t, "invalid yaml: {")
		_, err := loadFromYAML(path)
		assert.Error(t, err)
	})
}

func TestGetTelegramServers(t *testing.T) {
	t.Run("from modern config", func(t *testing.T) {
		cfg, err := loadFromYAML(createTempConfigFile(t, multiServerYAML))
		require.NoError(t, err)
		servers := cfg.GetTelegramServers()
		require.Len(t, servers, 2)
		assert.Equal(t, 12345, servers[0].APIID)
		assert.Equal(t, 67890, servers[1].APIID)
	})

	t.Run("from legacy config", func(t *testing.T) {
		cfg, err := loadFromYAML(createTempConfigFile(t, legacyYAML))
		require.NoError(t, err)
		servers := cfg.GetTelegramServers()
		require.Len(t, servers, 1)
		assert.Equal(t, 98765, servers[0].APIID)
		assert.Equal(t, "legacy_hash", servers[0].APIHash)
	})

	t.Run("empty config returns nil", func(t *testing.T) {
		cfg := &Config{}
		servers := cfg.GetTelegramServers()
		assert.Nil(t, servers)
	})
}

func TestLoadFromEnv(t *testing.T) {
	setEnv := func(t *testing.T, key, value string) {
		t.Helper()
		origValue, isSet := os.LookupEnv(key)
		os.Setenv(key, value)
		t.Cleanup(func() {
			if isSet {
				os.Setenv(key, origValue)
			} else {
				os.Unsetenv(key)
			}
		})
	}

	t.Run("success", func(t *testing.T) {
		setEnv(t, "API_ID", "54321")
		setEnv(t, "API_HASH", "fedcba")
		setEnv(t, "PHONE_NUMBER", "+0987654321")
		setEnv(t, "SERVER_PORT", "9090")

		cfg, err := loadFromEnv()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 54321, cfg.TelegramAPI.APIID)
		assert.Equal(t, "fedcba", cfg.TelegramAPI.APIHash)
		assert.Equal(t, "+0987654321", cfg.TelegramAPI.PhoneNumber)
		assert.Equal(t, 9090, cfg.Server.Port)
	})

	t.Run("missing required env", func(t *testing.T) {
		// Используем t.Cleanup для гарантии очистки
		t.Setenv("API_ID", "1")
		t.Setenv("API_HASH", "hash")
		os.Unsetenv("PHONE_NUMBER")

		_, err := loadFromEnv()
		assert.Error(t, err)
	})

	t.Run("invalid int env", func(t *testing.T) {
		t.Setenv("API_ID", "not-an-int")
		t.Setenv("API_HASH", "hash")
		t.Setenv("PHONE_NUMBER", "phone")
		_, err := loadFromEnv()
		assert.Error(t, err)
	})
}

func TestValidate(t *testing.T) {
	validConfig := func(t *testing.T) *Config {
		cfg, err := loadFromYAML(createTempConfigFile(t, multiServerYAML))
		require.NoError(t, err)
		return cfg
	}

	testCases := []struct {
		name    string
		mutator func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"no servers", func(c *Config) { c.TelegramAPI.Servers = nil; c.TelegramAPI.APIID = 0 }, true},
		{"invalid server api_id", func(c *Config) { c.TelegramAPI.Servers[0].APIID = 0 }, true},
		{"empty server api_hash", func(c *Config) { c.TelegramAPI.Servers[0].APIHash = "" }, true},
		{"empty server phone", func(c *Config) { c.TelegramAPI.Servers[0].PhoneNumber = "" }, true},
		{"invalid port", func(c *Config) { c.Server.Port = 0 }, true},
		{"invalid shutdown timeout", func(c *Config) { c.Server.ShutdownTimeoutSeconds = 0 }, true},
		{"invalid task_timeout", func(c *Config) { c.Processing.TaskTimeoutSeconds = -1 }, true},
		{"invalid cache_ttl", func(c *Config) { c.Processing.CacheTTLMinutes = 0 }, true},
		{"invalid health_check", func(c *Config) { c.TelegramAPI.HealthCheckIntervalSeconds = 0 }, true},
		{"invalid pool_size", func(c *Config) { c.Enrichment.PoolSize = 0 }, true},
		{"invalid retry_pause", func(c *Config) { c.Enrichment.ClientRetryPauseSeconds = 0 }, true},
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

func TestGetEnv(t *testing.T) {
	key := "TEST_GET_ENV"
	t.Setenv(key, "my-value")
	assert.Equal(t, "my-value", getEnv(key, "default"))
	assert.Equal(t, "default", getEnv("NON_EXISTENT_KEY", "default"))
}
