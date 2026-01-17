package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ports"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks for dependencies
type mockParser struct{ mock.Mock }

func (m *mockParser) Parse(data []byte) ([]domain.RawParticipant, error) {
	args := m.Called(data)
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

type mockParserFactory struct{ mock.Mock }

func (m *mockParserFactory) GetParser(fileName string) (ports.Parser, error) {
	args := m.Called(fileName)
	if res := args.Get(0); res != nil {
		return res.(ports.Parser), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockParserFactory) GetParserForData(data []byte) (ports.Parser, error) {
	args := m.Called(data)
	if res := args.Get(0); res != nil {
		return res.(ports.Parser), args.Error(1)
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
		parserFactory := new(mockParserFactory)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()
		uc := NewProcessChatUseCase(cfg, parserFactory, enricher, cacheStore)

		// File 1
		filePath1 := createTempFile(t, `{"name": "chat1"}`)
		rawParticipants1 := []domain.RawParticipant{{UserID: "user1"}}
		parserFactory.On("GetParser", filePath1).Return(parser, nil).Once()
		parser.On("Parse", []byte(`{"name": "chat1"}`)).Return(rawParticipants1, nil).Once()

		// File 2
		filePath2 := createTempFile(t, `{"name": "chat2"}`)
		rawParticipants2 := []domain.RawParticipant{{UserID: "user2"}}
		parserFactory.On("GetParser", filePath2).Return(parser, nil).Once()
		parser.On("Parse", []byte(`{"name": "chat2"}`)).Return(rawParticipants2, nil).Once()

		// Combined
		allRawParticipants := append(rawParticipants1, rawParticipants2...)
		finalUsers := []domain.User{{ID: 1, Name: "User 1"}, {ID: 2, Name: "User 2"}}
		enricher.On("Enrich", mock.Anything, allRawParticipants).Return(finalUsers, nil).Once()

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

		parserFactory.AssertExpectations(t)
		parser.AssertExpectations(t)
		enricher.AssertExpectations(t)
	})

	t.Run("cache hit", func(t *testing.T) {
		parserFactory := new(mockParserFactory)
		enricher := new(mockEnricher)
		cacheStore := cache.NewCacheStore()
		uc := NewProcessChatUseCase(cfg, parserFactory, enricher, cacheStore)

		cachedUsers := []domain.User{{ID: 99, Name: "Cached User"}}
		fileHash, _ := cache.CalculateFileHash(filePath)
		combinedHash := cache.CalculateHashFromString(fmt.Sprintf("%v", []string{fileHash}))
		cacheStore.Put(combinedHash, cachedUsers, 10*time.Minute)

		users, err := uc.ProcessChat(ctx, []string{filePath})

		assert.NoError(t, err)
		assert.Equal(t, cachedUsers, users)
		parserFactory.AssertNotCalled(t, "GetParser", mock.Anything)
	})

	t.Run("fetch error", func(t *testing.T) {
		uc := NewProcessChatUseCase(cfg, new(mockParserFactory), nil, cache.NewCacheStore())
		_, err := uc.ProcessChat(ctx, []string{"non_existent_file.json"})
		assert.Error(t, err)
	})

	t.Run("parse error", func(t *testing.T) {
		parser := new(mockParser)
		parserFactory := new(mockParserFactory)
		uc := NewProcessChatUseCase(cfg, parserFactory, nil, cache.NewCacheStore())
		parseErr := errors.New("parse error")

		parserFactory.On("GetParser", filePath).Return(parser, nil)
		parser.On("Parse", mock.Anything).Return(nil, parseErr)

		_, err := uc.ProcessChat(ctx, []string{filePath})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), parseErr.Error())
		parserFactory.AssertExpectations(t)
		parser.AssertExpectations(t)
	})

	t.Run("enrich error", func(t *testing.T) {
		parser := new(mockParser)
		parserFactory := new(mockParserFactory)
		enricher := new(mockEnricher)
		uc := NewProcessChatUseCase(cfg, parserFactory, enricher, cache.NewCacheStore())

		rawParticipants := []domain.RawParticipant{}
		enrichErr := errors.New("enrich error")

		parserFactory.On("GetParser", filePath).Return(parser, nil)
		parser.On("Parse", mock.Anything).Return(rawParticipants, nil)
		enricher.On("Enrich", mock.Anything, mock.AnythingOfType("[]domain.RawParticipant")).Return(nil, enrichErr)

		_, err := uc.ProcessChat(ctx, []string{filePath})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), enrichErr.Error())
		enricher.AssertExpectations(t)
	})
}
