package ratelimiter

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// State представляет состояние ограничителя частоты
type State string

const (
	StateNormal    State = "normal"
	StateThrottled State = "throttled"
	StateCooldown  State = "cooldown"
)

// AdaptiveRateLimiter реализует адаптивный механизм ограничения частоты
type AdaptiveRateLimiter struct {
	state            State
	stateMutex       sync.Mutex
	requestDelay     time.Duration
	throttledRetries int
	cooldownDuration time.Duration

	currentRetries int
}

// NewAdaptiveRateLimiter создает новый экземпляр AdaptiveRateLimiter
func NewAdaptiveRateLimiter(delay time.Duration, throttledRetries int, cooldownDuration time.Duration) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		state:            StateNormal,
		requestDelay:     delay,
		throttledRetries: throttledRetries,
		cooldownDuration: cooldownDuration,
	}
}

// Acquire ожидает разрешения на выполнение запроса в зависимости от текущего состояния
func (rl *AdaptiveRateLimiter) Acquire(ctx context.Context) error {
	rl.stateMutex.Lock()
	defer rl.stateMutex.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	delay := rl.calculateDelay()
	if delay > 0 {
		time.Sleep(delay)
	}

	return nil
}

// ReportError сообщает об ошибке (например, ожидании флуда) ограничителю частоты
func (rl *AdaptiveRateLimiter) ReportError(err error) {
	rl.stateMutex.Lock()
	defer rl.stateMutex.Unlock()

	slog.Debug("Ограничитель частоты: ошибка", "error", err, "current_state", rl.state)

	if rl.state == StateCooldown {
		// Игнорировать ошибки во время охлаждения
		return
	}

	// Переход в состояние ограничения, если еще не в нем
	if rl.state != StateThrottled {
		rl.state = StateThrottled
		rl.currentRetries = 0
		slog.Info("Ограничитель частоты: переход в состояние THROTTLED", "delay", rl.requestDelay)
		return
	}

	// Увеличение числа повторных попыток в состоянии ограничения
	rl.currentRetries++
	slog.Debug("Ограничитель частоты: увеличение числа повторных попыток", "retries", rl.currentRetries, "max_retries", rl.throttledRetries)

	if rl.currentRetries >= rl.throttledRetries {
		// Переход в состояние охлаждения
		rl.state = StateCooldown
		rl.currentRetries = 0
		slog.Info("Ограничитель частоты: переход в состояние COOLDOWN", "duration", rl.cooldownDuration)

		// Запуск горутины для возврата к нормальному состоянию после охлаждения
		go rl.startCooldownTimer()
	}
}

// startCooldownTimer запускает таймер для возврата к нормальному состоянию после периода охлаждения
func (rl *AdaptiveRateLimiter) startCooldownTimer() {
	time.Sleep(rl.cooldownDuration)
	rl.stateMutex.Lock()
	defer rl.stateMutex.Unlock()

	rl.state = StateNormal
	rl.currentRetries = 0
	slog.Info("Ограничитель частоты: возврат к нормальному состоянию после охлаждения")
}

// calculateDelay вычисляет задержку на основе текущего состояния
func (rl *AdaptiveRateLimiter) calculateDelay() time.Duration {
	switch rl.state {
	case StateThrottled:
		return rl.requestDelay
	case StateCooldown:
		// Потенциально добавить небольшую задержку даже во время охлаждения, если запросы продолжают поступать
		// Или вернуть 0 и полагаться на внешнюю логику для предотвращения запросов во время охлаждения.
		// Пока возвращаем 0 и ожидаем, что внешняя логика справится с этим.
		return 0
	default: // StateNormal
		return 0
	}
}

// IsInCooldown возвращает true, если ограничитель частоты в настоящее время находится в состоянии охлаждения
func (rl *AdaptiveRateLimiter) IsInCooldown() bool {
	rl.stateMutex.Lock()
	defer rl.stateMutex.Unlock()
	return rl.state == StateCooldown
}

// GetState возвращает текущее состояние ограничителя частоты
func (rl *AdaptiveRateLimiter) GetState() State {
	rl.stateMutex.Lock()
	defer rl.stateMutex.Unlock()
	return rl.state
}
