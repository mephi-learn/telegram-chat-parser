package log

import (
	"context"
	"log/slog"
	"regexp"
)

// TokenMaskerHandler - обертка для slog.Handler, которая маскирует токены в логах
type TokenMaskerHandler struct {
	handler slog.Handler
}

// NewTokenMaskerHandler создает новый обработчик с маскировкой токенов
func NewTokenMaskerHandler(handler slog.Handler) *TokenMaskerHandler {
	return &TokenMaskerHandler{
		handler: handler,
	}
}

// маскируем токены в формате botID:token, где ID - числа, token - буквенно-цифровой
var telegramTokenRegex = regexp.MustCompile(`(\bbot\d+:[A-Za-z0-9_-]{35,})`)

// maskTokens заменяет найденные токены на маску
func maskTokens(text string) string {
	return telegramTokenRegex.ReplaceAllString(text, "bot***:***masked-token***")
}

// Enabled реализует интерфейс slog.Handler
func (h *TokenMaskerHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle реализует интерфейс slog.Handler
func (h *TokenMaskerHandler) Handle(ctx context.Context, record slog.Record) error {
	// Создаем полную, изолированную копию записи.
	// Это предотвращает гонку данных, так как мы больше не работаем
	// с оригинальной записью, которую slog может переиспользовать.
	// Метод Clone() также обнуляет атрибуты в копии, поэтому их нужно добавить заново.
	r := record.Clone()

	// Маскируем основное сообщение.
	r.Message = maskTokens(r.Message)

	// Итерируемся по атрибутам оригинальной записи и добавляем их маскированные версии в клон.
	record.Attrs(func(a slog.Attr) bool {
		r.AddAttrs(slog.Attr{
			Key:   a.Key,
			Value: maskAttributeValue(a.Value),
		})
		return true
	})

	return h.handler.Handle(ctx, r)
}

// WithAttrs реализует интерфейс slog.Handler
func (h *TokenMaskerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	maskedAttrs := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		maskedAttrs[i] = slog.Attr{
			Key:   attr.Key,
			Value: maskAttributeValue(attr.Value),
		}
	}
	return &TokenMaskerHandler{
		handler: h.handler.WithAttrs(maskedAttrs),
	}
}

// WithGroup реализует интерфейс slog.Handler
func (h *TokenMaskerHandler) WithGroup(name string) slog.Handler {
	return &TokenMaskerHandler{
		handler: h.handler.WithGroup(name),
	}
}

// maskAttributeValue рекурсивно маскирует значения атрибутов
func maskAttributeValue(value slog.Value) slog.Value {
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(maskTokens(value.String()))
	case slog.KindAny:
		// Это основной фикс: мы проверяем, не является ли значение ошибкой.
		// Если да, то преобразуем ошибку в строку и маскируем ее.
		if err, ok := value.Any().(error); ok {
			return slog.StringValue(maskTokens(err.Error()))
		}
		return value
	case slog.KindGroup:
		group := value.Group()
		maskedGroup := make([]slog.Attr, len(group))
		for i, attr := range group {
			maskedGroup[i] = slog.Attr{
				Key:   attr.Key,
				Value: maskAttributeValue(attr.Value),
			}
		}
		return slog.GroupValue(maskedGroup...)
	default:
		// Для других типов возвращаем оригинальное значение
		return value
	}
}

// NewMaskedLogger создает новый экземпляр slog.Logger с маскировкой токенов
func NewMaskedLogger(handler slog.Handler) *slog.Logger {
	return slog.New(NewTokenMaskerHandler(handler))
}
