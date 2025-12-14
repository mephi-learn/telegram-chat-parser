package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"

	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ports"
)

// mockClient - это мок-реализация интерфейса ports.TelegramClient для использования в тестах.
type mockClient struct {
	mockID string
	mu     sync.RWMutex
	// healthErr симулирует состояние здоровья клиента.
	healthErr error
	// returnErr симулирует ошибку от API.
	returnErr error
	// Cчетчики вызовов для верификации в тестах.
	usersGetUsersCount           atomic.Int32
	contactsResolveUsernameCount atomic.Int32
	usersGetFullUserCount        atomic.Int32
}

func newMockClient(id string, isHealthy bool) *mockClient {
	c := &mockClient{mockID: id}
	if !isHealthy {
		c.healthErr = errors.New("client is not healthy")
	}
	return c
}

func (m *mockClient) ID() string {
	return m.mockID
}

func (m *mockClient) Start(ctx context.Context) {}

func (m *mockClient) Health(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthErr
}

func (m *mockClient) setHealthy(isHealthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isHealthy {
		m.healthErr = nil
	} else {
		m.healthErr = errors.New("client is not healthy")
	}
}

func (m *mockClient) setReturnError(err error) {
	m.returnErr = err
}

func (m *mockClient) UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error) {
	m.usersGetUsersCount.Add(1)
	return nil, m.returnErr
}

func (m *mockClient) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	m.contactsResolveUsernameCount.Add(1)
	return nil, m.returnErr
}

func (m *mockClient) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	m.usersGetFullUserCount.Add(1)
	return nil, m.returnErr
}

func newTestRouter(t *testing.T, clients []ports.TelegramClient, interval time.Duration) *Router {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := &Router{
		healthy:             make(map[string]ports.TelegramClient),
		unhealthy:           make(map[string]ports.TelegramClient),
		strategy:            NewRoundRobinStrategy(),
		healthCheckInterval: interval,
		done:                make(chan struct{}),
		log:                 logger,
	}
	for _, c := range clients {
		r.healthy[c.ID()] = c
	}

	r.ticker = time.NewTicker(r.healthCheckInterval)
	r.wg.Add(1)
	go r.healthCheckLoop()

	return r
}

// --- Тесты для Router ---

func TestRouter_NewRouter(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// Используем опции, как и в реальном коде
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := []config.TelegramAPIServer{{APIID: 1, APIHash: "a", SessionFile: "test.session"}}
		r, err := NewRouter(context.Background(), WithServerConfigs(cfg), WithHealthCheckInterval(100*time.Millisecond), WithLogger(logger))
		require.NoError(t, err)
		require.NotNil(t, r)
		defer r.Stop()

		require.Len(t, r.healthy, 1)
		require.Len(t, r.unhealthy, 0)
	})

	t.Run("no_clients_error", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		_, err := NewRouter(context.Background(), WithServerConfigs([]config.TelegramAPIServer{}), WithLogger(logger))
		require.Error(t, err)
	})
}

func TestRouter_GetClient(t *testing.T) {
	clients := []ports.TelegramClient{
		newMockClient("client-1", true),
		newMockClient("client-2", true),
	}
	r := newTestRouter(t, clients, time.Minute)
	defer r.Stop()

	c, err := r.GetClient(context.Background())
	require.NoError(t, err)
	require.NotNil(t, c)

	// Проверяем, что вернулся wrapper.
	wrapper, ok := c.(*clientWrapper)
	require.True(t, ok, "GetClient should return a clientWrapper")
	require.Contains(t, []string{"client-1", "client-2"}, wrapper.TelegramClient.ID())
}

func TestRouter_ClientFailsAndMovesToUnhealthy(t *testing.T) {
	mockErr := errors.New("telegram API error")
	client1 := newMockClient("client-1", true)
	clients := []ports.TelegramClient{client1}

	r := newTestRouter(t, clients, time.Minute)
	defer r.Stop()

	// Сначала клиент здоров.
	require.Len(t, r.healthy, 1)
	require.Len(t, r.unhealthy, 0)
	require.NoError(t, client1.Health(context.Background()))

	// Получаем клиент и симулируем ошибку.
	wrappedClient, err := r.GetClient(context.Background())
	require.NoError(t, err)

	client1.setReturnError(mockErr)
	client1.setHealthy(false) // Симулируем, что проверка здоровья после ошибки провалилась.

	// Вызываем метод API. Обертка должна перехватить ошибку и инициировать проверку.
	_, apiErr := wrappedClient.UsersGetUsers(context.Background(), nil)
	require.ErrorIs(t, apiErr, mockErr)

	// Даем время горутине в wrapper'е выполниться.
	time.Sleep(50 * time.Millisecond)

	// Проверяем, что клиент переместился в нездоровые.
	r.mu.RLock()
	defer r.mu.RUnlock()
	require.Len(t, r.healthy, 0)
	require.Len(t, r.unhealthy, 1)
	require.Equal(t, client1, r.unhealthy["client-1"])
	require.Error(t, client1.Health(context.Background()))
}

