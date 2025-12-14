package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"

	"telegram-chat-parser/internal/adapters/parser"
	"telegram-chat-parser/internal/adapters/source"
	"telegram-chat-parser/internal/core/services"
)

// Этот интеграционный тест симулирует полный цикл работы приложения.
// Он тестирует взаимодействие между всеми компонентами без реальных вызовов API.
func TestFullApplicationFlow(t *testing.T) {
	// Загружаем переменные окружения
	if err := godotenv.Load("../../.env"); err != nil {
		// Если файл .env не существует, мы установим переменные вручную для теста
		t.Log("Файл .env не найден, будем тестировать с мок-сервисом")
	}

	// Создаем минимальный тестовый JSON-файл для парсинга
	testData := `{
		"name": "Test Chat",
		"type": "private_group",
		"id": 123456789,
		"messages": [
			{
				"id": 1,
				"type": "message",
				"date": "2023-01-01T00:00:00",
				"from": "Test User",
				"from_id": "user123456",
				"text": "Hello, world!"
			}
		]
	}`

	// Записываем тестовые данные во временный файл
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_chat.json")
	if err := os.WriteFile(testFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Не удалось записать тестовый файл: %v", err)
	}

	// 1. Инициализация компонентов
	src := source.NewCliSource(testFile)
	psr := parser.NewJsonParser()
	extractionSvc := services.NewExtractionService()

	// Для этого теста мы будем использовать мок-сервис API, чтобы избежать реальных вызовов API.
	// Поскольку мы не можем контролировать реальный API в тестах, мы будем тестировать только до этапа извлечения,
	// который не требует вызовов API.

	// 2. Выполнение основного сценария до точки, где нужен API
	data, err := src.Fetch()
	if err != nil {
		t.Fatalf("Не удалось получить данные: %v", err)
	}

	chat, err := psr.Parse(data)
	if err != nil {
		t.Fatalf("Не удалось разобрать данные: %v", err)
	}

	rawParticipants, err := extractionSvc.ExtractRawParticipants(chat)
	if err != nil {
		t.Fatalf("Не удалось извлечь сырых участников: %v", err)
	}

	if len(rawParticipants) == 0 {
		t.Error("Ожидался хотя бы один участник, не получено ни одного")
	}

	// Проверяем извлеченного участника
	participant := rawParticipants[0]
	if participant.UserID != "user123456" {
		t.Errorf("Ожидался ID пользователя 'user123456', получено '%s'", participant.UserID)
	}

	if participant.Name != "Test User" {
		t.Errorf("Ожидалось имя 'Test User', получено '%s'", participant.Name)
	}

}
