package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"

	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// TelegramClientInterface объединяет все методы Telegram API, используемые в приложении
type TelegramClientInterface interface {
	// Run запускает клиент телеграма с предоставленным контекстом
	Run(ctx context.Context, f func(ctx context.Context) error) error
	// API возвращает доступ к клиентскому API
	API() TelegramAPIClientInterface
}

// TelegramAPIClientInterface для мокирования API
type TelegramAPIClientInterface interface {
	// UsersGetUsers извлекает информацию о пользователях по предоставленным InputUser
	UsersGetUsers(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error)
	// ContactsResolveUsername преобразует имя пользователя в информацию о пользователе
	ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error)
	// UsersGetFullUser извлекает полную информацию о пользователе
	UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error)
}

// TelegramAPIClient оборачивает tg.Client для удовлетворения интерфейсу
type TelegramAPIClient struct {
	client *tg.Client
}

// NewTelegramAPIClient создает новый экземпляр TelegramAPIClient
func NewTelegramAPIClient(client *tg.Client) TelegramAPIClientInterface {
	return &TelegramAPIClient{client: client}
}

// UsersGetUsers реализует интерфейс TelegramAPIClientInterface
func (t *TelegramAPIClient) UsersGetUsers(ctx context.Context, inputUsers []tg.InputUserClass) ([]tg.UserClass, error) {
	return t.client.UsersGetUsers(ctx, inputUsers)
}

// ContactsResolveUsername реализует интерфейс TelegramAPIClientInterface
func (t *TelegramAPIClient) ContactsResolveUsername(ctx context.Context, req *tg.ContactsResolveUsernameRequest) (*tg.ContactsResolvedPeer, error) {
	return t.client.ContactsResolveUsername(ctx, req)
}

// UsersGetFullUser реализует интерфейс TelegramAPIClientInterface
func (t *TelegramAPIClient) UsersGetFullUser(ctx context.Context, inputUser tg.InputUserClass) (*tg.UsersUserFull, error) {
	return t.client.UsersGetFullUser(ctx, inputUser)
}

// EnrichmentServiceImpl реализует интерфейс EnrichmentService.
// Он обогащает участников чата с помощью Telegram API.
type EnrichmentServiceImpl struct {
	// telegramClient клиент Telegram, предоставленный извне
	telegramClient TelegramClientInterface
}

// NewEnrichmentService создает новый экземпляр EnrichmentServiceImpl.
// DEPRECATED: Use NewEnrichmentServiceWithClient instead.
func NewEnrichmentService(apiID int, apiHash, phoneNumber string) ports.EnrichmentService {
	// Создаем клиент как раньше для совместимости.
	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: "tg.session"},
	})
	return &EnrichmentServiceImpl{
		telegramClient: &defaultTelegramClientWrapper{client: client},
	}
}

// NewEnrichmentServiceWithClient создает экземпляр с готовым Telegram клиентом.
func NewEnrichmentServiceWithClient(client *telegram.Client) ports.EnrichmentService {
	return &EnrichmentServiceImpl{
		telegramClient: &defaultTelegramClientWrapper{client: client},
	}
}

// NewEnrichmentServiceWithClientFactory создает экземпляр с указанной фабрикой клиента
func NewEnrichmentServiceWithClientFactory(apiID int, apiHash, phoneNumber string, factory func() TelegramClientInterface) ports.EnrichmentService {
	// Используем фабрику для получения клиента
	client := factory()
	return &EnrichmentServiceImpl{
		telegramClient: client,
	}
}

// defaultTelegramClientWrapper - внутренняя обертка для telegram.Client
type defaultTelegramClientWrapper struct {
	client *telegram.Client
}

// Run реализует интерфейс TelegramClientInterface
func (dtcw *defaultTelegramClientWrapper) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return dtcw.client.Run(ctx, f)
}

// API реализует интерфейс TelegramClientInterface
func (dtcw *defaultTelegramClientWrapper) API() TelegramAPIClientInterface {
	return NewTelegramAPIClient(dtcw.client.API())
}

