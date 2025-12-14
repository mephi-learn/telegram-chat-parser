package source

import (
	"fmt"
	"os"
	"telegram-chat-parser/internal/ports"
)

// CliSource реализует интерфейс DataSource для чтения данных из файла,
// указанного в командной строке.
type CliSource struct {
	filePath string
}

// NewCliSource создает новый экземпляр CliSource.
func NewCliSource(filePath string) ports.DataSource {
	return &CliSource{filePath: filePath}
}

// Fetch читает файл по указанному пути и возвращает его содержимое.
func (s *CliSource) Fetch() ([]byte, error) {
	if s.filePath == "" {
		return nil, fmt.Errorf("не указан путь к файлу")
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", s.filePath, err)
	}

	return data, nil
}
