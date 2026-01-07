package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/tg"

	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// ErrParticipantNotResolved - терминальная ошибка, указывающая, что участник не может быть найден.
var ErrParticipantNotResolved = errors.New("participant not resolvable")

// channelRegexp — это скомпилированное регулярное выражение для поиска упоминаний каналов в bio пользователя.
// Оно ищет шаблоны вида @channelname или t.me/channelname.
var channelRegexp = regexp.MustCompile(`(?:@|t\.me/)([a-zA-Z0-9_]{5,})`)

// extractChannelFromBio парсит bio пользователя для поиска потенциального упоминания канала.
// Возвращает имя канала, если оно найдено, в противном случае — пустую строку.
func extractChannelFromBio(bio string) string {
	if bio == "" {
		return ""
	}

	matches := channelRegexp.FindStringSubmatch(bio)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// Config хранит конфигурацию для EnrichmentService.
type Config struct {
	// TotalTimeout — максимальная продолжительность обработки всего списка участников.
	TotalTimeout time.Duration
	// OperationTimeout — таймаут для одного вызова Telegram API.
	OperationTimeout time.Duration
	// PoolSize — количество одновременных воркеров для обогащения.
	PoolSize int
	// ClientRetryPause — продолжительность паузы перед повторной попыткой получить клиент от роутера.
	ClientRetryPause time.Duration
}

// Option — функциональная опция для настройки EnrichmentService.
type Option func(*EnrichmentService)

// WithTotalTimeout устанавливает общий таймаут для процесса обогащения.
func WithTotalTimeout(d time.Duration) Option {
	return func(s *EnrichmentService) {
		s.config.TotalTimeout = d
	}
}

// WithOperationTimeout устанавливает таймаут для одной операции API.
func WithOperationTimeout(d time.Duration) Option {
	return func(s *EnrichmentService) {
		s.config.OperationTimeout = d
	}
}

// WithPoolSize устанавливает количество одновременных воркеров.
func WithPoolSize(n int) Option {
	return func(s *EnrichmentService) {
		if n > 0 {
			s.config.PoolSize = n
		}
	}
}

// WithClientRetryPause устанавливает длительность паузы между повторными попытками получения клиента.
func WithClientRetryPause(d time.Duration) Option {
	return func(s *EnrichmentService) {
		s.config.ClientRetryPause = d
	}
}

// WithLogger устанавливает логгер для сервиса.
func WithLogger(l *slog.Logger) Option {
	return func(s *EnrichmentService) {
		if l != nil {
			s.log = l
		}
	}
}

// EnrichmentService обогащает данные участников, используя Telegram API.
// Сервис не хранит состояние и безопасен для одновременного использования.
type EnrichmentService struct {
	router ports.Router
	config Config
	log    *slog.Logger
}

// NewEnrichmentService создает новый EnrichmentService с использованием функциональных опций.
// Он начинает с конфигурации по умолчанию, которая может быть переопределена предоставленными опциями.
func NewEnrichmentService(r ports.Router, opts ...Option) *EnrichmentService {
	// Конфигурация по умолчанию.
	s := &EnrichmentService{
		router: r,
		config: Config{
			TotalTimeout:     10 * time.Minute,
			OperationTimeout: 5 * time.Second,
			PoolSize:         1,
			ClientRetryPause: 1 * time.Second,
		},
		log: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// enrichResult — вспомогательная структура для передачи результатов от воркеров.
type enrichResult struct {
	user  domain.User
	err   error
	isSet bool // Отличает успешное обогащение от случая, когда пользователь не был найден.
}

// Enrich обрабатывает список "сырых" участников для обогащения их данных.
// Метод принимает функциональные опции для переопределения конфигурации сервиса по умолчанию для этого конкретного вызова.
func (s *EnrichmentService) Enrich(ctx context.Context, participants []domain.RawParticipant) ([]domain.User, error) {
	if len(participants) == 0 {
		return nil, nil
	}

	// Дедупликация списка участников по UserID или Username.
	seen := make(map[string]struct{}, len(participants))
	uniqueParticipants := make([]domain.RawParticipant, 0, len(participants))
	for _, p := range participants {
		var key string
		if p.UserID != "" {
			key = p.UserID
		} else if p.Username != "" {
			key = p.Username
		}

		if key == "" {
			// Участники без ключа не могут быть продублированы, добавляем их как есть.
			uniqueParticipants = append(uniqueParticipants, p)
			continue
		}

		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			uniqueParticipants = append(uniqueParticipants, p)
		}
	}

	if len(uniqueParticipants) < len(participants) {
		s.log.InfoContext(ctx, "Removed duplicate participants", "original_count", len(participants), "unique_count", len(uniqueParticipants))
	}
	participants = uniqueParticipants

	// После дедупликации может не остаться участников для обработки.
	if len(participants) == 0 {
		return nil, nil
	}

	cfg := s.config // Используем конфигурацию, заданную при создании сервиса

	ctx, cancel := context.WithTimeout(ctx, cfg.TotalTimeout)
	defer cancel()

	s.log.InfoContext(ctx, "Starting enrichment process",
		"participants", len(participants),
		"pool_size", cfg.PoolSize,
		"total_timeout", cfg.TotalTimeout,
	)

	tasks := make(chan domain.RawParticipant, len(participants))
	results := make(chan enrichResult, len(participants))
	var wg sync.WaitGroup

	for i := 0; i < cfg.PoolSize; i++ {
		wg.Add(1)
		go s.worker(ctx, &wg, &cfg, tasks, results)
	}

	for _, p := range participants {
		tasks <- p
	}

	enrichedUsersMap := make(map[int64]domain.User, len(participants))
	var unidentifiedUsers []domain.User // Для пользователей с ID = 0.
	var processingErrors []error
	finishedCount := 0

	for finishedCount < len(participants) {
		select {
		case res := <-results:
			if res.err != nil {
				// Это терминальная ошибка (скорее всего, таймаут), задача завершена с ошибкой.
				processingErrors = append(processingErrors, res.err)
			} else if res.isSet {
				// Пользователи с ID=0 не могут быть однозначно идентифицированы,
				// поэтому мы не применяем к ним логику дедупликации и собираем отдельно.
				if res.user.ID == 0 {
					unidentifiedUsers = append(unidentifiedUsers, res.user)
				} else if res.user.Username != "" {
					// Пользователь с юзернеймом имеет приоритет и перезаписывает любую существующую запись.
					enrichedUsersMap[res.user.ID] = res.user
				} else {
					// Пользователя без юзернейма добавляем, только если такого ID еще нет в мапе.
					// Это предотвращает перезапись пользователя с юзернеймом на пользователя без него.
					if _, exists := enrichedUsersMap[res.user.ID]; !exists {
						enrichedUsersMap[res.user.ID] = res.user
					}
				}
			}
			finishedCount++
		case <-ctx.Done():
			// Глобальный таймаут сработал, пока мы ждали результатов.
			enrichedUsers := make([]domain.User, 0, len(enrichedUsersMap)+len(unidentifiedUsers))
			for _, u := range enrichedUsersMap {
				enrichedUsers = append(enrichedUsers, u)
			}
			enrichedUsers = append(enrichedUsers, unidentifiedUsers...)

			err := fmt.Errorf("enrichment process timed out: %w", ctx.Err())
			s.log.WarnContext(ctx, "Enrichment process timed out", "enriched_count", len(enrichedUsers), "error", err)
			// Прекращаем ждать и возвращаем то, что успели собрать.
			return enrichedUsers, err
		}
	}

	// Все задачи получили терминальный статус (успех или ошибка).
	// Теперь можно безопасно закрыть канал задач, чтобы воркеры завершились.
	close(tasks)
	wg.Wait()
	close(results)

	enrichedUsers := make([]domain.User, 0, len(enrichedUsersMap)+len(unidentifiedUsers))
	for _, u := range enrichedUsersMap {
		enrichedUsers = append(enrichedUsers, u)
	}
	enrichedUsers = append(enrichedUsers, unidentifiedUsers...)

	if len(processingErrors) > 0 {
		return enrichedUsers, errors.Join(processingErrors...)
	}

	s.log.InfoContext(ctx, "Enrichment process finished successfully", "enriched_count", len(enrichedUsers))
	return enrichedUsers, nil
}

func (s *EnrichmentService) worker(ctx context.Context, wg *sync.WaitGroup, cfg *Config, tasks chan domain.RawParticipant, results chan<- enrichResult) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			// Глобальный контекст завершен, выходим.
			return
		case p, ok := <-tasks:
			if !ok {
				// Канал задач закрыт, больше работы нет.
				return
			}

			user, err := s.enrichParticipant(ctx, cfg, p)
			if err != nil {
				// Проверяем, является ли ошибка терминальной (например, пользователь не найден).
				if errors.Is(err, ErrParticipantNotResolved) {
					s.log.DebugContext(ctx, "Participant could not be resolved, skipping", "participant", p, "error", err)
					// Это не ошибка всего процесса, а просто неудача для одного участника.
					// Отправляем пустой результат, чтобы счетчик в Enrich уменьшился.
					results <- enrichResult{isSet: false}
				} else if ctx.Err() != nil {
					// Глобальный контекст отменен, это терминальная ошибка для воркера.
					s.log.WarnContext(ctx, "Failed to enrich participant due to context cancellation", "participant", p, "error", err)
					results <- enrichResult{err: err}
				} else {
					// Любая другая ошибка считается временной, перемещаем задачу в конец очереди.
					s.log.WarnContext(ctx, "Re-queueing participant due to transient error", "participant", p, "error", err)
					tasks <- p
				}
				continue
			}

			// Успех, отправляем результат.
			results <- enrichResult{user: user, isSet: true}
		}
	}
}

