package source

import (
	"fmt"
	"telegram-chat-parser/internal/ports"
)

// MemorySource реализует интерфейс DataSource для чтения данных из памяти.
type MemorySource struct {
	data []byte
}

// NewMemorySource создает новый экземпляр MemorySource.
func NewMemorySource(data []byte) ports.DataSource {
	return &MemorySource{data: data}
}

// Fetch возвращает данные из памяти.
func (s *MemorySource) Fetch() ([]byte, error) {
	if s.data == nil {
		return nil, fmt.Errorf("данные не установлены")
	}

	// Возвращаем копию данных, чтобы избежать изменений оригинальных данных
	dataCopy := make([]byte, len(s.data))
	copy(dataCopy, s.data)

	return dataCopy, nil
}
