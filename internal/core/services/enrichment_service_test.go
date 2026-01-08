package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"telegram-chat-parser/internal/domain"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"telegram-chat-parser/internal/ports"
)

// mockClient — это мок для интерфейса ports.TelegramClient.
type mockClient struct {
	mock.Mock
}

func (m *mockClient) UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error) {
	args := m.Called(ctx, request)
	if res := args.Get(0); res != nil {
		return res.([]tg.UserClass), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockClient) ContactsResolveUsername(ctx context.Context, request *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	args := m.Called(ctx, request)
	if res := args.Get(0); res != nil {
		return res.(*tg.ContactsResolvedPeer), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockClient) UsersGetFullUser(ctx context.Context, request tg.InputUserClass) (*tg.UsersUserFull, error) {
	args := m.Called(ctx, request)
	if res := args.Get(0); res != nil {
		return res.(*tg.UsersUserFull), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockClient) Health(ctx context.Context) error { return nil }
func (m *mockClient) ID() string                       { return "mock-client" }
func (m *mockClient) Start(ctx context.Context)        {}
func (m *mockClient) GetRecoveryTime() time.Time       { return time.Time{} }

// mockRouter — это мок для интерфейса ports.Router.
type mockRouter struct {
	mock.Mock
}

func (m *mockRouter) GetClient(ctx context.Context) (ports.TelegramClient, error) {
	args := m.Called(ctx)
	if cli := args.Get(0); cli != nil {
		return cli.(ports.TelegramClient), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockRouter) Stop() {}

func (m *mockRouter) NextRecoveryTime() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func TestEnrichmentService_Enrich_Success(t *testing.T) {
	router := new(mockRouter)
	client := new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router,
		1, 10*time.Millisecond, 1*time.Second,
		WithLogger(logger),
	)

	participant := domain.RawParticipant{Username: "testuser"}
	tgUser := &tg.User{ID: 1, Username: "testuser", FirstName: "Test", LastName: "User"}
	tgUser.SetAccessHash(123) // Корректно устанавливаем access hash
	resolvedPeer := &tg.ContactsResolvedPeer{Users: []tg.UserClass{tgUser}}
	fullUser := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio"}}

	router.On("GetClient", mock.Anything).Return(client, nil).Twice()
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "testuser"}).Return(resolvedPeer, nil).Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: tgUser.ID, AccessHash: 123}).Return(fullUser, nil).Once()

	users, err := service.Enrich(context.Background(), []domain.RawParticipant{participant})

	assert.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "Test User", users[0].Name)
	assert.Equal(t, "Bio", users[0].Bio)
	assert.Empty(t, users[0].Channel, "Channel should be empty for a bio without a link")
	router.AssertExpectations(t)
	client.AssertExpectations(t)
}

// TestEnrichmentService_Enrich_RequeueOnFailure проверяет, что задача перепостанавливается в очередь при временной ошибке.
func TestEnrichmentService_Enrich_RequeueOnFailure(t *testing.T) {
	router := new(mockRouter)
	client1, client2 := new(mockClient), new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router, 1, 10*time.Millisecond, 1*time.Second, WithLogger(logger))

	participant := domain.RawParticipant{Username: "testuser"}
	tgUser := &tg.User{ID: 1, Username: "testuser", FirstName: "Test"}
	tgUser.SetAccessHash(123)
	resolvedPeer := &tg.ContactsResolvedPeer{Users: []tg.UserClass{tgUser}}
	fullUser := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio"}}
	apiError := errors.New("API_ERROR")

	// Первая попытка (для resolve) вернет ошибку
	router.On("GetClient", mock.Anything).Return(client1, nil).Once()
	client1.On("ContactsResolveUsername", mock.Anything, mock.Anything).Return(nil, apiError).Once()

	// Вторая попытка (снова для resolve) будет успешной
	router.On("GetClient", mock.Anything).Return(client2, nil).Once()
	client2.On("ContactsResolveUsername", mock.Anything, mock.Anything).Return(resolvedPeer, nil).Once()

	// Третья попытка (для get full info) будет успешной
	router.On("GetClient", mock.Anything).Return(client2, nil).Once()
	client2.On("UsersGetFullUser", mock.Anything, mock.Anything).Return(fullUser, nil).Once()

	users, err := service.Enrich(context.Background(), []domain.RawParticipant{participant})

	assert.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "Bio", users[0].Bio)
	router.AssertExpectations(t)
	client1.AssertExpectations(t)
	client2.AssertExpectations(t)
}

