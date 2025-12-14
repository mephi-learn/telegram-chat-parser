package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"telegram-chat-parser/internal/domain"
	"time"
)

// CacheItem представляет кэшированный результат
type CacheItem struct {
	Data      []domain.User
	ExpiresAt time.Time
}

// CacheStore управляет хранением и извлечением кэшированных результатов
type CacheStore struct {
	cache map[string]*CacheItem
	mutex sync.RWMutex
}

// NewCacheStore создает новый экземпляр CacheStore
func NewCacheStore() *CacheStore {
	return &CacheStore{
		cache: make(map[string]*CacheItem),
	}
}

// Get извлекает кэшированный элемент по его ключу (хешу)
func (cs *CacheStore) Get(key string) (*CacheItem, bool) {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	item, exists := cs.cache[key]
	if !exists || time.Now().After(item.ExpiresAt) {
		// Элемент не существует или срок его действия истек
		return nil, false
	}

	return item, true
}

// Put сохраняет элемент в кэш с указанным сроком действия
func (cs *CacheStore) Put(key string, data []domain.User, ttl time.Duration) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	now := time.Now()
	cs.cache[key] = &CacheItem{
		Data:      data,
		ExpiresAt: now.Add(ttl),
	}
}

// CleanupExpired удаляет просроченные элементы из кэша
func (cs *CacheStore) CleanupExpired() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	now := time.Now()
	for key, item := range cs.cache {
		if now.After(item.ExpiresAt) {
			delete(cs.cache, key)
		}
	}
}

// StartCleanupTicker запускает таймер для периодической очистки просроченных элементов
func (cs *CacheStore) StartCleanupTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cs.CleanupExpired()
			}
		}
	}()
}

// CalculateFileHash вычисляет хеш SHA256 содержимого файла
func CalculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть файл: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("не удалось прочитать файл: %w", err)
	}

	hash := fmt.Sprintf("%x", hasher.Sum(nil))
	return hash, nil
}
