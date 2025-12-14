package parser

import (
	"encoding/json"
	"fmt"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// JsonParser реализует интерфейс Parser для разбора JSON данных.
type JsonParser struct{}

// NewJsonParser создает новый экземпляр JsonParser.
func NewJsonParser() ports.Parser {
	return &JsonParser{}
}

// Parse преобразует срез байт с JSON в структуру ExportedChat.
func (p *JsonParser) Parse(data []byte) (*domain.ExportedChat, error) {
	var chat domain.ExportedChat
	if err := json.Unmarshal(data, &chat); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}
	return &chat, nil
}