func TestEnrichmentService_Enrich_RetryOnGetClientError(t *testing.T) {
	router := new(mockRouter)
	client := new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router, 1, 10*time.Millisecond, 1*time.Second, WithLogger(logger))

	participant := domain.RawParticipant{Username: "testuser"}
	tgUser := &tg.User{ID: 1, Username: "testuser", FirstName: "Test"}
	tgUser.SetAccessHash(123)
	resolvedPeer := &tg.ContactsResolvedPeer{Users: []tg.UserClass{tgUser}}
	fullUser := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio"}}

	// Первый GetClient неудачен, второй успешен
	router.On("GetClient", mock.Anything).Return(nil, errors.New("NO_CLIENTS")).Once()
	router.On("NextRecoveryTime").Return(time.Time{}).Once()          // Добавляем ожидание вызова
	router.On("GetClient", mock.Anything).Return(client, nil).Twice() // Для resolve и get full user

	client.On("ContactsResolveUsername", mock.Anything, mock.Anything).Return(resolvedPeer, nil).Once()
	client.On("UsersGetFullUser", mock.Anything, mock.Anything).Return(fullUser, nil).Once()

	users, err := service.Enrich(context.Background(), []domain.RawParticipant{participant})

	assert.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "Bio", users[0].Bio)
	router.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestEnrichmentService_Enrich_TotalTimeout(t *testing.T) {
	router := new(mockRouter)
	client := new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router,
		2, 100*time.Millisecond, 1*time.Second,
		WithLogger(logger),
	)

	p1 := domain.RawParticipant{Username: "user1"} // Будет обработан
	p2 := domain.RawParticipant{Username: "user2"} // Вызовет таймаут

	user1 := &tg.User{ID: 1, Username: "user1", FirstName: "U1"}
	user1.SetAccessHash(1)
	resolved1 := &tg.ContactsResolvedPeer{Users: []tg.UserClass{user1}}
	fullUser1 := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio1"}}

	// Настройка для user1 (быстрый)
	router.On("GetClient", mock.Anything).Return(client, nil) // Разрешаем несколько вызовов
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user1"}).Return(resolved1, nil).Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: 1, AccessHash: 1}).Return(fullUser1, nil).Once()

	// Настройка для user2 (медленный, вызовет таймаут)
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user2"}).
		Return(nil, context.DeadlineExceeded).
		After(150 * time.Millisecond) // Вернули задержку

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	users, err := service.Enrich(ctx, []domain.RawParticipant{p1, p2})

	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Ожидалась ошибка истечения времени ожидания контекста")
	assert.Len(t, users, 1, "Должен вернуть частично обработанных пользователей")
	assert.Equal(t, int64(1), users[0].ID)
}

func TestEnrichmentService_Enrich_ParallelProcessing(t *testing.T) {
	router := new(mockRouter)
	client := new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router, 2, 10*time.Millisecond, 1*time.Second, WithLogger(logger))

	participants := []domain.RawParticipant{
		{Username: "user1"},
		{Username: "user2"},
	}

	user1 := &tg.User{ID: 1, Username: "user1"}
	user1.SetAccessHash(1)
	user2 := &tg.User{ID: 2, Username: "user2"}
	user2.SetAccessHash(2)

	var wg sync.WaitGroup
	wg.Add(2)

	// Пользователь 1
	router.On("GetClient", mock.Anything).Return(client, nil)
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user1"}).
		Return(&tg.ContactsResolvedPeer{Users: []tg.UserClass{user1}}, nil).
		Run(func(args mock.Arguments) { wg.Done() }).
		Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: 1, AccessHash: 1}).
		Return(&tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio1"}}, nil).
		Once()

	// Пользователь 2
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user2"}).
		Return(&tg.ContactsResolvedPeer{Users: []tg.UserClass{user2}}, nil).
		Run(func(args mock.Arguments) { wg.Done() }).
		Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: 2, AccessHash: 2}).
		Return(&tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio2"}}, nil).
		Once()

	users, err := service.Enrich(context.Background(), participants)

	// Ждем, пока обе горутины будут запущены
	waitChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		// continue
	case <-time.After(1 * time.Second):
		t.Fatal("Тест превысил время ожидания, возможно, параллельное выполнение работает некорректно")
	}

	assert.NoError(t, err)
	assert.Len(t, users, 2)
}

func TestEnrichmentService_Enrich_NoUsernameOrID(t *testing.T) {
	router := new(mockRouter)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router, 1, 10*time.Millisecond, 1*time.Second, WithLogger(logger))

	participant := domain.RawParticipant{Name: "Nameless User"}
	users, err := service.Enrich(context.Background(), []domain.RawParticipant{participant})

	assert.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "Nameless User", users[0].Name)
	assert.Equal(t, int64(0), users[0].ID)
	router.AssertNotCalled(t, "GetClient")
}

