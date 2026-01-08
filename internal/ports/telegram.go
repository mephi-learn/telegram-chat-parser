package ports

import (
	"context"
	"time"

	"github.com/gotd/td/tg"
)

// TelegramClient определяет публичный интерфейс для клиента Telegram.
type TelegramClient interface {
	UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error)
	ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
	Health(ctx context.Context) error
	ID() string
	Start(ctx context.Context)
	GetRecoveryTime() time.Time
}

// Router определяет интерфейс для роутера клиентов Telegram.
type Router interface {
	GetClient(ctx context.Context) (TelegramClient, error)
	Stop()
	NextRecoveryTime() time.Time
}

// Strategy определяет интерфейс для стратегии выбора клиента.
type Strategy interface {
	Next(clients []TelegramClient) (TelegramClient, error)
}
