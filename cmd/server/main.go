package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"telegram-chat-parser/internal/adapters/parser"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/core/services"
	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ratelimiter"
	"telegram-chat-parser/internal/server"
	"telegram-chat-parser/internal/server/usecase"
	telegram_auth "telegram-chat-parser/internal/telegram"
	"time"

	"github.com/gotd/td/telegram"

	"github.com/sevlyar/go-daemon"
)

func main() {
	// Парсим аргументы командной строки
	action := flag.String("action", "run", "Action: start, stop, restart, run (run is interactive mode)")
	flag.Parse()

	// Загружаем конфигурацию
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Валидируем конфигурацию
	if err := cfg.Validate(); err != nil {
		slog.Error("Config validation failed", "error", err)
		os.Exit(1)
	}

	pidFilePath := cfg.PIDFilePath()

	switch *action {
	case "start":
		start(cfg, pidFilePath)
	case "stop":
		stop(pidFilePath)
	case "restart":
		stop(pidFilePath)
		// Небольшая задержка перед перезапуском
		time.Sleep(1 * time.Second)
		start(cfg, pidFilePath)
	case "run":
		// Создаем AuthManager
		authManager := telegram_auth.NewAuthManager(
			cfg.TelegramAPI.APIID,
			cfg.TelegramAPI.APIHash,
			cfg.TelegramAPI.PhoneNumber,
			"tg.session", // путь к файлу сессии
		)

		// Запускаем аутентифицированный клиент Telegram и внутри него инициализируем и запускаем HTTP-сервер.
		slog.Info("Starting Telegram client and server...")
		if err := authManager.RunAuthenticatedClient(context.Background(), func(ctx context.Context, client *telegram.Client) error {
			return runServerWithContext(ctx, cfg, client)
		}); err != nil {
			slog.Error("Telegram client error", "error", err)
			os.Exit(1)
		}
	default:
		slog.Error("Unknown action", "action", *action)
		os.Exit(1)
	}
}

func start(cfg *config.Config, pidFilePath string) {
	// Проверяем, не запущен ли сервер уже (по PID-файлу или по процессу)
	if pidFilePath != "" {
		if isRunning(pidFilePath) {
			slog.Error("Server is already running (PID file check)")
			os.Exit(1)
		}
	} else {
		// Если PID-файл не задан, используем проверку по процессу
		if isRunningByName() {
			slog.Error("Server is already running (process name check)")
			os.Exit(1)
		}
	}

	// Создаём контекст для демона
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Настройка параметров демона
	dctx := &daemon.Context{
		// Убираем PidFileName, так как не полагаемся на него
		// PidFileName: pidFilePath,
		PidFilePerm: 0644,
		LogFileName: "", // Можно указать файл лога, если нужно
		LogFilePerm: 0640,
		WorkDir:     "./", // Рабочая директория
		Umask:       027,
		Args:        os.Args[1:], // Передаём аргументы, но меняем "start" на "run"
	}

	// Заменяем аргументы для дочернего процесса
	args := make([]string, len(os.Args))
	copy(args, os.Args)
	for i, arg := range args {
		if arg == "-action=start" || arg == "--action=start" {
			args[i] = "-action=run"
			break
		}
	}
	dctx.Args = args

	// Запускаем дочерний процесс (демон)
	child, err := dctx.Reborn()
	if err != nil {
		slog.Error("Failed to daemonize", "error", err)
		os.Exit(1)
	}

	if child != nil {
		// Родительский процесс завершает работу после запуска демона
		slog.Info("Server started in background")
		return
	}

	// В дочернем процессе (демоне) запускаем аутентифицированный клиент Telegram и внутри него инициализируем и запускаем HTTP-сервер.
	defer dctx.Release()
	// PID-файл не используем, так как go-daemon его не заполняет

	authManager := telegram_auth.NewAuthManager(
		cfg.TelegramAPI.APIID,
		cfg.TelegramAPI.APIHash,
		cfg.TelegramAPI.PhoneNumber,
		"tg.session", // путь к файлу сессии
	)

	if err := authManager.RunAuthenticatedClient(context.Background(), func(ctx context.Context, client *telegram.Client) error {
		return runServerWithContext(ctx, cfg, client)
	}); err != nil {
		slog.Error("Telegram client error in daemon mode", "error", err)
		os.Exit(1)
	}
}

