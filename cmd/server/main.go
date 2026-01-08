package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"telegram-chat-parser/internal/adapters/parser"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/core/services"
	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/server"
	"telegram-chat-parser/internal/server/usecase"
	"telegram-chat-parser/internal/telegram/router"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application run failed", "error", err)
		os.Exit(1)
	}
}

// run инкапсулирует всю логику инициализации и запуска приложения.
func run() error {
	// 1. Загрузка и валидация конфигурации
	cfg, err := config.LoadConfig()
	if err != nil {
		// Логгер еще не инициализирован, выводим в stderr
		_, _ = fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 2. Инициализация логгера
	var level slog.Level
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// 3. Валидация конфигурации (после инициализации логгера)
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}
	// 4. Инициализация и запуск фоновых сервисов
	appCtx, appCancel := context.WithCancel(context.Background())

	tgServers := cfg.GetTelegramServers()
	tgRouter, err := router.NewRouter(appCtx,
		router.WithServerConfigs(tgServers),
		router.WithHealthCheckInterval(cfg.TelegramAPI.HealthCheckInterval),
	)
	if err != nil {
		appCancel()
		return fmt.Errorf("failed to create telegram router: %w", err)
	}

	// 4. Инициализация зависимостей
	taskStore := server.NewTaskStore()
	cacheStore := cache.NewCacheStore()
	parserSvc := parser.NewJsonParser()
	extractorSvc := services.NewExtractionService()
	enricherSvc := services.NewEnrichmentService(tgRouter,
		cfg.Enrichment.PoolSize,
		cfg.Enrichment.ClientRetryPause,
		cfg.Enrichment.OperationTimeout,
	)
	processor := usecase.NewProcessChatUseCase(cfg, parserSvc, extractorSvc, enricherSvc, cacheStore)

	// 5. Создание HTTP-сервера
	srv, err := server.New(cfg, processor, taskStore, cacheStore)
	if err != nil {
		appCancel()
		return fmt.Errorf("failed to create server: %w", err)
	}

	// 6. Запуск сервера и graceful shutdown
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		slog.Info("Starting server", "addr", cfg.Address())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Signal received, shutting down...")

	// Сначала отменяем контекст приложения, чтобы остановить фоновые процессы (клиенты Telegram)
	appCancel()
	slog.Info("Application context canceled, waiting for background services to stop...")

	// Затем останавливаем HTTP-сервер
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	<-serverDone
	slog.Info("HTTP server stopped")

	// В конце останавливаем роутер (его health-check тикер)
	tgRouter.Stop()

	slog.Info("Application exited gracefully")
	return nil
}