func (s *EnrichmentService) enrichParticipant(ctx context.Context, cfg *Config, p domain.RawParticipant) (domain.User, error) {
	if p.Username == "" && p.UserID == "" {
		s.log.DebugContext(ctx, "Participant has no username or ID, skipping enrichment", "participant_name", p.Name)
		return domain.User{ID: 0, Name: p.Name}, nil
	}

	var tgUser *tg.User
	var err error

	if p.Username != "" {
		s.log.DebugContext(ctx, "Resolving participant by username", "username", p.Username)
		tgUser, err = s.resolveByUsername(ctx, cfg, p.Username)
	} else {
		// Если у пользователя не указан Username, его не нужно обогащать.
		// Вместо вызова API возвращаем пользователя с имеющимися данными.
		s.log.DebugContext(ctx, "Participant has no username, creating user from existing data", "user_id", p.UserID)
		id, parseErr := strconv.ParseInt(strings.TrimPrefix(p.UserID, "user"), 10, 64)
		if parseErr != nil {
			return domain.User{}, fmt.Errorf("invalid user ID format %q: %w", p.UserID, parseErr)
		}
		return domain.User{ID: id, Name: p.Name}, nil

		// tgUser, err = s.resolveByUserID(ctx, cfg, p.UserID)
	}

	if err != nil {
		s.log.WarnContext(ctx, "Failed to resolve participant", "participant", p, "error", err)
		return domain.User{}, fmt.Errorf("failed to resolve participant %v: %w", p, err)
	}

	if tgUser == nil {
		s.log.DebugContext(ctx, "Participant could not be resolved to a TG user, returning as is", "participant", p)
		// Не удалось найти, возвращаем как есть.
		// Возвращаем пользователя как есть, но пытаемся установить корректный ID.
		var userID int64
		if p.UserID != "" {
			id, err := strconv.ParseInt(strings.TrimPrefix(p.UserID, "user"), 10, 64)
			if err != nil {
				s.log.WarnContext(ctx, "Could not parse UserID for unresolved participant; falling back to ID 0", "user_id", p.UserID, "error", err)
			} else {
				userID = id
			}
		}
		return domain.User{ID: userID, Name: p.Name}, nil
	}

	s.log.DebugContext(ctx, "Participant resolved successfully", "participant", p, "tg_user_id", tgUser.ID)

	bio, bioErr := s.getFullUserInfo(ctx, cfg, tgUser)
	if bioErr != nil {
		// Ошибку получения bio считаем некритичной, но возвращаем ее, чтобы можно было перепоставить в очередь.
		s.log.WarnContext(ctx, "Failed to get full user info, will retry", "tg_user_id", tgUser.ID, "error", bioErr)
		return domain.User{}, fmt.Errorf("failed to get full user info for %d: %w", tgUser.ID, bioErr)
	}

	channel := extractChannelFromBio(bio)

	return domain.User{
		ID:       tgUser.ID,
		Name:     strings.TrimSpace(fmt.Sprintf("%s %s", tgUser.FirstName, tgUser.LastName)),
		Username: tgUser.Username,
		Bio:      bio,
		Channel:  channel,
	}, nil
}