func stop(pidFilePath string) {
	var pid int
	var proc *os.Process
	var err error

	if pidFilePath != "" {
		// Сначала пробуем остановить по PID из файла
		pid, err = daemon.ReadPidFile(pidFilePath)
		if err == nil {
			proc, err = os.FindProcess(pid)
			if err != nil {
				slog.Warn("PID file exists but process not found via os.FindProcess", "pid", pid, "error", err)
				// Возможно, процесс уже умер, но файл не был удалён. Попробуем по имени.
				goto stop_by_name
			}
		} else {
			slog.Warn("Could not read PID file, attempting to stop by process name", "error", err)
			// Если не удалось прочитать PID-файл, пробуем остановить по имени
			goto stop_by_name
		}
	} else {
		// Если PID-файл не задан, останавливаем по имени
		goto stop_by_name
	}

	// Отправляем SIGTERM процессу, найденному по PID
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		slog.Warn("Failed to send SIGTERM to process from PID file, attempting to stop by process name", "pid", pid, "error", err)
		// Если не удалось отправить сигнал по PID, пробуем остановить по имени
		goto stop_by_name
	}

	// Ждём, пока процесс завершится
	for i := 0; i < 50; i++ { // Ожидаем до 5 секунд (50 * 100ms)
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); os.IsNotExist(err) {
			// Процесс завершён, удаляем PID-файл, если он был
			if pidFilePath != "" {
				if err := os.Remove(pidFilePath); err != nil {
					slog.Warn("Failed to remove PID file", "pid_file", pidFilePath, "error", err)
				} else {
					slog.Info("Server stopped and PID file removed", "pid_file", pidFilePath)
				}
			} else {
				slog.Info("Server stopped (no PID file configured)")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Если процесс не завершился, принудительно убиваем
	slog.Warn("Server did not stop gracefully, killing it", "pid", pid)
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		slog.Error("Failed to send SIGKILL", "pid", pid, "error", err)
		os.Exit(1)
	}

	// Удаляем PID-файл после убийства
	if pidFilePath != "" {
		if err := os.Remove(pidFilePath); err != nil {
			slog.Warn("Failed to remove PID file after SIGKILL", "pid_file", pidFilePath, "error", err)
		} else {
			slog.Info("Server killed and PID file removed", "pid_file", pidFilePath)
		}
	} else {
		slog.Info("Server killed (no PID file configured)")
	}
	return

stop_by_name:
	// Остановка по имени процесса
	// Находим PID через ps
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		slog.Error("Could not execute 'ps aux' to find process for stop", "error", err)
		os.Exit(1)
	}

	psOutput := string(output)
	lines := strings.Split(psOutput, "\n")
	for _, line := range lines {
		if strings.Contains(line, "./server -action=run") || strings.Contains(line, "server -action=run") {
			// Найдена строка с нашим процессом
			// Разбиваем строку и пытаемся извлечь PID (второе поле в ps aux)
			fields := strings.Fields(line)
			if len(fields) > 1 {
				pidStr := fields[1] // PID - второе поле
				pid, err := strconv.Atoi(pidStr)
				if err != nil {
					slog.Warn("Could not parse PID from ps output", "pid_str", pidStr, "line", line)
					continue
				}
				// Нашли PID, пытаемся отправить сигнал
				proc, err := os.FindProcess(pid)
				if err != nil {
					slog.Warn("Found PID from ps, but os.FindProcess failed", "pid", pid, "error", err)
					continue
				}
				if err := proc.Signal(syscall.SIGTERM); err != nil {
					slog.Warn("Failed to send SIGTERM to process found by name", "pid", pid, "error", err)
					// Пробуем SIGKILL
					if err := proc.Signal(syscall.SIGKILL); err != nil {
						slog.Error("Failed to send SIGKILL to process found by name", "pid", pid, "error", err)
						continue // Пробуем следующий процесс, если есть
					} else {
						slog.Info("Server killed by name (SIGKILL)", "pid", pid)
						return
					}
				} else {
					slog.Info("SIGTERM sent to server found by name, waiting for exit", "pid", pid)
					// Ждём завершения
					for i := 0; i < 50; i++ {
						if _, err := os.Stat("/proc/" + pidStr); os.IsNotExist(err) {
							slog.Info("Server stopped by name (SIGTERM)", "pid", pid)
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
					// Если не завершился по SIGTERM, пробуем SIGKILL
					slog.Warn("Server found by name did not stop gracefully, killing it", "pid", pid)
					if err := proc.Signal(syscall.SIGKILL); err != nil {
						slog.Error("Failed to send SIGKILL to process found by name after timeout", "pid", pid, "error", err)
					} else {
						slog.Info("Server killed by name after timeout (SIGKILL)", "pid", pid)
					}
					return
				}
			}
		}
	}
	slog.Error("Could not find server process to stop by name")
	os.Exit(1)
}

func isRunning(pidFilePath string) bool {
	if pidFilePath != "" {
		// Сначала пробуем проверить через PID-файл, если он указан
		pid, err := daemon.ReadPidFile(pidFilePath)
		if err == nil {
			// PID-файл существует и читаем
			proc, err := os.FindProcess(pid)
			if err == nil {
				// Процесс найден, проверим, жив ли он
				err = proc.Signal(syscall.Signal(0))
				return err == nil
			}
		}
		// Если PID-файл не помог (не существует, ошибка, процесс не найден), пробуем по имени
	}
	// Если PID-файл не указан или не читается, используем проверку по имени процесса
	return isRunningByName()
}

// isRunningByName проверяет, запущен ли сервер, по имени исполняемого файла в списке процессов.
func isRunningByName() bool {
	// Простая проверка через выполнение команды ps
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		// Если ps не сработал, не можем точно определить
		slog.Warn("Could not execute 'ps aux' to check if running", "error", err)
		return false
	}

	// Ищем выводе команду запуска сервера в режиме run
	// Например, "./server -action=run"
	// Используем простое вхождение строки
	psOutput := string(output)
	// Просто проверим, есть ли в выводе ps процесс с аргументом -action=run
	// Это не учитывает случай, когда запущено несколько разных экземпляров с разными путями.
	// Для учебного проекта этого достаточно.
	return strings.Contains(psOutput, "./server -action=run") || strings.Contains(psOutput, "server -action=run")
}

