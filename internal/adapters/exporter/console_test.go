package exporter

import (
	"bytes"
	"os"
	"strings"
	"telegram-chat-parser/internal/domain"
	"testing"
)

func TestConsoleExporter(t *testing.T) {
	t.Run("NewConsoleExporter создает корректный экземпляр", func(t *testing.T) {
		exporter := NewConsoleExporter()
		if exporter == nil {
			t.Error("Ожидался экземпляр ConsoleExporter, получен nil")
		}
	})

	t.Run("Export корректно выводит пользователей", func(t *testing.T) {
		// Перехватываем stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		exporter := &ConsoleExporter{}
		users := []domain.User{
			{
				ID:       123,
				Name:     "John Doe",
				Username: "@johndoe",
				Bio:      "Software Developer",
			},
			{
				ID:       456,
				Name:     "Jane Smith",
				Username: "@janesmith",
			},
			{
				ID:   789,
				Name: "Bob Johnson",
			},
		}

		err := exporter.Export(users)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		if !strings.Contains(output, "--- Chat Participants ---") {
			t.Error("Ожидался заголовок в выводе")
		}

		if !strings.Contains(output, "John Doe") {
			t.Error("Ожидалось 'John Doe' в выводе")
		}

		if !strings.Contains(output, "@@johndoe") {
			t.Error("Ожидалось '@@johndoe' в выводе")
		}

		if !strings.Contains(output, "Software Developer") {
			t.Error("Ожидалось 'Software Developer' в выводе")
		}

		if !strings.Contains(output, "Jane Smith") {
			t.Error("Ожидалось 'Jane Smith' в выводе")
		}

		if !strings.Contains(output, "Bob Johnson") {
			t.Error("Ожидалось 'Bob Johnson' в выводе")
		}
	})

	t.Run("Export выводит сообщение при отсутствии пользователей", func(t *testing.T) {
		// Перехватываем stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		exporter := &ConsoleExporter{}
		users := []domain.User{}

		err := exporter.Export(users)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		if !strings.Contains(output, "--- Chat Participants ---") {
			t.Error("Ожидался заголовок в выводе")
		}

		if !strings.Contains(output, "No participants found.") {
			t.Error("Ожидалось 'No participants found.' в выводе")
		}
	})

	t.Run("Export обрабатывает пользователей с различными комбинациями полей", func(t *testing.T) {
		// Перехватываем stdout
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		exporter := &ConsoleExporter{}
		users := []domain.User{
			{
				ID:       123,
				Name:     "User With All Fields",
				Username: "@user123",
				Bio:      "This is a bio",
			},
			{
				ID:       456,
				Name:     "User Without Bio",
				Username: "@user456",
			},
			{
				ID:   789,
				Name: "User Without Username",
			},
			{
				ID:       999,
				Name:     "User With Empty Bio",
				Username: "@user999",
				Bio:      "",
			},
		}

		err := exporter.Export(users)
		if err != nil {
			t.Errorf("Неожиданная ошибка: %v", err)
		}

		w.Close()
		os.Stdout = old

		var buf bytes.Buffer
		buf.ReadFrom(r)
		output := buf.String()

		// Проверяем, что все пользователи отображаются в выводе с правильным форматированием
		// Примечание: экспортер добавляет @ к именам пользователей, поэтому "@user123" становится "@@user123"
		expectedOutputs := []string{
			"User With All Fields, Username: @@user123, ID: 123, Bio: This is a bio",
			"User Without Bio, Username: @@user456, ID: 456",
			"User Without Username, ID: 789",
			"User With Empty Bio, Username: @@user999, ID: 999",
		}

		for _, expected := range expectedOutputs {
			if !strings.Contains(output, expected) {
				t.Errorf("Ожидалось '%s' в выводе", expected)
			}
		}
	})

	t.Run("Export возвращает nil в качестве ошибки", func(t *testing.T) {
		exporter := &ConsoleExporter{}
		users := []domain.User{
			{
				ID:       123,
				Name:     "Test User",
				Username: "@testuser",
			},
		}

		err := exporter.Export(users)
		if err != nil {
			t.Errorf("Ожидалась ошибка nil, получено %v", err)
		}
	})
}
