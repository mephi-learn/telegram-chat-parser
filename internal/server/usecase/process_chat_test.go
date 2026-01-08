package usecase

import (
	"context"
	"errors"
	"fmt"
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
	t.Run("success flow with multiple files", func(t *testing.T) {
		parser := new(mockParser)
		extractor := new(mockExtractor)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()
		uc := NewProcessChatUseCase(cfg, parser, extractor, enricher, cacheStore)

		// File 1
		filePath1 := createTempFile(t, `{"name": "chat1"}`)
		chat1 := &domain.ExportedChat{Name: "Test Chat 1"}
		rawParticipants1 := []domain.RawParticipant{{UserID: "user1"}}
		parser.On("Parse", []byte(`{"name": "chat1"}`)).Return(chat1, nil).Once()
		extractor.On("ExtractRawParticipants", chat1).Return(rawParticipants1, nil).Once()

		// File 2
		filePath2 := createTempFile(t, `{"name": "chat2"}`)
		chat2 := &domain.ExportedChat{Name: "Test Chat 2"}
		rawParticipants2 := []domain.RawParticipant{{UserID: "user2"}}
		parser.On("Parse", []byte(`{"name": "chat2"}`)).Return(chat2, nil).Once()
		extractor.On("ExtractRawParticipants", chat2).Return(rawParticipants2, nil).Once()

		// Combined
		allRawParticipants := append(rawParticipants1, rawParticipants2...)
		finalUsers := []domain.User{{ID: 1, Name: "User 1"}, {ID: 2, Name: "User 2"}}
		enricher.On("Enrich", ctx, allRawParticipants).Return(finalUsers, nil).Once()

		users, err := uc.ProcessChat(ctx, []string{filePath1, filePath2})

		assert.NoError(t, err)
		assert.Equal(t, finalUsers, users)

		// Check cache
		hash1, _ := cache.CalculateFileHash(filePath1)
		hash2, _ := cache.CalculateFileHash(filePath2)
		combinedHash := cache.CalculateHashFromString(fmt.Sprintf("%v", []string{hash1, hash2}))
		cached, found := cacheStore.Get(combinedHash)
		assert.True(t, found)
		assert.Equal(t, finalUsers, cached.Data)

		parser.AssertExpectations(t)
		extractor.AssertExpectations(t)
		enricher.AssertExpectations(t)
	})

	t.Run("cache hit", func(t *testing.T) {
		parser := new(mockParser)
		extractor := new(mockExtractor)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()
		uc := NewProcessChatUseCase(cfg, parser, extractor, enricher, cacheStore)

		cachedUsers := []domain.User{{ID: 99, Name: "Cached User"}}
		fileHash, _ := cache.CalculateFileHash(filePath)
		combinedHash := cache.CalculateHashFromString(fmt.Sprintf("%v", []string{fileHash}))
		cacheStore.Put(combinedHash, cachedUsers, 10*time.Minute)

		users, err := uc.ProcessChat(ctx, []string{filePath})

		assert.NoError(t, err)
		assert.Equal(t, cachedUsers, users)
		parser.AssertNotCalled(t, "Parse", mock.Anything)
	})

	t.Run("fetch error", func(t *testing.T) {
		uc := NewProcessChatUseCase(cfg, nil, nil, nil, cache.NewCacheStore())
		_, err := uc.ProcessChat(ctx, []string{"non_existent_file.json"})
		assert.Error(t, err)
	})

	t.Run("parse error", func(t *testing.T) {
		parser := new(mockParser)
		uc := NewProcessChatUseCase(cfg, parser, nil, nil, cache.NewCacheStore())
		parseErr := errors.New("parse error")
		parser.On("Parse", mock.Anything).Return(nil, parseErr)

		_, err := uc.ProcessChat(ctx, []string{filePath})

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

		_, err := uc.ProcessChat(ctx, []string{filePath})

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
		enricher.On("Enrich", ctx, mock.AnythingOfType("[]domain.RawParticipant")).Return(nil, enrichErr)

		_, err := uc.ProcessChat(ctx, []string{filePath})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), enrichErr.Error())
		enricher.AssertExpectations(t)
	})
}
