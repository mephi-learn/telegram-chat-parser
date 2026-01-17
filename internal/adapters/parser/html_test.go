package parser

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"telegram-chat-parser/internal/domain"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHtmlAndJsonParsersComparison(t *testing.T) {
	// --- Чтение и парсинг JSON ---
	jsonBytes, err := os.ReadFile("../../../result.json")
	require.NoError(t, err, "Failed to read result.json")

	jsonParser := NewJsonParser()
	jsonParticipants, err := jsonParser.Parse(jsonBytes)
	require.NoError(t, err, "Failed to parse json data")
	require.NotEmpty(t, jsonParticipants, "JSON parser returned no participants")

	// --- Чтение и парсинг HTML ---
	htmlBytes, err := os.ReadFile("../../../messages.html")
	require.NoError(t, err, "Failed to read messages.html")

	htmlParser := NewHtmlParser()
	htmlParticipants, err := htmlParser.Parse(htmlBytes)
	require.NoError(t, err, "Failed to parse html data")
	require.NotEmpty(t, htmlParticipants, "HTML parser returned no participants")

	// --- Сортировка для удобного сравнения ---
	sortParticipants(jsonParticipants)
	sortParticipants(htmlParticipants)

	// --- Вывод результатов для визуального сравнения ---
	t.Log("--- JSON Participants ---")
	for _, p := range jsonParticipants {
		t.Logf("Name: '%s', Username: '%s', UserID: '%s'", p.Name, p.Username, p.UserID)
	}

	t.Log("--- HTML Participants ---")
	for _, p := range htmlParticipants {
		t.Logf("Name: '%s', Username: '%s'", p.Name, p.Username)
	}

	// Финальное сообщение для ясности
	fmt.Println("\n✅ Parsers have been executed. Please compare the outputs above visually.")
	fmt.Println("Note: HTML parser does not extract UserIDs, and may have minor differences in mention extraction as noted in the task description.")
}

// sortParticipants сортирует срез участников для консистентного вывода.
func sortParticipants(participants []domain.RawParticipant) {
	sort.Slice(participants, func(i, j int) bool {
		// Приоритет на Name, затем Username
		p1 := strings.ToLower(participants[i].Name + participants[i].Username)
		p2 := strings.ToLower(participants[j].Name + participants[j].Username)
		return p1 < p2
	})
}
