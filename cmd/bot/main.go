package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"telegram-chat-parser/cmd/bot/config"
	"telegram-chat-parser/internal/bot"
)

func main() {
	// Инициализация логгера
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Загрузка конфигурации бота
	cfg, err := config.LoadBotConfig("bot_config.yml")
	if err != nil {
		slog.Error("failed to load bot config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("failed to validate bot config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Инициализация компонентов
	taskStore := bot.NewTaskStore()
	serverClient := bot.NewServerClient(cfg.BackendURL)

	b, err := bot.NewBot(*cfg, serverClient, taskStore, logger.With(slog.String("component", "bot")))
	if err != nil {
		slog.Error("failed to create bot", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.Info("Bot created successfully, starting...")

	// Ожидание сигналов для graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Запуск бота в отдельной goroutine, чтобы не блокировать graceful shutdown
	go b.Start(ctx)

	<-ctx.Done() // Ожидаем сигнал завершения

	slog.Info("Shutting down bot...")

	// TODO: Реализовать graceful shutdown
	// bot.Stop() // TODO: Реализовать метод Stop() в боте для корректного завершения

	slog.Info("Bot stopped gracefully")
}