func TestRouter_ClientRecoversOnHealthCheck(t *testing.T) {
	client1 := newMockClient("client-1", false) // Начинаем с нездорового клиента.
	client1.setHealthy(false)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := &Router{
		healthy:   make(map[string]ports.TelegramClient),
		unhealthy: map[string]ports.TelegramClient{"client-1": client1},
		log:       logger,
		// Остальные поля не важны для этого теста.
	}

	// Симулируем, что клиент восстановился.
	client1.setHealthy(true)

	// Запускаем проверку вручную.
	r.checkUnhealthyClients()

	// Проверяем, что клиент вернулся в пул здоровых.
	r.mu.RLock()
	defer r.mu.RUnlock()
	require.Len(t, r.healthy, 1)
	require.Len(t, r.unhealthy, 0)
	require.Equal(t, client1, r.healthy["client-1"])
}

func TestRouter_AutomaticRecovery(t *testing.T) {
	client1 := newMockClient("client-1", true)
	clients := []ports.TelegramClient{client1}

	// Короткий интервал для быстрого теста.
	r := newTestRouter(t, clients, 50*time.Millisecond)
	defer r.Stop()

	// Перемещаем клиента в нездоровые вручную.
	r.setClientUnhealthy("client-1")
	require.Len(t, r.healthy, 0)
	require.Len(t, r.unhealthy, 1)

	// Симулируем, что клиент "ожил".
	client1.setHealthy(true)

	// Ждем, пока сработает тикер и проверка.
	time.Sleep(150 * time.Millisecond)

	// Проверяем, что клиент автоматически восстановился.
	r.mu.RLock()
	defer r.mu.RUnlock()
	require.Len(t, r.healthy, 1)
	require.Len(t, r.unhealthy, 0)
}

func TestRouter_Stop(t *testing.T) {
	clients := []ports.TelegramClient{newMockClient("client-1", true)}
	r := newTestRouter(t, clients, 10*time.Millisecond)

	// Проверяем, что горутина запущена (простой способ - проверить, что done не закрыт).
	select {
	case <-r.done:
		t.Fatal("done channel should not be closed yet")
	default:
	}

	r.Stop()

	// Проверяем, что горутина завершилась.
	select {
	case <-r.done:
		// Канал закрыт, все хорошо.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("router did not stop in time")
	}

	// Убедимся, что wg обнулился.
	// Это можно сделать, попытавшись вызвать Stop() еще раз или просто подождав.
	// В данном случае, успешный выход из r.Stop() уже означает, что wg.Wait() завершился.
}

func TestRouter_RaceCondition(t *testing.T) {
	clients := []ports.TelegramClient{
		newMockClient("c1", true),
		newMockClient("c2", true),
		newMockClient("c3", true),
	}
	r := newTestRouter(t, clients, 10*time.Millisecond)
	defer r.Stop()

	var wg sync.WaitGroup
	n := 100 // Количество параллельных операций.

	// Запускаем горутины, которые будут параллельно получать клиентов.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.GetClient(context.Background())
			// Мы не можем гарантировать здесь отсутствие ошибки,
			// так как клиенты могут перемещаться. Главное - отсутствие гонки.
			if err != nil && !errors.Is(err, ErrNoHealthyClients) {
				fmt.Printf("unexpected error: %v", err)
			}
		}()
	}

	// Запускаем горутину, которая будет менять стратегию.
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.SetStrategy(NewRoundRobinStrategy())
	}()

	// Запускаем горутину, которая будет симулировать сбои клиентов.
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.setClientUnhealthy("c2")
		time.Sleep(5 * time.Millisecond)
		r.setClientHealthy("c2")
	}()

	wg.Wait()
	// Если тест запускается с флагом -race и не падает, значит, гонок нет.
}