// runServerWithContext запускает HTTP-сервер и управляет его жизненным циклом.
// Она должна быть вызвана внутри client.Run цикла, например, через AuthManager.RunAuthenticatedClient.
func runServerWithContext(ctx context.Context, cfg *config.Config, tgClient *telegram.Client) error {
	slog.Info("runServerWithContext: Process started...")

	// tgClient гарантированно аутентифицирован.
	client := tgClient

	slog.Info("runServerWithContext: Telegram client ready, initializing server...")
	// Initialize dependencies
	taskStore := server.NewTaskStore()
	cacheStore := cache.NewCacheStore()
	rateLimiter := ratelimiter.NewAdaptiveRateLimiter(
		time.Duration(cfg.RateLimiter.RequestDelayMs)*time.Millisecond,
		cfg.RateLimiter.ThrottledRetries,
		time.Duration(cfg.RateLimiter.CooldownDurationSeconds)*time.Second,
	)
	parserSvc := parser.NewJsonParser()
	extractorSvc := services.NewExtractionService()
	enricherSvc := services.NewEnrichmentServiceWithClient(client)
	processor := usecase.NewProcessChatUseCase(cfg, parserSvc, extractorSvc, enricherSvc, cacheStore, rateLimiter)

	srv, err := server.New(cfg, processor, taskStore, cacheStore)
	if err != nil {
		slog.Error("Failed to create server", "error", err)
		// Возвращаем ошибку, чтобы она "всплыла" в client.Run и вызвала его остановку.
		return err
	}
	slog.Info("runServerWithContext: Server instance created, starting ListenAndServe goroutine...")

	// Запускаем сервер в горутине
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		slog.Info("Starting server", "addr", cfg.Address())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
			// Не возвращаем ошибку из горутины, а логируем.
			// Основная функция будет ждать serverDone.
		}
	}()
	slog.Info("runServerWithContext: ListenAndServe goroutine started, waiting for signal...")

	// Ждем сигнал остановки или отмены внешнего контекста.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done(): // Контекст отменен (например, при остановке клиента)
		slog.Info("Context cancelled, shutting down server...")
	case <-quit: // Получен сигнал остановки
		slog.Info("Signal received, shutting down server...")
	}

	// Плавное завершение HTTP-сервера
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		// Не вызываем os.Exit(1) здесь, так как мы внутри client.Run.
		// Достаточно логирования.
	}

	// Ждем, пока сервер полностью остановится.
	<-serverDone
	slog.Info("Server exited")

	// Возвращаем nil, чтобы client.Run завершился нормально.
	// Если был вызван srv.Shutdown, то ListenAndServe вернул http.ErrServerClosed или nil.
	return nil
}
