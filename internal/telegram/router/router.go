package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ports"
	"telegram-chat-parser/internal/telegram"

	"github.com/gotd/td/tg"
)

var (
	// ErrNoHealthyClients возвращается, когда в пуле нет доступных для работы клиентов.
	ErrNoHealthyClients = errors.New("no healthy clients available")
	// ErrClientNotFound возвращается, когда клиент с указанным ID не найден.
	ErrClientNotFound = errors.New("client not found")
)

// Option определяет функциональную опцию для конфигурации роутера.
type Option func(*Router)

// WithServerConfigs — опция для передачи конфигураций серверов.
// Клиенты будут созданы внутри роутера.
func WithServerConfigs(serverConfigs []config.TelegramAPIServer) Option {
	return func(r *Router) {
		clients := make([]ports.TelegramClient, 0, len(serverConfigs))
		for _, srvCfg := range serverConfigs {
			// Используем опцию WithLogger, чтобы передать логгер роутера в каждый клиент.
			// Логгер роутера к этому моменту уже должен быть инициализирован.
			client := telegram.NewClient(telegram.Config{
				APIID:       srvCfg.APIID,
				APIHash:     srvCfg.APIHash,
				PhoneNumber: srvCfg.PhoneNumber,
				SessionPath: srvCfg.SessionFile,
			}, telegram.WithLogger(r.log.With("client_phone", srvCfg.PhoneNumber)))
			clients = append(clients, client)
		}
		r.clients = clients
	}
}

// WithHealthCheckInterval — опция для установки интервала проверки работоспособности.
func WithHealthCheckInterval(d time.Duration) Option {
	return func(r *Router) {
		if d > 0 {
			r.healthCheckInterval = d
		}
	}
}

// WithStrategy — опция для установки стратегии выбора клиента.
func WithStrategy(s ports.Strategy) Option {
	return func(r *Router) {
		if s != nil {
			r.strategy = s
		}
	}
}

// WithLogger — опция для установки логгера.
func WithLogger(l *slog.Logger) Option {
	return func(r *Router) {
		if l != nil {
			r.log = l
		}
	}
}

// Router управляет пулом клиентов Telegram, их состоянием и выбором.
type Router struct {
	mu                sync.RWMutex
	healthy           map[string]ports.TelegramClient
	unhealthy         map[string]ports.TelegramClient
	scheduledRecovery map[string]struct{} // Отслеживает клиентов, для которых уже запланировано восстановление.
	strategy          ports.Strategy
	log               *slog.Logger

	clients             []ports.TelegramClient // Начальный список клиентов, созданный из конфигов
	healthCheckInterval time.Duration
	ticker              *time.Ticker
	done                chan struct{}
	wg                  sync.WaitGroup
}

// NewRouter создает и запускает новый роутер с использованием функциональных опций.
func NewRouter(ctx context.Context, opts ...Option) (*Router, error) {
	// Конфигурация по умолчанию
	r := &Router{
		healthy:             make(map[string]ports.TelegramClient),
		unhealthy:           make(map[string]ports.TelegramClient),
		scheduledRecovery:   make(map[string]struct{}),
		strategy:            NewRoundRobinStrategy(),
		healthCheckInterval: 30 * time.Second, // Значение по умолчанию
		done:                make(chan struct{}),
		log:                 slog.Default().With("component", "router"),
	}

	// Применяем опции
	for _, opt := range opts {
		opt(r)
	}

	if len(r.clients) == 0 {
		return nil, errors.New("no server configs provided to router")
	}

	// Запускаем клиенты и инициализируем пул здоровых клиентов
	for _, c := range r.clients {
		c.Start(ctx)
		r.healthy[c.ID()] = c
	}
	r.clients = nil // Больше не нужен

	// Запускаем фоновую проверку
	r.ticker = time.NewTicker(r.healthCheckInterval)
	r.wg.Add(1)
	go r.healthCheckLoop()

	return r, nil
}

