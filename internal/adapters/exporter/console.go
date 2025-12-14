package exporter

import (
	"fmt"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"
)

// ConsoleExporter реализует интерфейс Exporter для вывода данных в консоль.
type ConsoleExporter struct{}

// NewConsoleExporter создает новый экземпляр ConsoleExporter.
func NewConsoleExporter() ports.Exporter {
	return &ConsoleExporter{}
}

// Export выводит финальный список пользователей в консоль.
func (e *ConsoleExporter) Export(users []domain.User) error {
	fmt.Println("--- Chat Participants ---")
	if len(users) == 0 {
		fmt.Println("No participants found.")
	} else {
		for i, user := range users {
			if user.Username != "" {
				if user.Bio != "" {
					fmt.Printf("%d. Name: %s, Username: @%s, ID: %d, Bio: %s\n", i+1, user.Name, user.Username, user.ID, user.Bio)
				} else {
					fmt.Printf("%d. Name: %s, Username: @%s, ID: %d\n", i+1, user.Name, user.Username, user.ID)
				}
			} else {
				fmt.Printf("%d. Name: %s, ID: %d\n", i+1, user.Name, user.ID)
			}
		}
	}
	return nil
}