func TestEnrichmentService_Enrich_Deduplication(t *testing.T) {
	router := new(mockRouter)
	client := new(mockClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewEnrichmentService(router, 2, 10*time.Millisecond, 1*time.Second, WithLogger(logger))

	// --- Определяем участников и ожидаемые вызовы API ---

	// Участник 1: будет продублирован (с юзернеймом и без)
	p1_with_username := domain.RawParticipant{UserID: "user123", Username: "user_one"}
	p1_without_username := domain.RawParticipant{UserID: "user123", Name: "Old Name"}

	// Участник 2: уникальный
	p2_unique := domain.RawParticipant{Username: "user_two"}

	// Участник 3: без ID, но с именем. Должен быть сохранен.
	p3_no_id := domain.RawParticipant{Name: "Just a Name"}

	// --- Настройка моков для API ---

	// Мок для user_one
	tgUser1 := &tg.User{ID: 123, Username: "user_one", FirstName: "User", LastName: "One"}
	tgUser1.SetAccessHash(111)
	resolvedPeer1 := &tg.ContactsResolvedPeer{Users: []tg.UserClass{tgUser1}}
	fullUser1 := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio One"}}

	// Мок для user_two
	tgUser2 := &tg.User{ID: 456, Username: "user_two", FirstName: "User", LastName: "Two"}
	tgUser2.SetAccessHash(222)
	resolvedPeer2 := &tg.ContactsResolvedPeer{Users: []tg.UserClass{tgUser2}}
	fullUser2 := &tg.UsersUserFull{FullUser: tg.UserFull{About: "Bio Two"}}

	// Ожидаем, что GetClient будет вызван для каждого обогащаемого участника (user_one, user_two)
	router.On("GetClient", mock.Anything).Return(client, nil)

	// Ожидаем вызовы для user_one
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user_one"}).Return(resolvedPeer1, nil).Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: 123, AccessHash: 111}).Return(fullUser1, nil).Once()

	// Ожидаем вызовы для user_two
	client.On("ContactsResolveUsername", mock.Anything, &tg.ContactsResolveUsernameRequest{Username: "user_two"}).Return(resolvedPeer2, nil).Once()
	client.On("UsersGetFullUser", mock.Anything, &tg.InputUser{UserID: 456, AccessHash: 222}).Return(fullUser2, nil).Once()

	// --- Запускаем тест ---

	participants := []domain.RawParticipant{
		p1_with_username,
		p2_unique,
		p1_without_username, // Дубликат
		p3_no_id,
	}

	users, err := service.Enrich(context.Background(), participants)

	// --- Проверяем результат ---
	assert.NoError(t, err)
	assert.Len(t, users, 3, "Ожидалось 3 уникальных пользователя в результате")

	// Проверяем, что в итоговом списке есть правильные пользователи
	foundUser1 := false
	foundUser2 := false
	foundUser3 := false

	for _, u := range users {
		if u.ID == 123 {
			foundUser1 = true
			assert.Equal(t, "user_one", u.Username, "Для ID 123 должен остаться пользователь с юзернеймом")
			assert.Equal(t, "User One", u.Name)
			assert.Equal(t, "Bio One", u.Bio)
		}
		if u.ID == 456 {
			foundUser2 = true
			assert.Equal(t, "user_two", u.Username)
		}
		if u.ID == 0 && u.Name == "Just a Name" {
			foundUser3 = true
		}
	}

	assert.True(t, foundUser1, "Пользователь с ID 123 не найден")
	assert.True(t, foundUser2, "Пользователь с ID 456 не найден")
	assert.True(t, foundUser3, "Пользователь без ID не найден")

	router.AssertExpectations(t)
	client.AssertExpectations(t)
}

func TestExtractChannelFromBio(t *testing.T) {
	testCases := []struct {
		name        string
		bio         string
		wantChannel string
	}{
		{
			name:        "Bio with @channelname",
			bio:         "Check out my cool channel @my_awesome_channel for updates!",
			wantChannel: "my_awesome_channel",
		},
		{
			name:        "Bio with t.me/ link",
			bio:         "Follow me on t.me/another_channel_123",
			wantChannel: "another_channel_123",
		},
		{
			name:        "Bio with t.me/ link at the beginning",
			bio:         "t.me/start_channel and some other text",
			wantChannel: "start_channel",
		},
		{
			name:        "Bio with @ at the beginning",
			bio:         "@just_a_channel_name",
			wantChannel: "just_a_channel_name",
		},
		{
			name:        "Bio without channel",
			bio:         "Just a regular bio with no links.",
			wantChannel: "",
		},
		{
			name:        "Empty bio",
			bio:         "",
			wantChannel: "",
		},
		{
			name:        "Bio with short username",
			bio:         "My channel is @short",
			wantChannel: "short",
		},
		{
			name:        "Bio with multiple mentions, first one is taken",
			bio:         "Follow @channel1 and also @channel2",
			wantChannel: "channel1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractChannelFromBio(tc.bio)
			assert.Equal(t, tc.wantChannel, got)
		})
	}
}
