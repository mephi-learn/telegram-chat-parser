package log

import (
	"fmt"
	"log/slog"
	"strings"
)

// TGBotAPIAdapter адаптирует slog.Logger под интерфейс логгера,
// который ожидает библиотека go-telegram-bot-api/v5.
type TGBotAPIAdapter struct {
	Logger *slog.Logger
}

// Println реализует метод интерфейса tgbotapi.Logger.
func (a *TGBotAPIAdapter) Println(v ...interface{}) {
	// Сообщения от библиотеки считаем информационными.
	// Они будут проходить через основной маскировщик.
	a.Logger.Info(strings.TrimSpace(fmt.Sprintln(v...)))
}

// Printf реализует метод интерфейса tgbotapi.Logger.
func (a *TGBotAPIAdapter) Printf(format string, v ...interface{}) {
	a.Logger.Info(strings.TrimSpace(fmt.Sprintf(format, v...)))
}
