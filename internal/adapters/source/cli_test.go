package source

import (
	"os"
	"testing"
)

func TestCliSource(t *testing.T) {
	t.Run("NewCliSource создает корректный экземпляр", func(t *testing.T) {
		source := NewCliSource("test_file.json")
		if source == nil {
			t.Error("Ожидался экземпляр CliSource, получен nil")
		}
	})

	t.Run("Fetch возвращает ошибку для пустого пути к файлу", func(t *testing.T) {
		source := &CliSource{filePath: ""}

		data, err := source.Fetch()
		if err == nil {
			t.Error("Ожидалась ошибка для пустого пути к файлу, получено nil")
		}

		if data != nil {
			t.Error("Ожидались nil данные для пустого пути к файлу, получены данные")
		}

		if err.Error() != "не указан путь к файлу" {
			t.Errorf("Ожидалось сообщение об ошибке 'не указан путь к файлу', получено '%s'", err.Error())
		}
	})

	t.Run("Fetch возвращает ошибку для несуществующего файла", func(t *testing.T) {
		source := &CliSource{filePath: "non_existing_file.json"}

		data, err := source.Fetch()
		if err == nil {
			t.Error("Ожидалась ошибка для несуществующего файла, получено nil")
		}

		if data != nil {
			t.Error("Ожидались nil данные для несуществующего файла, получены данные")
		}
	})

	t.Run("Fetch возвращает данные для существующего файла", func(t *testing.T) {
		// Создаем временный файл для тестирования
		testData := []byte(`{"name": "Test Chat", "type": "private", "messages": []}`)
		tmpfile, err := os.CreateTemp("", "test_chat_*.json")
		if err != nil {
			t.Fatal("Не удалось создать временный файл")
		}
		defer os.Remove(tmpfile.Name()) // Очистка

		if _, err := tmpfile.Write(testData); err != nil {
			t.Fatal("Не удалось записать во временный файл")
		}
		if err := tmpfile.Close(); err != nil {
			t.Fatal("Не удалось закрыть временный файл")
		}

		source := &CliSource{filePath: tmpfile.Name()}

		data, err := source.Fetch()
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(data) == 0 {
			t.Error("Ожидались непустые данные, получено пусто")
		}

		if string(data) != string(testData) {
			t.Errorf("Ожидались данные '%s', получено '%s'", string(testData), string(data))
		}
	})
}
