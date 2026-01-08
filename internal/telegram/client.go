package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"

	trm "telegram-chat-parser/internal/pkg/term"
)

var (
	// ErrFloodWaitActive возвращается, когда клиент не может выполнить запрос из-за активного ограничения FLOOD_WAIT.
	ErrFloodWaitActive = errors.New("client is in flood wait")
	// floodWaitRegex используется для парсинга длительности ожидания из сообщения об ошибке.
	floodWaitRegex = regexp.MustCompile(`FLOOD_WAIT \((\d+)\)`)
)

// telegramAPI представляет необработанные методы API, которые мы используем.
type telegramAPI interface {
	UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error)
	ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
	HelpGetConfig(ctx context.Context) (*tg.Config, error)
}

// telegramAuth представляет клиент аутентификации.
type telegramAuth interface {
	auth.FlowClient
}

// telegramRunner определяет зависимости от клиента gotd.
// Это позволяет создавать моки в тестах.
type telegramRunner interface {
	Run(ctx context.Context, f func(ctx context.Context) error) error
	API() telegramAPI
	Auth() telegramAuth
}

// prodRunner является оберткой вокруг реального *telegram.Client для удовлетворения интерфейса telegramRunner.
type prodRunner struct {
	*telegram.Client
}

func (p *prodRunner) API() telegramAPI {
	return p.Client.API()
}

func (p *prodRunner) Auth() telegramAuth {
	return p.Client.Auth()
}

// authFlow определяет интерфейс для процесса аутентификации.
type authFlow interface {
	Run(ctx context.Context, client auth.FlowClient) error
}

// authenticator определяет интерфейс для получения учетных данных пользователя.
type authenticator interface {
	auth.UserAuthenticator
}

// Client представляет собой потокобезопасный клиент для Telegram API,
// который инкапсулирует логику аутентификации, обработки ошибок FLOOD_WAIT и выполнения запросов.
type Client struct {
	id         string
	tgRunner   telegramRunner
	authFlow   authFlow
	isTerminal func(fd int) bool
	clock      func() time.Time
	log        *slog.Logger

	mu             sync.RWMutex
	unhealthyUntil time.Time
	runErr         chan error
	startOnce      sync.Once
}

// Config содержит конфигурацию для создания нового клиента.
type Config struct {
	APIID       int
	APIHash     string
	PhoneNumber string
	SessionPath string
}

// ClientOption определяет функциональную опцию для конфигурации клиента.
type ClientOption func(*Client)

// WithLogger устанавливает логгер для клиента.
func WithLogger(l *slog.Logger) ClientOption {
	return func(c *Client) {
		if l != nil {
			c.log = l
		}
	}
}

// NewClient создает новый экземпляр Client.
func NewClient(cfg Config, opts ...ClientOption) *Client {
	// Создаем аутентификатор для терминала.
	termAuth := trm.NewTerminal(cfg.PhoneNumber)

	// Настраиваем хранилище сессии.
	sessionStorage := &session.FileStorage{Path: cfg.SessionPath}

	// Создаем и настраиваем базовый клиент gotd.
	tgClient := telegram.NewClient(cfg.APIID, cfg.APIHash, telegram.Options{
		SessionStorage: sessionStorage,
	})

	c := &Client{
		id:         uuid.NewString(),
		tgRunner:   &prodRunner{Client: tgClient},
		authFlow:   auth.NewFlow(termAuth, auth.SendCodeOptions{}),
		isTerminal: func(fd int) bool { return term.IsTerminal(fd) },
		clock:      time.Now,
		log:        slog.Default(),
		runErr:     make(chan error, 1),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// ID возвращает уникальный идентификатор клиента.
func (c *Client) ID() string {
	return c.id
}

// Start запускает фоновый процесс клиента, включая аутентификацию.
// Должен быть вызван один раз перед использованием клиента.
func (c *Client) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		go func() {
			c.log.InfoContext(ctx, "Starting telegram client background runner", "client_id", c.id)
			err := c.tgRunner.Run(ctx, func(runCtx context.Context) error {
				// Проверяем статус аутентификации при запуске.
				if _, err := c.tgRunner.API().UsersGetUsers(runCtx, []tg.InputUserClass{&tg.InputUserSelf{}}); err != nil {
					// Если ошибка - это ожидаемое отсутствие сессии, логируем кратко.
					// Для всех остальных, непредвиденных ошибок, сохраняем полный вывод.
					if strings.Contains(err.Error(), "AUTH_KEY_UNREGISTERED") {
						c.log.WarnContext(runCtx, "Session check failed, attempting interactive auth", "client_id", c.id, "reason", "AUTH_KEY_UNREGISTERED")
					} else {
						c.log.WarnContext(runCtx, "Session check failed, attempting interactive auth", "client_id", c.id, "error", err)
					}
					if !c.isTerminal(int(os.Stdout.Fd())) {
						return fmt.Errorf("session is invalid and cannot perform interactive auth in non-terminal: %w", err)
					}
					if authErr := c.authFlow.Run(runCtx, c.tgRunner.Auth()); authErr != nil {
						return fmt.Errorf("interactive auth failed: %w", authErr)
					}
					c.log.InfoContext(runCtx, "Interactive auth successful, session saved", "client_id", c.id)
				}
				c.log.InfoContext(runCtx, "Telegram client authenticated and ready", "client_id", c.id)

				// Держим соединение активным, пока не завершится контекст.
				<-runCtx.Done()
				return runCtx.Err()
			})

			if err != nil && !errors.Is(err, context.Canceled) {
				c.log.ErrorContext(ctx, "Telegram client background runner exited with error", "client_id", c.id, "error", err)
			} else {
				c.log.InfoContext(ctx, "Telegram client background runner stopped", "client_id", c.id)
			}

			c.runErr <- err
			close(c.runErr)
		}()
	})
}

