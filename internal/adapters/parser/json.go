package parser

import (
	"encoding/json"
	"fmt"
	"strings"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// JsonParser реализует интерфейс Parser для разбора JSON данных.
type JsonParser struct{}

// NewJsonParser создает новый экземпляр JsonParser.
func NewJsonParser() ports.Parser {
	return &JsonParser{}
}

// Parse преобразует срез байт с JSON в "сырой" список участников.
func (p *JsonParser) Parse(data []byte) ([]domain.RawParticipant, error) {
	var chat domain.ExportedChat
	if err := json.Unmarshal(data, &chat); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	var rawParticipants []domain.RawParticipant
	// Мапы для отслеживания уникальных участников
	uniqueUsers := make(map[string]bool)    // для отслеживания user ID
	uniqueMentions := make(map[string]bool) // для отслеживания username упоминаний

	for _, msg := range chat.Messages {
		entityID := msg.FromID
		entityName := msg.From
		if msg.Type == "service" {
			entityID = msg.ActorID
			entityName = msg.Actor
		}

		// Добавляем только пользователей-авторов
		if strings.HasPrefix(entityID, "user") && entityName != "" && entityName != "Deleted Account" {
			if !uniqueUsers[entityID] {
				uniqueUsers[entityID] = true
				rawParticipants = append(rawParticipants, domain.RawParticipant{
					UserID: entityID,
					Name:   entityName,
				})
			}
		}

		// Добавляем упоминания
		for _, entity := range msg.TextEntities {
			if entity.Type == "mention" {
				username := entity.Text
				if !uniqueMentions[username] {
					uniqueMentions[username] = true
					rawParticipants = append(rawParticipants, domain.RawParticipant{
						Username: username,
					})
				}
			}
		}
	}

	return rawParticipants, nil
}