func (s *EnrichmentService) resolveByUsername(ctx context.Context, cfg *Config, username string) (*tg.User, error) {
	cleanUsername := strings.TrimPrefix(username, "@")
	s.log.DebugContext(ctx, "Executing ContactsResolveUsername", "username", cleanUsername)
	logArgs := []any{"operation", "ContactsResolveUsername", "username", cleanUsername}
	res, err := s.executeOperation(ctx, cfg, logArgs, func(ctx context.Context, cl ports.TelegramClient) (any, error) {
		return cl.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: cleanUsername})
	})
	if err != nil {
		s.log.WarnContext(ctx, "resolveByUsername executeOperation failed", "username", username, "error", err)
		return nil, err
	}
	if res == nil {
		err := errors.New("resolve by username returned no result")
		s.log.ErrorContext(ctx, "Unexpected nil result from API", "username", username, "error", err)
		return nil, err
	}

	resolved, ok := res.(*tg.ContactsResolvedPeer)
	if !ok || resolved == nil || len(resolved.Users) == 0 {
		err := fmt.Errorf("%w: username not found or resolved to non-user", ErrParticipantNotResolved)
		s.log.DebugContext(ctx, "Could not resolve username", "username", username, "error", err)
		return nil, err
	}
	if user, ok := resolved.Users[0].(*tg.User); ok {
		return user, nil
	}

	err = errors.New("resolved peer is not a user")
	s.log.WarnContext(ctx, "Unexpected peer type from resolution", "username", username, "peer_type", fmt.Sprintf("%T", resolved.Users[0]))
	return nil, err
}

