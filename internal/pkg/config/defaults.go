package config

import "time"

// Default values for configuration.
const (
	// Server defaults
	DefaultServerHost      = "0.0.0.0"
	DefaultServerPort      = 8080
	DefaultReadTimeout     = 10 * time.Second
	DefaultWriteTimeout    = 10 * time.Second
	DefaultIdleTimeout     = 60 * time.Second
	DefaultShutdownTimeout = 15 * time.Second
	DefaultMaxUploadSizeMB = 10
	DefaultCleanupInterval = 1 * time.Hour

	// Processing defaults
	DefaultTaskTimeout = 600 * time.Second
	DefaultCacheTTL    = 60 * time.Minute

	// Telegram API defaults
	DefaultHealthCheckInterval  = 30 * time.Second
	DefaultTelegramRequestDelay = 0 * time.Second

	// Enrichment defaults
	DefaultEnrichmentPoolSize         = 1
	DefaultEnrichmentClientRetryPause = 1 * time.Second
	DefaultEnrichmentOperationTimeout = 5 * time.Second

	// Logging defaults
	DefaultLogLevel  = "info"
	DefaultLogFormat = "json"
)
