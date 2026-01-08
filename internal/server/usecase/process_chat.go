package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"telegram-chat-parser/internal/adapters/source"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ports"
)

// ProcessChatUseCase инкапсулирует бизнес-логику для обработки файла экспорта чата.
type ProcessChatUseCase struct {
	cfg        *config.Config
	parser     ports.Parser
	extractor  ports.ExtractionService
	enricher   ports.EnrichmentService
	cacheStore *cache.CacheStore
}

// NewProcessChatUseCase создает новый экземпляр ProcessChatUseCase.
func NewProcessChatUseCase(
	cfg *config.Config,
	parser ports.Parser,
	extractor ports.ExtractionService,
	enricher ports.EnrichmentService,
	cacheStore *cache.CacheStore,
) *ProcessChatUseCase {
	return &ProcessChatUseCase{
		cfg:        cfg,
		parser:     parser,
		extractor:  extractor,
		enricher:   enricher,
		cacheStore: cacheStore,
	}
}

// ProcessChat обрабатывает файл экспорта чата.
// Он извлекает данные, разбирает их, извлекает участников и обогащает их через Telegram API.
func (uc *ProcessChatUseCase) ProcessChat(ctx context.Context, filePath string) ([]domain.User, error) {
	// Вычисление хеша файла
	fileHash, err := cache.CalculateFileHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("не удалось вычислить хеш файла: %w", err)
	}

	// Проверка, есть ли результат уже в кеше
	if cachedItem, found := uc.cacheStore.Get(fileHash); found {
		slog.Info("Попадание в кеш для файла", "hash", fileHash)
		return cachedItem.Data, nil
	}

	// 1. Извлечение данных из файла
	slog.Info("Извлечение данных из файла...", "path", filePath)
	ds := source.NewCliSource(filePath)
	data, err := ds.Fetch()
	if err != nil {
		return nil, fmt.Errorf("не удалось извлечь данные: %w", err)
	}

	// 2. Разбор данных
	slog.Info("Разбор данных...")
	chat, err := uc.parser.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать данные: %w", err)
	}
	// Логирование разобранной структуры чата
	slog.Info("Разобранная структура чата", "message_count", len(chat.Messages), "chat_name", chat.Name, "chat_type", chat.Type)

	// 3. Извлечение сырых участников
	slog.Info("Извлечение сырых участников...")
	rawParticipants, err := uc.extractor.ExtractRawParticipants(chat)
	if err != nil {
		return nil, fmt.Errorf("не удалось извлечь сырых участников: %w", err)
	}
	// Логирование извлеченных сырых участников
	slog.Info("Извлеченные сырые участники", "count", len(rawParticipants), "participants", rawParticipants)

	// 4. Обогащение данных через Telegram API
	slog.Info("Обогащение данных через Telegram API... Это может занять некоторое время и потребовать аутентификации.")
	finalUsers, err := uc.enricher.Enrich(ctx, rawParticipants)
	if err != nil {
		return nil, fmt.Errorf("не удалось обогатить данные: %w", err)
	}

	// 5. Сохранение результата в кеше
	ttl := uc.cfg.Processing.CacheTTL
	uc.cacheStore.Put(fileHash, finalUsers, ttl)
	slog.Info("Результат кеширован для файла", "hash", fileHash, "ttl", ttl.String())

	slog.Info("Обработка успешно завершена", "user_count", len(finalUsers))
	slog.Info("Окончательный список обогащенных пользователей", "users", finalUsers)
	return finalUsers, nil
}
