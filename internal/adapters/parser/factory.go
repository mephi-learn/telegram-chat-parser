package parser

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"telegram-chat-parser/internal/ports"
)

// Factory — фабрика, создающая парсеры в зависимости от типа файла.
type Factory struct {
	jsonParser ports.Parser
	htmlParser ports.Parser
}

// NewFactory создает новую фабрику парсеров.
func NewFactory() *Factory {
	return &Factory{
		jsonParser: NewJsonParser(),
		htmlParser: NewHtmlParser(),
	}
}

// GetParser возвращает парсер на основе расширения файла.
func (f *Factory) GetParser(fileName string) (ports.Parser, error) {
	extension := strings.ToLower(filepath.Ext(fileName))
	switch extension {
	case ".json":
		return f.jsonParser, nil
	case ".html", ".htm":
		return f.htmlParser, nil
	default:
		return nil, fmt.Errorf("no parser found for file type: %s", extension)
	}
}

// GetParserForData определяет тип контента и возвращает подходящий парсер.
// Это упрощенная реализация для обработки данных из памяти.
func (f *Factory) GetParserForData(data []byte) (ports.Parser, error) {
	trimmedData := bytes.TrimSpace(data)
	if len(trimmedData) == 0 {
		return nil, fmt.Errorf("empty data, cannot determine parser")
	}

	firstChar := trimmedData[0]
	// JSON обычно начинается с '{' (объект) или '[' (массив).
	if firstChar == '{' || firstChar == '[' {
		return f.jsonParser, nil
	}
	// HTML может начинаться с '<' (тег).
	if bytes.HasPrefix(trimmedData, []byte("<")) {
		return f.htmlParser, nil
	}

	return nil, fmt.Errorf("could not determine parser for data content")
}