// Health проверяет работоспособность клиента.
// Если активен FLOOD_WAIT, возвращает ошибку.
// В противном случае выполняет легковесный запрос к API.
// При получении новой ошибки FLOOD_WAIT, обновляет внутреннее состояние.
func (c *Client) Health(ctx context.Context) error {
	if err := c.checkHealthStatus(); err != nil {
		return err
	}

	// Выполняем легковесный запрос для проверки связи.
	err := c.do(ctx, func(ctx context.Context) error {
		_, err := c.tgRunner.API().HelpGetConfig(ctx)
		return err
	})

	// Если даже проверка здоровья вызвала ошибку, возвращаем ее.
	// Метод do уже обработал и установил новый FLOOD_WAIT, если это было необходимо.
	return err
}

// UsersGetUsers выполняет запрос UsersGetUsers, инкапсулируя всю логику.
func (c *Client) UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error) {
	var result []tg.UserClass
	c.log.DebugContext(ctx, "Executing API call: UsersGetUsers")
	err := c.do(ctx, func(ctx context.Context) error {
		res, err := c.tgRunner.API().UsersGetUsers(ctx, request)
		if err == nil {
			result = res
		}
		return err
	})
	// Ошибка FLOOD_WAIT уже логируется в handleError. Логируем остальные для полноты картины.
	if err != nil && !errors.Is(err, ErrFloodWaitActive) {
		c.log.WarnContext(ctx, "API call UsersGetUsers failed", "error", err)
	}
	return result, err
}

// ContactsResolveUsername выполняет запрос ContactsResolveUsername.
func (c *Client) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	var result *tg.ContactsResolvedPeer
	c.log.DebugContext(ctx, "Executing API call: ContactsResolveUsername", "username", req.Username)
	err := c.do(ctx, func(ctx context.Context) error {
		res, err := c.tgRunner.API().ContactsResolveUsername(ctx, req)
		if err == nil {
			result = res
		}
		return err
	})
	if err != nil && !errors.Is(err, ErrFloodWaitActive) {
		c.log.WarnContext(ctx, "API call ContactsResolveUsername failed", "username", req.Username, "error", err)
	}
	return result, err
}

// UsersGetFullUser выполняет запрос UsersGetFullUser.
func (c *Client) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	var result *tg.UsersUserFull
	c.log.DebugContext(ctx, "Executing API call: UsersGetFullUser")
	err := c.do(ctx, func(ctx context.Context) error {
		res, err := c.tgRunner.API().UsersGetFullUser(ctx, inputUser)
		if err == nil {
			result = res
		}
		return err
	})
	if err != nil && !errors.Is(err, ErrFloodWaitActive) {
		c.log.WarnContext(ctx, "API call UsersGetFullUser failed", "error", err)
	}
	return result, err
}

// do — это основной метод, который выполняет всю работу.
// Он проверяет состояние, запускает клиент, обрабатывает аутентификацию и ошибки.
func (c *Client) do(ctx context.Context, f func(ctx context.Context) error) error {
	c.log.DebugContext(ctx, "Executing 'do' method")
	if err := c.checkHealthStatus(); err != nil {
		c.log.WarnContext(ctx, "Client is unhealthy, aborting 'do'", "error", err)
		return err
	}

	// Предполагается, что c.Start() был вызван, и клиент работает в фоновом режиме.
	// Просто выполняем запрошенную операцию.
	opErr := f(ctx)

	if opErr != nil {
		// Обрабатываем специфичные ошибки, такие как FLOOD_WAIT.
		c.handleError(opErr)

		// Также проверяем, не отвалился ли сам клиент.
		select {
		case runErr, ok := <-c.runErr:
			if ok && runErr != nil {
				return fmt.Errorf("клиент telegram не запущен: %w (ошибка операции: %v)", runErr, opErr)
			}
		default:
			// Клиент все еще работает, возвращаем ошибку операции.
		}
	}

	return opErr
}

// checkHealthStatus проверяет, не находится ли клиент в состоянии FLOOD_WAIT.
func (c *Client) checkHealthStatus() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.unhealthyUntil.IsZero() && c.clock().Before(c.unhealthyUntil) {
		err := fmt.Errorf("%w: active until %v", ErrFloodWaitActive, c.unhealthyUntil)
		c.log.Debug("Health check failed: client is in flood wait", "until", c.unhealthyUntil)
		return err
	}

	c.log.Debug("Health check passed: client is not in flood wait")
	return nil
}

// handleError обрабатывает ошибки, ищет FLOOD_WAIT и обновляет состояние клиента.
func (c *Client) handleError(err error) {
	if waitDuration, ok := parseFloodWait(err); ok {
		c.mu.Lock()
		defer c.mu.Unlock()

		c.unhealthyUntil = c.clock().Add(waitDuration)
		c.log.Warn("Client got FLOOD_WAIT, set unhealthy", "wait_duration", waitDuration, "until", c.unhealthyUntil)
	}
}

// parseFloodWait извлекает длительность ожидания из ошибки.
func parseFloodWait(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}

	matches := floodWaitRegex.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return 0, false
	}

	seconds, convErr := strconv.Atoi(matches[1])
	if convErr != nil {
		return 0, false
	}

	return time.Duration(seconds) * time.Second, true
}
