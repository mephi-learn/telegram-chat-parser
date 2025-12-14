package integration

import (
	"context"
	"testing"

	"github.com/gotd/td/tg"

	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

type telegramAPIClientInterface interface {
	UsersGetUsers(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error)
	ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
}

// MockTelegramClient - это мок-реализация telegramClientInterface
type MockTelegramClient struct {
	runFunc func(ctx context.Context, f func(ctx context.Context) error) error
	apiFunc func() telegramAPIClientInterface
}

func (m *MockTelegramClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if m.runFunc != nil {
		return m.runFunc(ctx, f)
	}
	// Реализация по умолчанию, которая просто вызывает функцию
	return f(ctx)
}

func (m *MockTelegramClient) API() telegramAPIClientInterface {
	if m.apiFunc != nil {
		return m.apiFunc()
	}
	// Возвращаем мок API по умолчанию
	return &MockTelegramAPIClient{}
}

// MockTelegramAPIClient - это мок-реализация telegramAPIClientInterface
type MockTelegramAPIClient struct {
	usersGetUsersFunc           func(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error)
	contactsResolveUsernameFunc func(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	usersGetFullUserFunc        func(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
}

func (m *MockTelegramAPIClient) UsersGetUsers(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error) {
	if m.usersGetUsersFunc != nil {
		return m.usersGetUsersFunc(ctx, inputUsers)
	}
	// По умолчанию возвращаем несколько мок-пользователей
	users := make([]tg.UserClass, len(inputUsers))
	for i := range inputUsers {
		users[i] = &tg.User{
			ID:        12345,
			FirstName: "Test",
			LastName:  "User",
			Username:  "testuser",
		}
	}
	return users, nil
}

func (m *MockTelegramAPIClient) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	if m.contactsResolveUsernameFunc != nil {
		return m.contactsResolveUsernameFunc(ctx, req)
	}
	// По умолчанию возвращаем мок разрешенного пира
	return &tg.ContactsResolvedPeer{
		Users: []tg.UserClass{
			&tg.User{
				ID:        12345,
				FirstName: "Test",
				LastName:  "User",
				Username:  req.Username,
			},
		},
	}, nil
}

func (m *MockTelegramAPIClient) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	if m.usersGetFullUserFunc != nil {
		return m.usersGetFullUserFunc(ctx, inputUser)
	}
	// По умолчанию возвращаем мок полного пользователя
	return &tg.UsersUserFull{
		FullUser: tg.UserFull{
			About: "Test bio",
		},
	}, nil
}

// MockEnrichmentService реализует интерфейс ports.EnrichmentService для тестирования
type MockEnrichmentService struct {
	enrichFunc func(context.Context, []domain.RawParticipant) ([]domain.User, error)
}

func (m *MockEnrichmentService) Enrich(ctx context.Context, participants []domain.RawParticipant) ([]domain.User, error) {
	if m.enrichFunc != nil {
		return m.enrichFunc(ctx, participants)
	}
	// Реализация по умолчанию, которая преобразует сырых участников в пользователей
	users := make([]domain.User, len(participants))
	for i, p := range participants {
		users[i] = domain.User{
			ID:       int64(i + 1),
			Name:     p.Name,
			Username: p.Username,
			Bio:      "Test bio",
		}
	}
	return users, nil
}

func TestEnrichmentServiceWithMock(t *testing.T) {
	// Этот тест демонстрирует, что мы можем создать сервис, который реализует
	// интерфейс EnrichmentService
	var _ ports.EnrichmentService = &MockEnrichmentService{}

	// Тестируем мок-реализацию
	service := &MockEnrichmentService{}

	participants := []domain.RawParticipant{
		{
			UserID:   "user123",
			Name:     "Test User",
			Username: "@testuser",
		},
	}

	users, err := service.Enrich(context.Background(), participants)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибки от мок-обогащения, получено: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Ожидался 1 пользователь, получено %d", len(users))
	}

	if users[0].Name != "Test User" {
		t.Errorf("Ожидалось имя 'Test User', получено '%s'", users[0].Name)
	}
}

func TestMockAPIClients(t *testing.T) {
	// Тестируем реализации мок-клиента API
	api := &MockTelegramAPIClient{}

	// Тестируем UsersGetUsers
	ctx := context.Background()
	inputUsers := []tg.InputUserClass{&tg.InputUser{UserID: 123, AccessHash: 456}}
	users, err := api.UsersGetUsers(ctx, inputUsers)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибки от мок UsersGetUsers, получено: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Ожидался 1 пользователь, получено %d", len(users))
	}

	// Тестируем ContactsResolveUsername
	req := &tg.ContactsResolveUsernameRequest{Username: "testuser"}
	resolved, err := api.ContactsResolveUsername(ctx, req)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибки от мок ContactsResolveUsername, получено: %v", err)
	}

	if len(resolved.Users) != 1 {
		t.Errorf("Ожидался 1 разрешенный пользователь, получено %d", len(resolved.Users))
	}

	// Тестируем UsersGetFullUser
	inputUser := &tg.InputUser{UserID: 123, AccessHash: 456}
	fullUser, err := api.UsersGetFullUser(ctx, inputUser)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибки от мок UsersGetFullUser, получено: %v", err)
	}

	if fullUser.FullUser.About != "Test bio" {
		t.Errorf("Ожидалось био 'Test bio', получено '%s'", fullUser.FullUser.About)
	}
}