func (s *EnrichmentService) resolveByUserID(ctx context.Context, cfg *Config, userIDStr string) (*tg.User, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(userIDStr, "user"), 10, 64)
	if err != nil {
		err = fmt.Errorf("invalid user ID format: %s: %w", userIDStr, err)
		s.log.WarnContext(ctx, "Failed to parse user ID", "user_id_str", userIDStr, "error", err)
		return nil, err
	}

	s.log.DebugContext(ctx, "Executing UsersGetUsers", "user_id", id)
	logArgs := []any{"operation", "UsersGetUsers", "user_id", id}
	res, err := s.executeOperation(ctx, cfg, logArgs, func(ctx context.Context, cl ports.TelegramClient) (any, error) {
		return cl.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUser{UserID: id}})
	})
	if err != nil {
		s.log.WarnContext(ctx, "resolveByUserID executeOperation failed", "user_id", id, "error", err)
		return nil, err
	}
	if res == nil {
		err := errors.New("resolve by user id returned no result")
		s.log.ErrorContext(ctx, "Unexpected nil result from API", "user_id", id, "error", err)
		return nil, err
	}

	users, ok := res.([]tg.UserClass)
	if !ok || len(users) == 0 {
		err := fmt.Errorf("%w: user not found by ID", ErrParticipantNotResolved)
		s.log.DebugContext(ctx, "Could not resolve user by ID", "user_id", id, "error", err)
		return nil, err
	}
	if user, ok := users[0].(*tg.User); ok {
		return user, nil
	}

	err = errors.New("could not cast to user type")
	s.log.WarnContext(ctx, "Unexpected user type from resolution by ID", "user_id", id, "user_type", fmt.Sprintf("%T", users[0]))
	return nil, err
}