// GetClient возвращает работоспособного клиента согласно текущей стратегии.
// Возвращаемый клиент обернут в clientWrapper для обработки ошибок "на лету".
func (r *Router) GetClient(ctx context.Context) (ports.TelegramClient, error) {
	r.mu.RLock()
	// Преобразуем map в срез для стратегии.
	// Это компромисс ради удобства использования стратегий.
	// В реальных условиях с высокой нагрузкой можно было бы оптимизировать.
	clients := make([]ports.TelegramClient, 0, len(r.healthy))
	for _, c := range r.healthy {
		clients = append(clients, c)
	}
	strategy := r.strategy
	r.mu.RUnlock()

	client, err := strategy.Next(clients)
	if err != nil {
		r.log.DebugContext(ctx, "Strategy failed to get next client", "error", err)
		return nil, fmt.Errorf("strategy failed to get next client: %w", err)
	}

	r.log.DebugContext(ctx, "Client selected by strategy", "client_id", client.ID())

	// Оборачиваем клиент в декоратор для перехвата ошибок.
	return &clientWrapper{
		TelegramClient: client,
		router:         r,
	}, nil
}

// SetStrategy позволяет безопасно сменить стратегию выбора клиента на лету.
func (r *Router) SetStrategy(s ports.Strategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategy = s
	r.log.Info("router strategy updated")
}

// Stop останавливает фоновую проверку работоспособности клиентов.
func (r *Router) Stop() {
	r.log.Info("stopping router...")
	r.ticker.Stop()
	close(r.done)
	r.wg.Wait()
	r.log.Info("router stopped")
}

// NextRecoveryTime возвращает ближайшее время, когда один из неработоспособных клиентов
// может снова стать доступным. Если неработоспособных клиентов нет или для них
// не установлено время восстановления, возвращает нулевое значение time.Time.
func (r *Router) NextRecoveryTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var nextRecovery time.Time
	for _, c := range r.unhealthy {
		recoveryTime := c.GetRecoveryTime()
		if !recoveryTime.IsZero() && (nextRecovery.IsZero() || recoveryTime.Before(nextRecovery)) {
			nextRecovery = recoveryTime
		}
	}
	return nextRecovery
}

// healthCheckLoop - это фоновая горутина, которая периодически
// проверяет неработоспособных клиентов и пытается вернуть их в пул здоровых.
func (r *Router) healthCheckLoop() {
	defer r.wg.Done()
	for {
		select {
		case t := <-r.ticker.C:
			r.log.Debug("Health check ticker fired", "time", t)
			r.checkUnhealthyClients()
		case <-r.done:
			r.log.Info("Health check loop is stopping.")
			return
		}
	}
}

// checkUnhealthyClients итерируется по нездоровым клиентам и проверяет их.
func (r *Router) checkUnhealthyClients() {
	r.mu.RLock()
	// Создаем копию списка ID для проверки, чтобы не блокировать роутер надолго.
	idsToCheck := make([]string, 0, len(r.unhealthy))
	for id := range r.unhealthy {
		idsToCheck = append(idsToCheck, id)
	}
	r.mu.RUnlock()

	if len(idsToCheck) == 0 {
		return
	}

	r.log.Debug("starting periodic health check for unhealthy clients", "count", len(idsToCheck))

	for _, id := range idsToCheck {
		r.mu.RLock()
		client, ok := r.unhealthy[id]
		r.mu.RUnlock()

		if !ok {
			continue // Клиент мог быть перемещен или удален.
		}

		if err := client.Health(context.Background()); err == nil {
			r.log.Info("client recovered, moving back to healthy pool", "client_id", id)
			r.setClientHealthy(id)
		} else {
			r.log.Debug("Client remains unhealthy", "client_id", id, "reason", err)
		}
	}
}

// handleClientError обрабатывает ошибку, полученную от клиента.
// Он перемещает клиента в пул нездоровых и, если это возможно,
// планирует проактивное восстановление.
func (r *Router) handleClientError(client ports.TelegramClient, err error) {
	clientID := client.ID()
	r.log.Warn("Client returned an error, processing...", "client_id", clientID, "error", err)

	// Перемещаем клиента в нездоровый пул.
	r.setClientUnhealthy(client)

	// Проверяем, нужно ли планировать проактивное восстановление.
	recoveryTime := client.GetRecoveryTime()
	now := time.Now() // Для тестов здесь можно использовать мок времени.

	if recoveryTime.IsZero() || now.After(recoveryTime) {
		return // Нет времени восстановления или оно уже в прошлом.
	}

	r.mu.Lock()
	if _, exists := r.scheduledRecovery[clientID]; exists {
		r.mu.Unlock()
		r.log.Debug("Proactive recovery already scheduled for client", "client_id", clientID)
		return
	}

	// Помечаем, что восстановление запланировано.
	r.scheduledRecovery[clientID] = struct{}{}
	delay := time.Until(recoveryTime)
	r.mu.Unlock()

	r.log.Info("Scheduling proactive recovery for client", "client_id", clientID, "delay", delay)

	// Запускаем проверку по истечении таймаута.
	time.AfterFunc(delay, func() {
		r.checkAndRecoverClient(clientID)
	})
}

