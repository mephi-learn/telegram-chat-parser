package parser

import (
	"telegram-chat-parser/internal/domain"
	"testing"
)

func TestJsonParser(t *testing.T) {
	t.Run("NewJsonParser создает корректный экземпляр", func(t *testing.T) {
		parser := NewJsonParser()
		if parser == nil {
			t.Error("Ожидался экземпляр JsonParser, получен nil")
		}
	})

	t.Run("Разбор корректного JSON с авторами и упоминаниями", func(t *testing.T) {
		parser := &JsonParser{}
		testData := `{
			"name": "Test Chat",
			"messages": [
				{
					"id": 1, "type": "message", "from": "John Doe", "from_id": "user123",
					"text_entities": [{"type": "mention", "text": "@jane_doe"}]
				},
				{
					"id": 2, "type": "message", "from": "Peter Jones", "from_id": "user456",
					"text_entities": []
				},
				{
					"id": 3, "type": "message", "from": "John Doe", "from_id": "user123",
					"text_entities": [{"type": "mention", "text": "@peter_jones"}]
				},
				{
					"id": 4, "type": "service", "actor": "Peter Jones", "actor_id": "user456",
					"text_entities": []
				}
			]
		}`

		participants, err := parser.Parse([]byte(testData))
		if err != nil {
			t.Fatalf("Неожиданная ошибка: %v", err)
		}

		expected := []domain.RawParticipant{
			{UserID: "user123", Name: "John Doe"},
			{Username: "@jane_doe"},
			{UserID: "user456", Name: "Peter Jones"},
			{Username: "@peter_jones"},
		}

		if len(participants) != len(expected) {
			t.Fatalf("Ожидалось %d участников, получено %d", len(expected), len(participants))
		}

		// Проверяем наличие всех ожидаемых участников
		foundMap := make(map[string]bool)
		for _, p := range participants {
			key := p.UserID + p.Name + p.Username
			foundMap[key] = true
		}

		for _, e := range expected {
			key := e.UserID + e.Name + e.Username
			if !foundMap[key] {
				t.Errorf("Ожидаемый участник не найден: %+v", e)
			}
		}
	})

	t.Run("Разбор некорректного JSON возвращает ошибку", func(t *testing.T) {
		parser := &JsonParser{}
		invalidData := `{"name": "Test Chat", "invalid_json":}`

		participants, err := parser.Parse([]byte(invalidData))
		if err == nil {
			t.Error("Ожидалась ошибка для некорректного JSON, получено nil")
		}

		if participants != nil {
			t.Errorf("Ожидался nil срез участников, получено %d", len(participants))
		}
	})

	t.Run("Разбор JSON без сообщений возвращает пустой срез", func(t *testing.T) {
		parser := &JsonParser{}
		testData := `{"name": "Test Chat", "messages": []}`

		participants, err := parser.Parse([]byte(testData))
		if err != nil {
			t.Fatalf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 0 {
			t.Errorf("Ожидался пустой срез участников, получено %d", len(participants))
		}
	})
}
