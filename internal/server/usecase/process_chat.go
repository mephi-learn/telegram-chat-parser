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

// ProcessChat обрабатывает несколько файлов экспорта чата.
// Он извлекает, разбирает, объединяет участников и затем обогащает их данные.
func (uc *ProcessChatUseCase) ProcessChat(ctx context.Context, filePaths []string) ([]domain.User, error) {
	var allRawParticipants []domain.RawParticipant
	var fileHashes []string

	for _, filePath := range filePaths {
		fileHash, err := cache.CalculateFileHash(filePath)
		if err != nil {
			return nil, fmt.Errorf("не удалось вычислить хеш файла %s: %w", filePath, err)
		}
		fileHashes = append(fileHashes, fileHash)
	}

	// Создание единого хеша для набора файлов
	combinedHash := cache.CalculateHashFromString(fmt.Sprintf("%v", fileHashes))

	// Проверка кеша по единому хешу
	if cachedItem, found := uc.cacheStore.Get(combinedHash); found {
		slog.Info("Попадание в кеш для набора файлов", "hash", combinedHash)
		return cachedItem.Data, nil
	}

	for _, filePath := range filePaths {
		slog.Info("Обработка файла", "path", filePath)

		ds := source.NewCliSource(filePath)
		data, err := ds.Fetch()
		if err != nil {
			return nil, fmt.Errorf("не удалось извлечь данные из %s: %w", filePath, err)
		}

		chat, err := uc.parser.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("не удалось разобрать данные из %s: %w", filePath, err)
		}
		slog.Info("Разобран чат", "path", filePath, "message_count", len(chat.Messages))

		rawParticipants, err := uc.extractor.ExtractRawParticipants(chat)
		if err != nil {
			return nil, fmt.Errorf("не удалось извлечь участников из %s: %w", filePath, err)
		}
		slog.Info("Извлечены участники", "path", filePath, "count", len(rawParticipants))

		allRawParticipants = append(allRawParticipants, rawParticipants...)
	}

	slog.Info("Всего сырых участников из всех чатов", "count", len(allRawParticipants))

	// Обогащение объединенного списка участников
	slog.Info("Обогащение данных через Telegram API...")
	finalUsers, err := uc.enricher.Enrich(ctx, allRawParticipants)
	if err != nil {
		return nil, fmt.Errorf("не удалось обогатить данные: %w", err)
	}

	// Кеширование окончательного результата
	ttl := uc.cfg.Processing.CacheTTL
	uc.cacheStore.Put(combinedHash, finalUsers, ttl)
	slog.Info("Результат кеширован для набора файлов", "hash", combinedHash, "ttl", ttl.String())

	slog.Info("Обработка успешно завершена", "user_count", len(finalUsers))
	return finalUsers, nil
}