// checkAndRecoverClient выполняет проверку здоровья для одного клиента и восстанавливает его при успехе.
// Эта функция вызывается как по таймеру проактивного восстановления, так и периодическим health check'ом.
func (r *Router) checkAndRecoverClient(clientID string) {
	r.mu.Lock()
	// Сразу удаляем флаг запланированного восстановления.
	delete(r.scheduledRecovery, clientID)
	client, ok := r.unhealthy[clientID]
	r.mu.Unlock()

	if !ok {
		r.log.Debug("Client to recover not found in unhealthy pool (already recovered?)", "client_id", clientID)
		return
	}

	r.log.Debug("Running health check for client", "client_id", clientID)
	if err := client.Health(context.Background()); err == nil {
		r.log.Info("Client recovered, moving back to healthy pool", "client_id", clientID)
		r.setClientHealthy(clientID)
	} else {
		r.log.Warn("Recovery check failed, client remains unhealthy", "client_id", clientID, "reason", err)
	}
}

// setClientUnhealthy перемещает клиента из пула здоровых в пул нездоровых.
func (r *Router) setClientUnhealthy(client ports.TelegramClient) {
	id := client.ID()
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.healthy[id]; !ok {
		return // Клиент уже не в пуле здоровых.
	}

	delete(r.healthy, id)
	r.unhealthy[id] = client

	r.log.Warn("Client moved to unhealthy pool", "client_id", id, "healthy_count", len(r.healthy), "unhealthy_count", len(r.unhealthy))
}

// setClientHealthy перемещает клиента из пула нездоровых в пул здоровых.
func (r *Router) setClientHealthy(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	client, ok := r.unhealthy[id]
	if !ok {
		return // Клиент уже был перемещен.
	}

	delete(r.unhealthy, id)
	r.healthy[id] = client

	r.log.Info("Client moved back to healthy pool", "client_id", id, "healthy_count", len(r.healthy), "unhealthy_count", len(r.unhealthy))
}

// --- clientWrapper ---

// clientWrapper - это декоратор для Client, который перехватывает ошибки
// вызовов API и инициирует проверку работоспособности клиента.
// Это позволяет инкапсулировать логику обработки сбоев от вызывающего кода.
type clientWrapper struct {
	ports.TelegramClient
	router *Router
}

// Переопределяем все методы интерфейса TelegramAPIRepositoryInterface,
// добавляя к ним обработку ошибок.

func (w *clientWrapper) UsersGetUsers(ctx context.Context, request []tg.InputUserClass) ([]tg.UserClass, error) {
	w.router.log.DebugContext(ctx, "Calling UsersGetUsers via wrapper", "client_id", w.ID())
	res, err := w.TelegramClient.UsersGetUsers(ctx, request)
	if err != nil {
		// Сообщаем роутеру об ошибке, чтобы он принял решение.
		// Запускаем в горутине, чтобы не блокировать вызывающий код.
		go w.router.handleClientError(w.TelegramClient, err)
	}
	return res, err
}

func (w *clientWrapper) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	w.router.log.DebugContext(ctx, "Calling ContactsResolveUsername via wrapper", "client_id", w.ID(), "username", req.Username)
	res, err := w.TelegramClient.ContactsResolveUsername(ctx, req)
	if err != nil {
		go w.router.handleClientError(w.TelegramClient, err)
	}
	return res, err
}

func (w *clientWrapper) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	w.router.log.DebugContext(ctx, "Calling UsersGetFullUser via wrapper", "client_id", w.ID())
	res, err := w.TelegramClient.UsersGetFullUser(ctx, inputUser)
	if err != nil {
		go w.router.handleClientError(w.TelegramClient, err)
	}
	return res, err
}
