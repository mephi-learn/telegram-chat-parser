package services

import (
	"strings"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// ExtractionServiceImpl реализует интерфейс ExtractionService.
type ExtractionServiceImpl struct{}

// NewExtractionService создает новый экземпляр ExtractionServiceImpl.
func NewExtractionService() ports.ExtractionService {
	return &ExtractionServiceImpl{}
}

// ExtractRawParticipants извлекает "сырой" список авторов и упоминаний из чата.
func (s *ExtractionServiceImpl) ExtractRawParticipants(chat *domain.ExportedChat) ([]domain.RawParticipant, error) {
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
