package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestTokenMaskerHandler_Handle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mask telegram token in message",
			input:    `Post "https://api.telegram.org/bot8462697481:AAEJSXuTcb2F1Js2sWiK0TVWvxbHL9xX05Q/getUpdates": net/http: request canceled`,
			expected: `Post "https://api.telegram.org/bot***:***masked-token***/getUpdates": net/http: request canceled`,
		},
		{
			name:     "no token in message",
			input:    "This is a normal log message without tokens",
			expected: "This is a normal log message without tokens",
		},
		{
			name:     "multiple tokens in message",
			input:    "Token1: bot123456789:AAABCdEfGhIjKlMnOpQrStUvWxYz1234567, Token2: bot987654321:AAzZzYyXxWwVvUuTtSsRrQqPpOnNmLlKkJjI",
			expected: "Token1: bot***:***masked-token***, Token2: bot***:***masked-token***",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel() // Добавляем параллельное выполнение для выявления гонок
			var buf bytes.Buffer
			originalHandler := slog.NewJSONHandler(&buf, nil)
			maskerHandler := NewTokenMaskerHandler(originalHandler)

			logger := slog.New(maskerHandler)

			logger.Info(tt.input)

			output := buf.String()
			expectedEscaped := strings.ReplaceAll(tt.expected, "\"", "\\\"")
			if !strings.Contains(output, expectedEscaped) {
				t.Errorf("expected output to contain %q, got %q", expectedEscaped, output)
			}
		})
	}
}

func TestTokenMaskerHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	originalHandler := slog.NewJSONHandler(&buf, nil)
	maskerHandler := NewTokenMaskerHandler(originalHandler)

	logger := slog.New(maskerHandler)

	token := "bot8462697481:AAEJSXuTcb2F1Js2sWiK0TVWvxbHL9xX05Q"
	logger = logger.With(slog.String("token", token))

	logger.Info("message with token in attr")

	output := buf.String()
	if strings.Contains(output, token) {
		t.Errorf("expected output to not contain original token %q, but it did", token)
	}
	if !strings.Contains(output, "***masked-token***") {
		t.Errorf("expected output to contain masked token, got %q", output)
	}
}

func TestMaskTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `Post "https://api.telegram.org/bot8462697481:AAEJSXuTcb2F1Js2sWiK0TVWvxbHL9xX05Q/getUpdates"`,
			expected: `Post "https://api.telegram.org/bot***:***masked-token***/getUpdates"`,
		},
		{
			input:    "No token here",
			expected: "No token here",
		},
		{
			input:    "bot123456789:AAABCdEfGhIjKlMnOpQrStUvWxYz1234567",
			expected: "bot***:***masked-token***",
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := maskTokens(tt.input)
			if result != tt.expected {
				t.Errorf("maskTokens(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
