package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
server:
  host: "127.0.0.1"
  port: 8081
telegram_api:
  api_id: 12345
  api_hash: "abcdef"
  phone_number: "+1234567890"
  session_file: "test.session"
processing:
  task_timeout_seconds: 60
  cache_ttl_minutes: 30
rate_limiter:
  strategy: "adaptive"
  request_delay_ms: 500
  throttled_retries: 3
  cooldown_duration_seconds: 45
service:
  pid_file_path: "/var/run/test.pid"
`

func createTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

func TestLoadFromYAML(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		path := createTempConfigFile(t, validYAML)
		cfg, err := loadFromYAML(path)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "127.0.0.1", cfg.Server.Host)
		assert.Equal(t, 8081, cfg.Server.Port)
		assert.Equal(t, 12345, cfg.TelegramAPI.APIID)
		assert.Equal(t, "abcdef", cfg.TelegramAPI.APIHash)
		assert.Equal(t, "+1234567890", cfg.TelegramAPI.PhoneNumber)
		assert.Equal(t, "test.session", cfg.TelegramAPI.SessionFile)
		assert.Equal(t, 60, cfg.Processing.TaskTimeoutSeconds)
		assert.Equal(t, 30, cfg.Processing.CacheTTLMinutes)
		assert.Equal(t, "adaptive", cfg.RateLimiter.Strategy)
		assert.Equal(t, 500, cfg.RateLimiter.RequestDelayMs)
		assert.Equal(t, 3, cfg.RateLimiter.ThrottledRetries)
		assert.Equal(t, 45, cfg.RateLimiter.CooldownDurationSeconds)
		assert.Equal(t, "/var/run/test.pid", cfg.Service.PIDFilePath)
		assert.Equal(t, "/var/run/test.pid", cfg.PIDFilePath())
		assert.Equal(t, "127.0.0.1:8081", cfg.Address())
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
		setEnv(t, "API_ID", "1")
		setEnv(t, "API_HASH", "hash")
		os.Unsetenv("PHONE_NUMBER") // Ensure it's not set
		_, err := loadFromEnv()
		assert.Error(t, err)
	})

	t.Run("invalid int env", func(t *testing.T) {
		setEnv(t, "API_ID", "not-an-int")
		setEnv(t, "API_HASH", "hash")
		setEnv(t, "PHONE_NUMBER", "phone")
		_, err := loadFromEnv()
		assert.Error(t, err)
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("yaml has priority", func(t *testing.T) {
		// Set env var
		os.Setenv("SERVER_PORT", "9999")
		t.Cleanup(func() { os.Unsetenv("SERVER_PORT") })

		// Create YAML file
		wd, _ := os.Getwd()
		path := filepath.Join(wd, "config.yml")
		err := os.WriteFile(path, []byte("server:\n  port: 8888"), 0644)
		require.NoError(t, err)
		t.Cleanup(func() { os.Remove(path) })

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, 8888, cfg.Server.Port)
	})

	t.Run("fallback to env", func(t *testing.T) {
		// Ensure config.yml does not exist
		os.Remove("config.yml")

		os.Setenv("API_ID", "123")
		os.Setenv("API_HASH", "abc")
		os.Setenv("PHONE_NUMBER", "123")
		t.Cleanup(func() {
			os.Unsetenv("API_ID")
			os.Unsetenv("API_HASH")
			os.Unsetenv("PHONE_NUMBER")
		})

		cfg, err := LoadConfig()
		require.NoError(t, err)
		assert.Equal(t, 123, cfg.TelegramAPI.APIID)
	})
}

func TestValidate(t *testing.T) {
	// A valid config to start with
	validConfig := func() *Config {
		cfg, _ := loadFromYAML(createTempConfigFile(t, validYAML))
		return cfg
	}

	testCases := []struct {
		name    string
		mutator func(*Config)
		wantErr bool
	}{
		{"valid", func(c *Config) {}, false},
		{"invalid api_id", func(c *Config) { c.TelegramAPI.APIID = 0 }, true},
		{"empty api_hash", func(c *Config) { c.TelegramAPI.APIHash = "" }, true},
		{"empty phone", func(c *Config) { c.TelegramAPI.PhoneNumber = "" }, true},
		{"invalid port", func(c *Config) { c.Server.Port = 0 }, true},
		{"invalid task_timeout", func(c *Config) { c.Processing.TaskTimeoutSeconds = -1 }, true},
		{"invalid cache_ttl", func(c *Config) { c.Processing.CacheTTLMinutes = 0 }, true},
		{"invalid delay", func(c *Config) { c.RateLimiter.RequestDelayMs = 0 }, true},
		{"invalid retries", func(c *Config) { c.RateLimiter.ThrottledRetries = 0 }, true},
		{"invalid cooldown", func(c *Config) { c.RateLimiter.CooldownDurationSeconds = 0 }, true},
		{"invalid strategy", func(c *Config) { c.RateLimiter.Strategy = "wrong" }, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
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
	os.Setenv(key, "my-value")
	t.Cleanup(func() { os.Unsetenv(key) })

	assert.Equal(t, "my-value", getEnv(key, "default"))
	assert.Equal(t, "default", getEnv("NON_EXISTENT_KEY", "default"))
}
