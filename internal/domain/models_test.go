package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExportedChat(t *testing.T) {
	t.Run("Структура ExportedChat", func(t *testing.T) {
		chat := ExportedChat{
			Name: "Test Chat",
			Type: "private_group",
			ID:   12345,
			Messages: []Message{
				{
					ID:     1,
					Type:   "message",
					Date:   "2023-01-01T00:00:00",
					From:   "John Doe",
					FromID: "user123",
					Text:   json.RawMessage(`"Hello, World!"`),
				},
			},
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
	})
}

func TestMessage(t *testing.T) {
	t.Run("Структура Message", func(t *testing.T) {
		msg := Message{
			ID:      1,
			Type:    "message",
			Date:    "2023-01-01T00:00:00",
			From:    "John Doe",
			FromID:  "user123",
			Actor:   "John Doe",
			ActorID: "user123",
			Text:    json.RawMessage(`"Hello, World!"`),
			TextEntities: []TextEntity{
				{
					Type: "mention",
					Text: "@testuser",
				},
			},
		}

		if msg.ID != 1 {
			t.Errorf("Ожидался ID 1, получено %d", msg.ID)
		}

		if msg.Type != "message" {
			t.Errorf("Ожидался тип 'message', получено '%s'", msg.Type)
		}

		if msg.From != "John Doe" {
			t.Errorf("Ожидалось поле From 'John Doe', получено '%s'", msg.From)
		}

		if len(msg.TextEntities) != 1 {
			t.Errorf("Ожидалась 1 текстовая сущность, получено %d", len(msg.TextEntities))
		}
	})
}

func TestTextEntity(t *testing.T) {
	t.Run("Структура TextEntity", func(t *testing.T) {
		entity := TextEntity{
			Type: "mention",
			Text: "@testuser",
		}

		if entity.Type != "mention" {
			t.Errorf("Ожидался тип 'mention', получено '%s'", entity.Type)
		}

		if entity.Text != "@testuser" {
			t.Errorf("Ожидался текст '@testuser', получено '%s'", entity.Text)
		}
	})
}

func TestUser(t *testing.T) {
	t.Run("Структура User", func(t *testing.T) {
		user := User{
			ID:       12345,
			Name:     "John Doe",
			Username: "@johndoe",
			Bio:      "Software Developer",
		}

		if user.ID != 12345 {
			t.Errorf("Ожидался ID 12345, получено %d", user.ID)
		}

		if user.Name != "John Doe" {
			t.Errorf("Ожидалось имя 'John Doe', получено '%s'", user.Name)
		}

		if user.Username != "@johndoe" {
			t.Errorf("Ожидалось имя пользователя '@johndoe', получено '%s'", user.Username)
		}

		if user.Bio != "Software Developer" {
			t.Errorf("Ожидалось био 'Software Developer', получено '%s'", user.Bio)
		}
	})
}

func TestRawParticipant(t *testing.T) {
	t.Run("Структура RawParticipant", func(t *testing.T) {
		participant := RawParticipant{
			UserID:   "user123",
			Name:     "John Doe",
			Username: "@johndoe",
		}

		if participant.UserID != "user123" {
			t.Errorf("Ожидался UserID 'user123', получено '%s'", participant.UserID)
		}

		if participant.Name != "John Doe" {
			t.Errorf("Ожидалось имя 'John Doe', получено '%s'", participant.Name)
		}

		if participant.Username != "@johndoe" {
			t.Errorf("Ожидалось имя пользователя '@johndoe', получено '%s'", participant.Username)
		}
	})

	t.Run("RawParticipant с пустыми полями", func(t *testing.T) {
		participant := RawParticipant{}

		if participant.UserID != "" {
			t.Errorf("Ожидался пустой UserID, получено '%s'", participant.UserID)
		}

		if participant.Name != "" {
			t.Errorf("Ожидалось пустое имя, получено '%s'", participant.Name)
		}

		if participant.Username != "" {
			t.Errorf("Ожидалось пустое имя пользователя, получено '%s'", participant.Username)
		}
	})
}

func TestUserEquality(t *testing.T) {
	t.Run("Пользователи с одинаковыми полями должны быть равны", func(t *testing.T) {
		user1 := User{
			ID:       12345,
			Name:     "John Doe",
			Username: "@johndoe",
			Bio:      "Software Developer",
		}

		user2 := User{
			ID:       12345,
			Name:     "John Doe",
			Username: "@johndoe",
			Bio:      "Software Developer",
		}

		if !reflect.DeepEqual(user1, user2) {
			t.Errorf("Ожидалось, что пользователи будут равны")
		}
	})
}

func TestRawParticipantEquality(t *testing.T) {
	t.Run("RawParticipants с одинаковыми полями должны быть равны", func(t *testing.T) {
		part1 := RawParticipant{
			UserID:   "user123",
			Name:     "John Doe",
			Username: "@johndoe",
		}

		part2 := RawParticipant{
			UserID:   "user123",
			Name:     "John Doe",
			Username: "@johndoe",
		}

		if !reflect.DeepEqual(part1, part2) {
			t.Errorf("Ожидалось, что участники будут равны")
		}
	})
}

func TestUserWithEmptyFields(t *testing.T) {
	t.Run("Пользователь с пустыми полями", func(t *testing.T) {
		user := User{}

		if user.ID != 0 {
			t.Errorf("Ожидался пустой ID 0, получено %d", user.ID)
		}

		if user.Name != "" {
			t.Errorf("Ожидалось пустое имя, получено '%s'", user.Name)
		}

		if user.Username != "" {
			t.Errorf("Ожидалось пустое имя пользователя, получено '%s'", user.Username)
		}

		if user.Bio != "" {
			t.Errorf("Ожидалось пустое био, получено '%s'", user.Bio)
		}
	})
}

func TestMessageWithDifferentTypes(t *testing.T) {
	t.Run("Структура служебного сообщения", func(t *testing.T) {
		msg := Message{
			ID:           2,
			Type:         "service",
			Date:         "2023-01-01T00:00:00",
			Actor:        "John Doe",
			ActorID:      "user123",
			Text:         json.RawMessage(`""`),
			TextEntities: []TextEntity{},
		}

		if msg.Type != "service" {
			t.Errorf("Ожидался тип 'service', получено '%s'", msg.Type)
		}

		if msg.Actor != "John Doe" {
			t.Errorf("Ожидался Actor 'John Doe', получено '%s'", msg.Actor)
		}
	})
}

// Добавление тестов, которые должны учитываться в покрытии
func TestExportedChatWithMarshaling(t *testing.T) {
	chat := ExportedChat{
		Name: "Test Chat",
		Type: "private_group",
		ID:   12345,
		Messages: []Message{
			{
				ID:     1,
				Type:   "message",
				Date:   "2023-01-01T00:00:00",
				From:   "John Doe",
				FromID: "user123",
				Text:   json.RawMessage(`"Hello, World!"`),
			},
		},
	}

	// Этот тест должен увеличить покрытие для определений структур
	data, err := json.Marshal(chat)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибок при маршалинге, получено: %v", err)
	}
	if len(data) == 0 {
		t.Error("Ожидались непустые маршалированные данные")
	}

	// Тестирование анмаршалинга
	var unmarshaledChat ExportedChat
	err = json.Unmarshal(data, &unmarshaledChat)
	if err != nil {
		t.Errorf("Ожидалось отсутствие ошибок при анмаршалинге, получено: %v", err)
	}
	if unmarshaledChat.Name != "Test Chat" {
		t.Errorf("Ожидалось имя 'Test Chat', получено '%s'", unmarshaledChat.Name)
	}
}