// ProcessParticipants обрабатывает участников с использованием переданного API клиента
// и возвращает список доменных объектов пользователей с дополнительной информацией.
func (s *EnrichmentServiceImpl) ProcessParticipants(ctx context.Context, api TelegramAPIClientInterface, participants []domain.RawParticipant) ([]domain.User, error) {
	slog.Info("Starting to process participants", "count", len(participants))

	accessHashes := make(map[int64]int64)
	resolvedUsers := make(map[int64]*tg.User)
	participantsByID := make(map[int64]domain.RawParticipant)
	participantsByUsername := make(map[string]domain.RawParticipant)

	// --- Фаза 1: Разделение участников на группы ---
	for _, p := range participants {
		if p.Username != "" {
			username := strings.TrimPrefix(p.Username, "@")
			if _, exists := participantsByUsername[username]; !exists {
				participantsByUsername[username] = p
			}
		} else if p.UserID != "" {
			strID := strings.TrimPrefix(p.UserID, "user")
			id, err := strconv.ParseInt(strID, 10, 64)
			if err == nil {
				if _, exists := participantsByID[id]; !exists {
					participantsByID[id] = p
				}
			}
		}
	}

	// --- Фаза 2: Обогащение по username для сбора AccessHashes ---
	for username, p := range participantsByUsername {
		slog.Info("Attempting to resolve username", "username", username)
		resolved, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: username})
		if err != nil {
			slog.Warn("Could not resolve username", "username", p.Username, "error", err)
			continue
		}
		if len(resolved.Users) > 0 {
			if u, ok := resolved.Users[0].(*tg.User); ok {
				slog.Info("Successfully resolved username to user", "username", username, "id", u.ID)
				resolvedUsers[u.ID] = u
				if accessHash, hasHash := u.GetAccessHash(); hasHash {
					accessHashes[u.ID] = accessHash
				}
			}
		}
	}

	// --- Фаза 3: Пакетное обогащение по ID ---
	inputUsers := make([]tg.InputUserClass, 0, len(participantsByID))
	for id := range participantsByID {
		if _, exists := resolvedUsers[id]; exists {
			continue // Уже обработан
		}
		accessHash, _ := accessHashes[id]
		inputUsers = append(inputUsers, &tg.InputUser{UserID: id, AccessHash: accessHash})
	}

	if len(inputUsers) > 0 {
		slog.Info("Attempting to get users by ID in a batch", "count", len(inputUsers))
		users, err := api.UsersGetUsers(ctx, inputUsers)
		if err != nil {
			slog.Error("Could not get users by ID in a batch", "error", err)
		} else {
			for _, userClass := range users {
				if u, ok := userClass.(*tg.User); ok {
					slog.Info("Successfully got user by ID", "id", u.ID)
					resolvedUsers[u.ID] = u
				}
			}
		}
	}

	// --- Фаза 4: Финальная сборка ---
	finalUsers := make(map[int64]domain.User)

	// Сначала добавляем обогащенных пользователей
	for id, user := range resolvedUsers {
		var bio string
		if accessHash, exists := accessHashes[id]; exists {
			if userFull, err := api.UsersGetFullUser(ctx, &tg.InputUser{UserID: id, AccessHash: accessHash}); err == nil {
				bio = userFull.FullUser.About
			}
		}
		finalUsers[id] = domain.User{
			ID:       user.ID,
			Name:     strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName)),
			Username: user.Username,
			Bio:      bio,
		}
	}

	// --- Фаза 5: Добавление пользователей, которых не удалось обогатить ---
	for id, p := range participantsByID {
		if _, exists := finalUsers[id]; !exists {
			slog.Info("Adding non-enriched user to the final list", "id", id)
			finalUsers[id] = domain.User{
				ID:   id,
				Name: p.Name, // Имя из JSON
			}
		}
	}

	slog.Info("Finished processing participants", "final_user_count", len(finalUsers))
	result := make([]domain.User, 0, len(finalUsers))
	for _, user := range finalUsers {
		result = append(result, user)
	}
	return result, nil
}

// Enrich подключается к Telegram, обогащает данные об участниках и возвращает уникальный список.
func (s *EnrichmentServiceImpl) Enrich(ctx context.Context, participants []domain.RawParticipant) ([]domain.User, error) {
	slog.Info("Enrich: Starting enrichment process", "participant_count", len(participants))
	// Используем готовый клиент Telegram.
	client := s.telegramClient

	slog.Info("Enrich: About to call client.Run")
	// Запускаем клиент и выполняем в нем основную логику.
	var result []domain.User
	err := client.Run(ctx, func(ctx context.Context) error {
		slog.Info("Enrich: Inside client.Run, about to call ProcessParticipants")
		// Получаем доступ к "сырому" API.
		api := client.API()

		// Вызываем отдельную функцию для обработки участников
		processedResult, err := s.ProcessParticipants(ctx, api, participants)
		if err != nil {
			slog.Error("Enrich: failed to process participants", "error", err)
			return fmt.Errorf("failed to process participants: %w", err)
		}
		result = processedResult
		slog.Info("Enrich: ProcessParticipants completed successfully inside client.Run")

		return nil
	})
	slog.Info("Enrich: Finished client.Run call")

	if err != nil {
		slog.Error("Enrich: telegram client error after client.Run", "error", err)
		return nil, fmt.Errorf("telegram client error: %w", err)
	}

	slog.Info("Enrich: Returning result", "result_count", len(result))
	return result, nil
}
