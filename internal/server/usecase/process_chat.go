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
	"telegram-chat-parser/internal/ratelimiter"
	"time"
)

// ProcessChatUseCase encapsulates the business logic for processing a chat export file.
type ProcessChatUseCase struct {
	cfg         *config.Config
	parser      ports.Parser
	extractor   ports.ExtractionService
	enricher    ports.EnrichmentService
	cacheStore  *cache.CacheStore
	rateLimiter *ratelimiter.AdaptiveRateLimiter
}

// NewProcessChatUseCase creates a new instance of ProcessChatUseCase.
func NewProcessChatUseCase(
	cfg *config.Config,
	parser ports.Parser,
	extractor ports.ExtractionService,
	enricher ports.EnrichmentService,
	cacheStore *cache.CacheStore,
	rateLimiter *ratelimiter.AdaptiveRateLimiter,
) *ProcessChatUseCase {
	return &ProcessChatUseCase{
		cfg:         cfg,
		parser:      parser,
		extractor:   extractor,
		enricher:    enricher,
		cacheStore:  cacheStore,
		rateLimiter: rateLimiter,
	}
}

// ProcessChat processes the chat export file.
// It fetches the data, parses it, extracts participants, and enriches them via Telegram API.
func (uc *ProcessChatUseCase) ProcessChat(ctx context.Context, filePath string) ([]domain.User, error) {
	// Calculate the file hash
	fileHash, err := cache.CalculateFileHash(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate file hash: %w", err)
	}

	// Check if the result is already in the cache
	if cachedItem, found := uc.cacheStore.Get(fileHash); found {
		slog.Info("Cache hit for file", "hash", fileHash)
		return cachedItem.Data, nil
	}

	// 1. Fetch data from file
	slog.Info("Fetching data from file...", "path", filePath)
	ds := source.NewCliSource(filePath)
	data, err := ds.Fetch()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}

	// 2. Parse data
	slog.Info("Parsing data...")
	chat, err := uc.parser.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse data: %w", err)
	}
	// Log the parsed chat structure
	slog.Info("Parsed chat structure", "message_count", len(chat.Messages), "chat_name", chat.Name, "chat_type", chat.Type)

	// 3. Extract raw participants
	slog.Info("Extracting raw participants...")
	rawParticipants, err := uc.extractor.ExtractRawParticipants(chat)
	if err != nil {
		return nil, fmt.Errorf("failed to extract raw participants: %w", err)
	}
	// Log the extracted raw participants
	slog.Info("Extracted raw participants", "count", len(rawParticipants), "participants", rawParticipants)

	// 4. Enrich data via Telegram API
	slog.Info("Enriching data via Telegram API... This may take a moment and require authentication.")
	finalUsers, err := uc.enricher.Enrich(ctx, rawParticipants)
	if err != nil {
		return nil, fmt.Errorf("failed to enrich data: %w", err)
	}

	// 5. Store the result in the cache
	ttl := time.Duration(uc.cfg.Processing.CacheTTLMinutes) * time.Minute
	uc.cacheStore.Put(fileHash, finalUsers, ttl)
	slog.Info("Result cached for file", "hash", fileHash, "ttl_minutes", ttl.Minutes())

	slog.Info("Processing completed successfully", "user_count", len(finalUsers))
	slog.Info("Final enriched users list", "users", finalUsers)
	return finalUsers, nil
}