func (s *EnrichmentService) getFullUserInfo(ctx context.Context, cfg *Config, user *tg.User) (string, error) {
	accessHash, ok := user.GetAccessHash()
	if !ok {
		s.log.WarnContext(ctx, "User object is missing access hash", "user_id", user.ID)
		return "", errors.New("no access hash for user")
	}

	s.log.DebugContext(ctx, "Executing UsersGetFullUser", "user_id", user.ID)
	logArgs := []any{"operation", "UsersGetFullUser", "user_id", user.ID}
	res, err := s.executeOperation(ctx, cfg, logArgs, func(ctx context.Context, cl ports.TelegramClient) (any, error) {
		return cl.UsersGetFullUser(ctx, &tg.InputUser{UserID: user.ID, AccessHash: accessHash})
	})
	if err != nil {
		s.log.WarnContext(ctx, "getFullUserInfo executeOperation failed", "user_id", user.ID, "error", err)
		return "", err
	}
	if res == nil {
		err := errors.New("get full user info returned no result")
		s.log.ErrorContext(ctx, "Unexpected nil result from API", "user_id", user.ID, "error", err)
		return "", err
	}

	userFull, ok := res.(*tg.UsersUserFull)
	if !ok {
		err := errors.New("failed to cast to UserFull")
		s.log.ErrorContext(ctx, "Unexpected type from getFullUserInfo", "user_id", user.ID, "type", fmt.Sprintf("%T", res))
		return "", err
	}
	return userFull.FullUser.About, nil
}

func (s *EnrichmentService) executeOperation(ctx context.Context, cfg *Config, logArgs []any, fn func(ctx context.Context, cl ports.TelegramClient) (any, error)) (any, error) {
	// Внутренний цикл отвечает за получение клиента. Он "бесконечный", но ограничен родительским контекстом.
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s.log.DebugContext(ctx, "Attempting to get a client from the router")
		apiClient, err := s.router.GetClient(ctx)
		if err != nil {
			s.log.WarnContext(ctx, "Failed to get a client from the router, will retry", "error", err, "pause", cfg.ClientRetryPause)
			select {
			case <-time.After(cfg.ClientRetryPause):
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("не удалось получить клиент, так как контекст был отменен: %w", ctx.Err())
			}
		}

		s.log.DebugContext(ctx, "Obtained client successfully", "client_id", apiClient.ID())

		opCtx, opCancel := context.WithTimeout(ctx, cfg.OperationTimeout)
		res, opErr := fn(opCtx, apiClient)
		opCancel()

		// Добавляем client_id к уже существующим аргументам лога.
		finalLogArgs := append(logArgs, "client_id", apiClient.ID())

		if opErr == nil {
			s.log.DebugContext(ctx, "API operation executed successfully", finalLogArgs...)
			return res, nil // Успех
		}

		// Ошибка от операции возвращается вызывающей стороне, которая решит, что делать дальше (например, перепоставить задачу).
		finalLogArgs = append(finalLogArgs, "error", opErr)
		s.log.WarnContext(ctx, "API operation failed", finalLogArgs...)
		return nil, fmt.Errorf("операция API завершилась с ошибкой: %w", opErr)
	}
}
