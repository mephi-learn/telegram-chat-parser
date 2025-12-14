package services

import (
	"encoding/json"
	"reflect"
	"telegram-chat-parser/internal/domain"
	"testing"
)

func TestExtractionService(t *testing.T) {
	t.Run("NewExtractionService создает корректный экземпляр", func(t *testing.T) {
		service := NewExtractionService()
		if service == nil {
			t.Error("Ожидался экземпляр ExtractionService, получен nil")
		}
	})

	t.Run("ExtractRawParticipants извлекает авторов", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:           1,
					Type:         "message",
					Date:         "2023-01-01T00:00:00",
					From:         "John Doe",
					FromID:       "user123",
					Text:         json.RawMessage(`"Hello, World!"`),
					TextEntities: []domain.TextEntity{},
				},
				{
					ID:           2,
					Type:         "message",
					Date:         "2023-01-01T00:01:00",
					From:         "Jane Smith",
					FromID:       "user456",
					Text:         json.RawMessage(`"Hi there!"`),
					TextEntities: []domain.TextEntity{},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 2 {
			t.Errorf("Ожидалось 2 участника, получено %d", len(participants))
		}

		expected := []domain.RawParticipant{
			{UserID: "user123", Name: "John Doe"},
			{UserID: "user456", Name: "Jane Smith"},
		}

		for i, exp := range expected {
			if !reflect.DeepEqual(participants[i], exp) {
				t.Errorf("Ожидался участник %+v, получено %+v", exp, participants[i])
			}
		}
	})

	t.Run("ExtractRawParticipants извлекает упоминания", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:     1,
					Type:   "message",
					Date:   "2023-01-01T00:00:00",
					From:   "John Doe",
					FromID: "user123",
					Text:   json.RawMessage(`"Hello @testuser!"`),
					TextEntities: []domain.TextEntity{
						{
							Type: "mention",
							Text: "@testuser",
						},
					},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 2 {
			t.Errorf("Ожидалось 2 участника (автор + упоминание), получено %d", len(participants))
		}

		hasAuthor := false
		hasMention := false
		for _, p := range participants {
			if p.UserID == "user123" && p.Name == "John Doe" {
				hasAuthor = true
			}
			if p.Username == "@testuser" {
				hasMention = true
			}
		}

		if !hasAuthor {
			t.Error("Ожидалось найти автора John Doe")
		}

		if !hasMention {
			t.Error("Ожидалось найти упоминание @testuser")
		}
	})

	t.Run("ExtractRawParticipants обрабатывает служебные сообщения", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:           1,
					Type:         "service",
					Date:         "2023-01-01T00:00:00",
					From:         "John Doe",
					FromID:       "user123",
					Actor:        "Jane Smith",
					ActorID:      "user456",
					Text:         json.RawMessage(`"changed group name"`),
					TextEntities: []domain.TextEntity{},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 1 {
			t.Errorf("Ожидался 1 участник (из actor), получено %d", len(participants))
		}

		if participants[0].UserID != "user456" || participants[0].Name != "Jane Smith" {
			t.Errorf("Ожидался участник с UserID 'user456' и именем 'Jane Smith', получено %+v", participants[0])
		}
	})

	t.Run("ExtractRawParticipants обрабатывает дублирующихся пользователей", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:           1,
					Type:         "message",
					Date:         "2023-01-01T00:00:00",
					From:         "John Doe",
					FromID:       "user123",
					Text:         json.RawMessage(`"Hello!"`),
					TextEntities: []domain.TextEntity{},
				},
				{
					ID:           2,
					Type:         "message",
					Date:         "2023-01-01T00:01:00",
					From:         "John Doe",
					FromID:       "user123", // Тот же пользователь
					Text:         json.RawMessage(`"Hi again!"`),
					TextEntities: []domain.TextEntity{},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 1 {
			t.Errorf("Ожидался 1 участник (без дубликатов), получено %d", len(participants))
		}

		if participants[0].UserID != "user123" || participants[0].Name != "John Doe" {
			t.Errorf("Ожидался участник с UserID 'user123' и именем 'John Doe', получено %+v", participants[0])
		}
	})

	t.Run("ExtractRawParticipants обрабатывает дублирующиеся упоминания", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:     1,
					Type:   "message",
					Date:   "2023-01-01T00:00:00",
					From:   "John Doe",
					FromID: "user123",
					Text:   json.RawMessage(`"Hello @testuser!"`),
					TextEntities: []domain.TextEntity{
						{
							Type: "mention",
							Text: "@testuser",
						},
					},
				},
				{
					ID:     2,
					Type:   "message",
					Date:   "2023-01-01T00:01:00",
					From:   "Jane Smith",
					FromID: "user456",
					Text:   json.RawMessage(`"Hi @testuser!"`),
					TextEntities: []domain.TextEntity{
						{
							Type: "mention",
							Text: "@testuser", // То же упоминание
						},
					},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		// Должно быть 3 участника: John Doe, Jane Smith и упоминание @testuser
		mentionCount := 0
		userCount := 0
		for _, p := range participants {
			if p.Username != "" {
				mentionCount++
			} else if p.UserID != "" {
				userCount++
			}
		}

		if userCount != 2 {
			t.Errorf("Ожидалось 2 пользователя (John, Jane), получено %d", userCount)
		}

		if mentionCount != 1 {
			t.Errorf("Ожидалось 1 уникальное упоминание (@testuser), получено %d", mentionCount)
		}
	})

	t.Run("ExtractRawParticipants фильтрует удаленные аккаунты", func(t *testing.T) {
		service := NewExtractionService()

		chat := &domain.ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []domain.Message{
				{
					ID:           1,
					Type:         "message",
					Date:         "2023-01-01T00:00:00",
					From:         "Deleted Account",
					FromID:       "user123",
					Text:         json.RawMessage(`"Deleted message"`),
					TextEntities: []domain.TextEntity{},
				},
			},
		}

		participants, err := service.ExtractRawParticipants(chat)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		if len(participants) != 0 {
			t.Errorf("Ожидалось 0 участников (удаленный аккаунт отфильтрован), получено %d", len(participants))
		}
	})
}
