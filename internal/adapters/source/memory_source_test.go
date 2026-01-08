package source

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemorySource(t *testing.T) {
	t.Run("NewMemorySource создает корректный экземпляр", func(t *testing.T) {
		data := []byte("test data")
		source := NewMemorySource(data)

		assert.NotNil(t, source)
	})

	t.Run("Fetch возвращает установленные данные", func(t *testing.T) {
		expectedData := []byte("test data")
		source := NewMemorySource(expectedData)

		actualData, err := source.Fetch()

		assert.NoError(t, err)
		assert.Equal(t, expectedData, actualData)
	})

	t.Run("Fetch возвращает ошибку для nil данных", func(t *testing.T) {
		source := NewMemorySource(nil)

		actualData, err := source.Fetch()

		assert.Error(t, err)
		assert.Nil(t, actualData)
		assert.Contains(t, err.Error(), "data not set")
	})

	t.Run("Fetch возвращает копию данных", func(t *testing.T) {
		originalData := []byte("test data")
		source := NewMemorySource(originalData)

		fetchedData, err := source.Fetch()

		assert.NoError(t, err)
		assert.Equal(t, originalData, fetchedData)

		// Изменяем полученные данные
		fetchedData[0] = 'X'

		// Проверяем, что оригинальные данные не изменились
		assert.NotEqual(t, fetchedData, originalData)
		assert.Equal(t, []byte("test data"), originalData)
	})
}
