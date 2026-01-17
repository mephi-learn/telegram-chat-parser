package ports

import (
	"context"
	"telegram-chat-parser/internal/domain"
)

// DataSource определяет интерфейс для получения исходных данных чата.
type DataSource interface {
	// Fetch загружает данные из источника и возвращает их в виде байтового среза.
	Fetch() ([]byte, error)
}

// Parser определяет интерфейс для парсинга данных чата.
type Parser interface {
	// Parse преобразует сырые данные в "сырой" список участников.
	Parse(data []byte) ([]domain.RawParticipant, error)
}

// EnrichmentService определяет интерфейс для обогащения данных об участниках
// с помощью Telegram API.
type EnrichmentService interface {
	Enrich(ctx context.Context, participants []domain.RawParticipant) ([]domain.User, error)
}

// Exporter определяет интерфейс для вывода результата.
type Exporter interface {
	// Export принимает финальный список пользователей и выводит их.
	Export(users []domain.User) error
}

// ParserFactory определяет интерфейс для фабрики парсеров.
// Это позволяет абстрагироваться от конкретной реализации фабрики при тестировании.
type ParserFactory interface {
	GetParser(fileName string) (Parser, error)
	GetParserForData(data []byte) (Parser, error)
}
