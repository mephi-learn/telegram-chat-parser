package cache

import (
	"context"
	"os"
	"telegram-chat-parser/internal/domain"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheStore(t *testing.T) {
	t.Run("Создание нового хранилища кэша", func(t *testing.T) {
		cs := NewCacheStore()
		assert.NotNil(t, cs)
		assert.NotNil(t, cs.cache)
	})

	t.Run("Запись и чтение из кэша", func(t *testing.T) {
		cs := NewCacheStore()
		key := "test_key"
		data := []domain.User{{ID: 1, Name: "User1"}}
		ttl := 1 * time.Minute

		cs.Put(key, data, ttl)

		item, found := cs.Get(key)
		require.True(t, found)
		require.NotNil(t, item)
		assert.Equal(t, data, item.Data)
		assert.WithinDuration(t, time.Now().Add(ttl), item.ExpiresAt, 1*time.Second)
	})

	t.Run("Чтение несуществующего ключа", func(t *testing.T) {
		cs := NewCacheStore()
		_, found := cs.Get("non_existent_key")
		assert.False(t, found)
	})

	t.Run("Чтение просроченного ключа", func(t *testing.T) {
		cs := NewCacheStore()
		key := "expired_key"
		data := []domain.User{{ID: 1}}
		ttl := -1 * time.Second // Просрочено в прошлом

		cs.Put(key, data, ttl)

		_, found := cs.Get(key)
		assert.False(t, found)
	})

	t.Run("Очистка просроченных ключей", func(t *testing.T) {
		cs := NewCacheStore()
		expiredKey := "expired"
		validKey := "valid"

		cs.Put(expiredKey, []domain.User{{ID: 1}}, -1*time.Minute)
		cs.Put(validKey, []domain.User{{ID: 2}}, 1*time.Minute)

		cs.CleanupExpired()

		_, foundExpired := cs.Get(expiredKey)
		assert.False(t, foundExpired, "Просроченный элемент должен быть удален")

		_, foundValid := cs.Get(validKey)
		assert.True(t, foundValid, "Действительный элемент не должен быть удален")
	})
}

func TestStartCleanupTicker(t *testing.T) {
	cs := NewCacheStore()
	expiredKey := "expired"
	validKey := "valid"

	cs.Put(expiredKey, []domain.User{{ID: 1}}, 50*time.Millisecond)
	cs.Put(validKey, []domain.User{{ID: 2}}, 1*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cs.StartCleanupTicker(ctx, 100*time.Millisecond)

	// Ждем, пока таймер сработает хотя бы раз
	time.Sleep(150 * time.Millisecond)

	_, foundExpired := cs.Get(expiredKey)
	assert.False(t, foundExpired, "Просроченный элемент должен быть удален таймером")

	_, foundValid := cs.Get(validKey)
	assert.True(t, foundValid, "Действительный элемент должен остаться")

	// Убеждаемся, что горутина останавливается
	cancel()
	time.Sleep(50 * time.Millisecond) // Даем время на реакцию на отмену
}

func TestCalculateFileHash(t *testing.T) {
	t.Run("Успешное вычисление хеша", func(t *testing.T) {
		// Создаем временный файл
		content := []byte("hello world")
		tmpFile, err := os.CreateTemp("", "testhash_*.txt")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write(content)
		require.NoError(t, err)
		err = tmpFile.Close()
		require.NoError(t, err)

		hash, err := CalculateFileHash(tmpFile.Name())
		require.NoError(t, err)
		// SHA256 для "hello world"
		expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
		assert.Equal(t, expectedHash, hash)
	})

	t.Run("Файл не найден", func(t *testing.T) {
		_, err := CalculateFileHash("non_existent_file.txt")
		assert.Error(t, err)
	})

	t.Run("Невозможно прочитать файл", func(t *testing.T) {
		// Создаем временную директорию вместо файла
		tmpDir, err := os.MkdirTemp("", "testhashdir_*")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		_, err = CalculateFileHash(tmpDir)
		assert.Error(t, err, "Должна быть ошибка при попытке хешировать директорию")
	})
}
