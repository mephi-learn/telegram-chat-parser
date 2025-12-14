package services

import (
	"context"
	"github.com/gotd/td/tg"
)

// MockTelegramAPIClient - мок-реализация TelegramAPIClientInterface для тестирования
type MockTelegramAPIClient struct {
	UsersGetUsersFunc           func(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error)
	ContactsResolveUsernameFunc func(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	UsersGetFullUserFunc        func(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
}

// UsersGetUsers реализует интерфейс TelegramAPIClientInterface
func (m *MockTelegramAPIClient) UsersGetUsers(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error) {
	if m.UsersGetUsersFunc != nil {
		return m.UsersGetUsersFunc(ctx, inputUsers)
	}
	return nil, nil
}

// ContactsResolveUsername реализует интерфейс TelegramAPIClientInterface
func (m *MockTelegramAPIClient) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	if m.ContactsResolveUsernameFunc != nil {
		return m.ContactsResolveUsernameFunc(ctx, req)
	}
	return nil, nil
}

// UsersGetFullUser реализует интерфейс TelegramAPIClientInterface
func (m *MockTelegramAPIClient) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	if m.UsersGetFullUserFunc != nil {
		return m.UsersGetFullUserFunc(ctx, inputUser)
	}
	return nil, nil
}
