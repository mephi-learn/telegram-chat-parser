package parser

import (
	"testing"
)

func TestJsonParser(t *testing.T) {
	t.Run("NewJsonParser создает корректный экземпляр", func(t *testing.T) {
		parser := NewJsonParser()
		if parser == nil {
			t.Error("Ожидался экземпляр JsonParser, получен nil")
		}
	})

	t.Run("Разбор корректного JSON", func(t *testing.T) {
		parser := &JsonParser{}
		testData := `{
			"name": "Test Chat",
			"type": "private_group",
			"id": 12345,
			"messages": [
				{
					"id": 1,
					"type": "message",
					"date": "2023-01-01T00:00:00",
					"from": "John Doe",
					"from_id": "user123",
					"text": "Hello, World!",
					"text_entities": []
				}
			]
		}`

		chat, err := parser.Parse([]byte(testData))
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if chat.Name != "Test Chat" {
			t.Errorf("Ожидалось имя 'Test Chat', получено '%s'", chat.Name)
		}

		if chat.Type != "private_group" {
			t.Errorf("Ожидался тип 'private_group', получено '%s'", chat.Type)
		}

		if chat.ID != 12345 {
			t.Errorf("Ожидался ID 12345, получено %d", chat.ID)
		}

		if len(chat.Messages) != 1 {
			t.Errorf("Ожидалось 1 сообщение, получено %d", len(chat.Messages))
		}

		if chat.Messages[0].ID != 1 {
			t.Errorf("Ожидался ID первого сообщения 1, получено %d", chat.Messages[0].ID)
		}
	})

	t.Run("Разбор некорректного JSON возвращает ошибку", func(t *testing.T) {
		parser := &JsonParser{}
		invalidData := `{"name": "Test Chat", "invalid_json":}`

		chat, err := parser.Parse([]byte(invalidData))
		if err == nil {
			t.Error("Ожидалась ошибка для некорректного JSON, получено nil")
		}

		if chat != nil {
			t.Error("Ожидался nil чат для некорректного JSON, получен чат")
		}
	})

	t.Run("Разбор пустого JSON возвращает ошибку", func(t *testing.T) {
		parser := &JsonParser{}
		emptyData := ``

		chat, err := parser.Parse([]byte(emptyData))
		if err == nil {
			t.Error("Ожидалась ошибка для пустого JSON, получено nil")
		}

		if chat != nil {
			t.Error("Ожидался nil чат для пустого JSON, получен чат")
		}
	})

	t.Run("Разбор JSON с вложенными структурами", func(t *testing.T) {
		parser := &JsonParser{}
		testData := `{
			"name": "Test Chat",
			"type": "private_group",
			"id": 12345,
			"messages": [
				{
					"id": 1,
					"type": "message",
					"date": "2023-01-01T00:00:00",
					"from": "John Doe",
					"from_id": "user123",
					"text": ["part1", "part2"],
					"text_entities": [
						{
							"type": "mention",
							"text": "@testuser"
						}
					]
				}
			]
		}`

		chat, err := parser.Parse([]byte(testData))
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if chat.Name != "Test Chat" {
			t.Errorf("Ожидалось имя 'Test Chat', получено '%s'", chat.Name)
		}

		if len(chat.Messages) != 1 {
			t.Errorf("Ожидалось 1 сообщение, получено %d", len(chat.Messages))
		}

		if len(chat.Messages[0].TextEntities) != 1 {
			t.Errorf("Ожидалась 1 текстовая сущность, получено %d", len(chat.Messages[0].TextEntities))
		}

		if chat.Messages[0].TextEntities[0].Type != "mention" {
			t.Errorf("Ожидался тип первой текстовой сущности 'mention', получено '%s'",
				chat.Messages[0].TextEntities[0].Type)
		}
	})
}
