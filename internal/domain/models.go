package domain

import "encoding/json"

// ExportedChat представляет корневую структуру файла экспорта.
type ExportedChat struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	ID       int       `json:"id"`
	Messages []Message `json:"messages"`
}

// Message представляет одно сообщение в чате.
type Message struct {
	ID           int             `json:"id"`
	Type         string          `json:"type"`
	Date         string          `json:"date"`
	From         string          `json:"from"`
	FromID       string          `json:"from_id"`
	Actor        string          `json:"actor"`
	ActorID      string          `json:"actor_id"`
	Text         json.RawMessage `json:"text"` // Может быть строкой или массивом
	TextEntities []TextEntity    `json:"text_entities"`
}

// TextEntity представляет "богатую" часть текста (упоминание, ссылка и т.д.).
type TextEntity struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// User представляет участника чата.
// Это наша внутренняя модель, а не структура из JSON.
type User struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Bio      string `json:"bio"`
	Channel  string `json:"channel,omitempty"`
}

// RawParticipant представляет "сырые" данные об участнике, извлеченные из файла,
// до обогащения через API.
type RawParticipant struct {
	// ID пользователя, если он известен (например, 'user12345').
	// Для упоминаний это поле будет пустым.
	UserID string
	// Отображаемое имя, если известно.
	Name string
	// Имя пользователя для упоминаний (например, '@username').
	Username string
}
