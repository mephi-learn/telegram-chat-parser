package router

import (
	"sync/atomic"
	"telegram-chat-parser/internal/ports"
)

// RoundRobinStrategy реализует стратегию выбора "по кругу" (Round Robin).
type RoundRobinStrategy struct {
	// currentIndex хранит индекс последнего выбранного клиента.
	// Используется atomic для потокобезопасного инкремента.
	currentIndex uint32
}

// NewRoundRobinStrategy создает новую Round Robin стратегию.
func NewRoundRobinStrategy() *RoundRobinStrategy {
	return &RoundRobinStrategy{}
}

// Next возвращает следующего клиента в списке, инкрементируя индекс по кругу.
func (s *RoundRobinStrategy) Next(clients []ports.TelegramClient) (ports.TelegramClient, error) {
	if len(clients) == 0 {
		return nil, ErrNoHealthyClients
	}
	// Атомарно увеличиваем счетчик и получаем индекс.
	// Вычитаем 1, чтобы получить текущий индекс до увеличения.
	idx := atomic.AddUint32(&s.currentIndex, 1) - 1
	// Берем клиента по модулю длины среза, чтобы обеспечить цикличность.
	return clients[idx%uint32(len(clients))], nil
}
