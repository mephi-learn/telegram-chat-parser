package ratelimiter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	testDelay            = 100 * time.Millisecond
	testCooldownDuration = 200 * time.Millisecond
	testRetries          = 2
)

func TestNewAdaptiveRateLimiter(t *testing.T) {
	rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)
	assert.NotNil(t, rl)
	assert.Equal(t, StateNormal, rl.GetState())
	assert.Equal(t, testDelay, rl.requestDelay)
	assert.Equal(t, testRetries, rl.throttledRetries)
	assert.Equal(t, testCooldownDuration, rl.cooldownDuration)
}

func TestAdaptiveRateLimiter_StateTransitions(t *testing.T) {
	rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)

	// 1. Normal -> Throttled (Обычный -> Ограничение)
	assert.Equal(t, StateNormal, rl.GetState())
	rl.ReportError(errors.New("flood wait"))
	assert.Equal(t, StateThrottled, rl.GetState())
	assert.Equal(t, 0, rl.currentRetries)

	// 2. Throttled -> Throttled (увеличение счетчика повторов)
	rl.ReportError(errors.New("flood wait"))
	assert.Equal(t, StateThrottled, rl.GetState())
	assert.Equal(t, 1, rl.currentRetries)

	// 3. Throttled -> Cooldown (Ограничение -> Охлаждение)
	rl.ReportError(errors.New("flood wait")) // Должно достигнуть максимального количества повторов
	assert.Equal(t, StateCooldown, rl.GetState())
	assert.True(t, rl.IsInCooldown())

	// 4. Ошибки в состоянии Cooldown игнорируются
	rl.ReportError(errors.New("another error"))
	assert.Equal(t, StateCooldown, rl.GetState())

	// 5. Cooldown -> Normal (после таймера)
	time.Sleep(testCooldownDuration + 50*time.Millisecond)
	assert.Equal(t, StateNormal, rl.GetState())
	assert.False(t, rl.IsInCooldown())
}

func TestAdaptiveRateLimiter_Acquire(t *testing.T) {
	t.Run("Acquire в состоянии Normal", func(t *testing.T) {
		rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)
		start := time.Now()
		err := rl.Acquire(context.Background())
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Less(t, duration, testDelay, "Задержка должна быть близка к нулю в состоянии Normal")
	})

	t.Run("Acquire в состоянии Throttled", func(t *testing.T) {
		rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)
		rl.ReportError(errors.New("flood wait")) // Переход в Throttled

		start := time.Now()
		err := rl.Acquire(context.Background())
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, duration, testDelay, "Должна быть задержка в состоянии Throttled")
	})

	t.Run("Acquire в состоянии Cooldown", func(t *testing.T) {
		rl := NewAdaptiveRateLimiter(testDelay, 1, testCooldownDuration)
		rl.ReportError(errors.New("err 1")) // в throttled
		rl.ReportError(errors.New("err 2")) // в cooldown

		start := time.Now()
		err := rl.Acquire(context.Background())
		duration := time.Since(start)

		assert.NoError(t, err)
		assert.Less(t, duration, testDelay, "Задержка должна быть равна нулю в состоянии Cooldown")
	})

	t.Run("Acquire с отмененным контекстом", func(t *testing.T) {
		rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Отменяем немедленно

		err := rl.Acquire(ctx)
		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestAdaptiveRateLimiter_GetState(t *testing.T) {
	rl := NewAdaptiveRateLimiter(testDelay, testRetries, testCooldownDuration)
	assert.Equal(t, StateNormal, rl.GetState())

	rl.state = StateThrottled
	assert.Equal(t, StateThrottled, rl.GetState())
}
