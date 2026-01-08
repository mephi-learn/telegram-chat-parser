package usecase

import (
	"context"
	"errors"
	"os"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks for dependencies
type mockParser struct{ mock.Mock }

func (m *mockParser) Parse(data []byte) (*domain.ExportedChat, error) {
	args := m.Called(data)
	if res := args.Get(0); res != nil {
		return res.(*domain.ExportedChat), args.Error(1)
	}
	return nil, args.Error(1)
}

type mockExtractor struct{ mock.Mock }

func (m *mockExtractor) ExtractRawParticipants(chat *domain.ExportedChat) ([]domain.RawParticipant, error) {
	args := m.Called(chat)
	if res := args.Get(0); res != nil {
		return res.([]domain.RawParticipant), args.Error(1)
	}
	return nil, args.Error(1)
}

type mockEnricher struct{ mock.Mock }

func (m *mockEnricher) Enrich(ctx context.Context, participants []domain.RawParticipant) ([]domain.User, error) {
	args := m.Called(ctx, participants)
	if res := args.Get(0); res != nil {
		return res.([]domain.User), args.Error(1)
	}
	return nil, args.Error(1)
}

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-*.json")
	assert.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	assert.NoError(t, err)
	assert.NoError(t, tmpFile.Close())
	return tmpFile.Name()
}

func TestProcessChatUseCase(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{Processing: config.Processing{CacheTTL: 10 * time.Minute}}

	// Create a dummy file for hash calculation
	filePath := createTempFile(t, "{}")
	fileHash, err := cache.CalculateFileHash(filePath)
	assert.NoError(t, err)

	t.Run("success flow", func(t *testing.T) {
		parser := new(mockParser)
		extractor := new(mockExtractor)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()

		uc := NewProcessChatUseCase(cfg, parser, extractor, enricher, cacheStore)

		chat := &domain.ExportedChat{Name: "Test Chat"}
		rawParticipants := []domain.RawParticipant{{UserID: "user1"}}
		finalUsers := []domain.User{{ID: 1, Name: "User 1"}}

		parser.On("Parse", mock.Anything).Return(chat, nil)
		extractor.On("ExtractRawParticipants", chat).Return(rawParticipants, nil)
		enricher.On("Enrich", ctx, rawParticipants).Return(finalUsers, nil)

		users, err := uc.ProcessChat(ctx, filePath)

		assert.NoError(t, err)
		assert.Equal(t, finalUsers, users)

		// Check cache
		cached, found := cacheStore.Get(fileHash)
		assert.True(t, found)
		assert.Equal(t, finalUsers, cached.Data)

		parser.AssertExpectations(t)
		extractor.AssertExpectations(t)
		enricher.AssertExpectations(t)
	})

	t.Run("cache hit", func(t *testing.T) {
		parser := new(mockParser) // Should not be called
		extractor := new(mockExtractor)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()

		cachedUsers := []domain.User{{ID: 99, Name: "Cached User"}}
		cacheStore.Put(fileHash, cachedUsers, 10*time.Minute)

		uc := NewProcessChatUseCase(cfg, parser, extractor, enricher, cacheStore)

		users, err := uc.ProcessChat(ctx, filePath)

		assert.NoError(t, err)
		assert.Equal(t, cachedUsers, users)
		parser.AssertNotCalled(t, "Parse", mock.Anything)
	})

	t.Run("fetch error", func(t *testing.T) {
		uc := NewProcessChatUseCase(cfg, nil, nil, nil, cache.NewCacheStore())
		_, err := uc.ProcessChat(ctx, "non_existent_file.json")
		assert.Error(t, err)
	})

	t.Run("parse error", func(t *testing.T) {
		parser := new(mockParser)
		uc := NewProcessChatUseCase(cfg, parser, nil, nil, cache.NewCacheStore())
		parseErr := errors.New("parse error")
		parser.On("Parse", mock.Anything).Return(nil, parseErr)

		_, err := uc.ProcessChat(ctx, filePath)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), parseErr.Error())
		parser.AssertExpectations(t)
	})

	t.Run("extract error", func(t *testing.T) {
		parser := new(mockParser)
		extractor := new(mockExtractor)
		uc := NewProcessChatUseCase(cfg, parser, extractor, nil, cache.NewCacheStore())

		chat := &domain.ExportedChat{}
		extractErr := errors.New("extract error")

		parser.On("Parse", mock.Anything).Return(chat, nil)
		extractor.On("ExtractRawParticipants", chat).Return(nil, extractErr)

		_, err := uc.ProcessChat(ctx, filePath)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), extractErr.Error())
		extractor.AssertExpectations(t)
	})

	t.Run("enrich error", func(t *testing.T) {
		parser := new(mockParser)
		extractor := new(mockExtractor)
		enricher := new(mockEnricher)
		uc := NewProcessChatUseCase(cfg, parser, extractor, enricher, cache.NewCacheStore())

		chat := &domain.ExportedChat{}
		rawParticipants := []domain.RawParticipant{}
		enrichErr := errors.New("enrich error")

		parser.On("Parse", mock.Anything).Return(chat, nil)
		extractor.On("ExtractRawParticipants", chat).Return(rawParticipants, nil)
		enricher.On("Enrich", ctx, rawParticipants).Return(nil, enrichErr)

		_, err := uc.ProcessChat(ctx, filePath)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), enrichErr.Error())
		enricher.AssertExpectations(t)
	})
}
