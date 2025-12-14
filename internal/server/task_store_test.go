package server

import (
	"context"
	"telegram-chat-parser/internal/domain"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskStore(t *testing.T) {
	t.Run("NewTaskStore", func(t *testing.T) {
		ts := NewTaskStore()
		assert.NotNil(t, ts)
		assert.NotNil(t, ts.tasks)
	})

	t.Run("CreateAndGetTask", func(t *testing.T) {
		ts := NewTaskStore()
		taskID := "task-1"
		ttl := 5 * time.Minute

		ts.CreateTask(taskID, ttl)

		task, err := ts.GetTask(taskID)
		require.NoError(t, err)
		require.NotNil(t, task)

		assert.Equal(t, taskID, task.ID)
		assert.Equal(t, TaskStatusPending, task.Status)
		assert.WithinDuration(t, time.Now().Add(ttl), task.ExpiresAt, time.Second)
	})

	t.Run("GetNonExistentTask", func(t *testing.T) {
		ts := NewTaskStore()
		_, err := ts.GetTask("non-existent")
		assert.Error(t, err)
	})

	t.Run("UpdateTaskStatus", func(t *testing.T) {
		ts := NewTaskStore()
		taskID := "task-1"
		ts.CreateTask(taskID, time.Minute)

		err := ts.UpdateTaskStatus(taskID, TaskStatusProcessing)
		require.NoError(t, err)

		task, _ := ts.GetTask(taskID)
		assert.Equal(t, TaskStatusProcessing, task.Status)

		err = ts.UpdateTaskStatus("non-existent", TaskStatusCompleted)
		assert.Error(t, err)
	})

	t.Run("UpdateTaskResult", func(t *testing.T) {
		ts := NewTaskStore()
		taskID := "task-1"
		ts.CreateTask(taskID, time.Minute)

		result := []domain.User{{ID: 1, Name: "User"}}
		err := ts.UpdateTaskResult(taskID, result)
		require.NoError(t, err)

		task, _ := ts.GetTask(taskID)
		assert.Equal(t, TaskStatusCompleted, task.Status)
		assert.Equal(t, result, task.Result)

		err = ts.UpdateTaskResult("non-existent", nil)
		assert.Error(t, err)
	})

	t.Run("UpdateTaskError", func(t *testing.T) {
		ts := NewTaskStore()
		taskID := "task-1"
		ts.CreateTask(taskID, time.Minute)

		errMsg := "something went wrong"
		err := ts.UpdateTaskError(taskID, errMsg)
		require.NoError(t, err)

		task, _ := ts.GetTask(taskID)
		assert.Equal(t, TaskStatusFailed, task.Status)
		assert.Equal(t, errMsg, task.ErrorMessage)

		err = ts.UpdateTaskError("non-existent", "")
		assert.Error(t, err)
	})

	t.Run("CleanupExpired", func(t *testing.T) {
		ts := NewTaskStore()
		expiredTaskID := "expired"
		validTaskID := "valid"

		ts.CreateTask(expiredTaskID, -1*time.Minute) // expired
		ts.CreateTask(validTaskID, 1*time.Minute)    // valid

		ts.CleanupExpired()

		_, err := ts.GetTask(expiredTaskID)
		assert.Error(t, err, "Expired task should be deleted")

		_, err = ts.GetTask(validTaskID)
		assert.NoError(t, err, "Valid task should not be deleted")
	})
}

func TestTaskStore_StartCleanupTicker(t *testing.T) {
	ts := NewTaskStore()
	expiredTaskID := "expired"
	ts.CreateTask(expiredTaskID, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ts.StartCleanupTicker(ctx, 100*time.Millisecond)

	time.Sleep(150 * time.Millisecond) // Wait for ticker to run

	_, err := ts.GetTask(expiredTaskID)
	assert.Error(t, err, "Expired task should be removed by ticker")

	// Check if the goroutine stops
	cancel()
	time.Sleep(50 * time.Millisecond)
}
