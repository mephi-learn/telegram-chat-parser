package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"telegram-chat-parser/cmd/bot/config"
	"telegram-chat-parser/internal/bot"
	"telegram-chat-parser/internal/log"
)

func main() {
	// Загрузка конфигурации бота
	cfg, err := config.LoadBotConfig("bot_config.yml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load bot config: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.ValidateFull(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to validate bot config: %v\n", err)
		os.Exit(1)
	}

	// Инициализация логгера с маскировкой токенов и настройками из конфига
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

	var handler slog.Handler
	switch cfg.Logging.Format {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}

	logger := log.NewMaskedLogger(handler)
	slog.SetDefault(logger)

	// Инициализация компонентов
	taskStore := bot.NewTaskStore()
	serverClient := bot.NewServerClient(cfg.Bot.BackendURL)

	b, err := bot.NewBot(cfg.Bot, serverClient, taskStore, logger.With(slog.String("component", "bot")))
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
