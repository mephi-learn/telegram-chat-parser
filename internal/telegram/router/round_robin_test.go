package router

import (
	"telegram-chat-parser/internal/ports"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Тесты для RoundRobinStrategy ---

func TestRoundRobinStrategy(t *testing.T) {
	clients := []ports.TelegramClient{
		newMockClient("client-1", true),
		newMockClient("client-2", true),
		newMockClient("client-3", true),
	}

	strategy := NewRoundRobinStrategy()

	// Проверяем, что клиенты выбираются по кругу.
	c1, err := strategy.Next(clients)
	require.NoError(t, err)
	require.Equal(t, "client-1", c1.ID())

	c2, err := strategy.Next(clients)
	require.NoError(t, err)
	require.Equal(t, "client-2", c2.ID())

	c3, err := strategy.Next(clients)
	require.NoError(t, err)
	require.Equal(t, "client-3", c3.ID())

	c4, err := strategy.Next(clients)
	require.NoError(t, err)
	require.Equal(t, "client-1", c4.ID()) // Возвращаемся к первому.
}

func TestRoundRobinStrategy_NoClients(t *testing.T) {
	strategy := NewRoundRobinStrategy()
	_, err := strategy.Next([]ports.TelegramClient{})
	require.ErrorIs(t, err, ErrNoHealthyClients)
}
