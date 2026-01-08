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
	mockID       string
	mu           sync.RWMutex
	recoveryTime time.Time
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

func (m *mockClient) GetRecoveryTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recoveryTime
}

func (m *mockClient) setRecoveryTime(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoveryTime = t
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
		scheduledRecovery:   make(map[string]struct{}),
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

	// Вызываем метод API. Обертка должна перехватить ошибку и инициировать обработку.
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
	r.setClientUnhealthy(client1)
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
		// client c2 is at index 1
		r.setClientUnhealthy(clients[1])
		time.Sleep(5 * time.Millisecond)
		r.setClientHealthy("c2")
	}()

	wg.Wait()
	// Если тест запускается с флагом -race и не падает, значит, гонок нет.
}

func TestRouter_ProactiveRecoveryAfterFloodWait(t *testing.T) {
	// 1. Настройка
	client1 := newMockClient("proactive-client", true)
	clients := []ports.TelegramClient{client1}

	// Создаем роутер с очень большим интервалом health check, чтобы он нам не мешал.
	r := newTestRouter(t, clients, 5*time.Minute)
	defer r.Stop()

	// 2. Симуляция ошибки FLOOD_WAIT
	floodWaitDuration := 200 * time.Millisecond
	// Ошибка, которую вернет клиент. Важно, чтобы она содержала текст "FLOOD_WAIT".
	floodWaitErr := fmt.Errorf("rpc error: code = Canceled desc = FLOOD_WAIT (%d)", int(floodWaitDuration.Seconds()))
	// Время, когда клиент "оживет".
	recoveryTime := time.Now().Add(floodWaitDuration)

	// Настраиваем мок-клиент
	client1.setReturnError(floodWaitErr)
	client1.setRecoveryTime(recoveryTime)

	// Получаем обернутый клиент
	wrappedClient, err := r.GetClient(context.Background())
	require.NoError(t, err)

	// 3. Вызов API, который приведет к ошибке
	_, apiErr := wrappedClient.UsersGetUsers(context.Background(), nil)
	require.Error(t, apiErr)

	// 4. Проверка немедленного перемещения в unhealthy
	// Даем горутине handleClientError немного времени на выполнение
	time.Sleep(50 * time.Millisecond)

	r.mu.RLock()
	require.Len(t, r.healthy, 0, "Client should be moved to unhealthy pool immediately")
	require.Len(t, r.unhealthy, 1, "Client should be in unhealthy pool")
	_, scheduled := r.scheduledRecovery[client1.ID()]
	require.True(t, scheduled, "Proactive recovery should be scheduled")
	r.mu.RUnlock()

	// 5. Симулируем, что клиент "ожил" и готов к работе
	client1.setHealthy(true)
	client1.setReturnError(nil) // Больше не возвращает ошибок

	// 6. Проверка восстановления ТОЧНО по таймеру
	// Ждем дольше, чем длительность flood wait
	time.Sleep(floodWaitDuration) // Ждем оставшиеся ~150ms + запас

	// Проверяем, что клиент вернулся в healthy пул
	require.Eventually(t, func() bool {
		r.mu.RLock()
		defer r.mu.RUnlock()
		_, isHealthy := r.healthy[client1.ID()]
		return isHealthy
	}, 100*time.Millisecond, 10*time.Millisecond, "Client should be back in healthy pool after recovery time")

	// Финальная проверка состояния
	r.mu.RLock()
	defer r.mu.RUnlock()
	require.Len(t, r.healthy, 1)
	require.Len(t, r.unhealthy, 0)
	_, scheduled = r.scheduledRecovery[client1.ID()]
	require.False(t, scheduled, "Recovery schedule should be cleared after execution")
}
