package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"

	trm "telegram-chat-parser/internal/pkg/term"
)

// Интерфейсы, определяющие точные методы, которые мы используем из клиента telegram.
// Это позволяет легко создавать моки в тестах.
type (
	APIClient interface {
		UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error)
	}

	AuthClient interface {
		IfNecessary(ctx context.Context, flow auth.Flow) error
	}

	// tgClient - это интерфейс для частей клиента telegram, с которыми мы взаимодействуем.
	tgClient interface {
		Run(ctx context.Context, f func(ctx context.Context) error) error
		API() APIClient
		Auth() AuthClient
	}
)

// clientAdapter адаптирует *telegram.Client к интерфейсу tgClient.
type clientAdapter struct {
	*telegram.Client
}

func (a *clientAdapter) API() APIClient {
	return a.Client.API()
}

func (a *clientAdapter) Auth() AuthClient {
	return a.Client.Auth()
}

// AuthManager управляет аутентификацией клиента Telegram.
type AuthManager struct {
	apiID       int
	apiHash     string
	sessionPath string

	// Зависимости для тестирования
	isTerminal    func(fd int) bool
	newClient     func(appID int, appHash string, opts telegram.Options) tgClient
	newAuthFlow   func(a auth.UserAuthenticator, opts auth.SendCodeOptions) auth.Flow
	authenticator auth.UserAuthenticator
}

// NewAuthManager создает новый AuthManager с производственными зависимостями.
func NewAuthManager(apiID int, apiHash, phoneNumber, sessionPath string) *AuthManager {
	return &AuthManager{
		apiID:       apiID,
		apiHash:     apiHash,
		sessionPath: sessionPath,
		isTerminal:  term.IsTerminal,
		newClient: func(appID int, appHash string, opts telegram.Options) tgClient {
			return &clientAdapter{Client: telegram.NewClient(appID, appHash, opts)}
		},
		newAuthFlow:   auth.NewFlow,
		authenticator: trm.NewTerminal(phoneNumber),
	}
}

// RunAuthenticatedClient запускает функцию с аутентифицированным клиентом Telegram.
func (m *AuthManager) RunAuthenticatedClient(ctx context.Context, f func(ctx context.Context, client *telegram.Client) error) error {
	client := m.newClient(m.apiID, m.apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: m.sessionPath},
	})

	return client.Run(ctx, func(ctx context.Context) error {
		_, err := client.API().UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})

		if err != nil {
			slog.Warn("Проверка сессии не удалась, попытка интерактивной аутентификации", "error", err)
			if !m.isTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("сессия telegram недействительна или отсутствует, и невозможно запустить интерактивную аутентификацию в неинтерактивном терминале: %w", err)
			}

			flow := m.newAuthFlow(m.authenticator, auth.SendCodeOptions{})

			if authErr := client.Auth().IfNecessary(ctx, flow); authErr != nil {
				slog.Error("Интерактивная аутентификация не удалась", "error", authErr)
				return fmt.Errorf("интерактивная аутентификация не удалась: %w", authErr)
			}
			slog.Info("Интерактивная аутентификация прошла успешно. Сессия сохранена.")
		} else {
			slog.Info("Сессия Telegram действительна. Пользователь аутентифицирован.")
		}

		if f != nil {
			if realClient, ok := client.(*clientAdapter); ok {
				return f(ctx, realClient.Client)
			}
			// В тестах клиент является моком, поэтому мы не можем получить реальный клиент.
			// В этом случае обратный вызов не выполняется с реальным клиентом.
		}
		return nil
	})
}
