package server

import (
	"context"
	"fmt"
	"sync"
	"telegram-chat-parser/internal/domain"
	"time"
)

// TaskStatus представляет статус задачи обработки
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// Task представляет собой одну задачу обработки
type Task struct {
	ID           string
	Status       TaskStatus
	Result       []domain.User
	ErrorMessage string
	CreatedAt    time.Time
	ExpiresAt    time.Time // Для автоматической очистки
}

// TaskStore управляет хранением и извлечением задач
type TaskStore struct {
	tasks map[string]*Task
	mutex sync.RWMutex
}

// NewTaskStore создает новый экземпляр TaskStore
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*Task),
	}
}

// CreateTask создает новую задачу со статусом 'pending'
func (ts *TaskStore) CreateTask(taskID string, ttl time.Duration) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	now := time.Now()
	ts.tasks[taskID] = &Task{
		ID:        taskID,
		Status:    TaskStatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
}

// UpdateTaskStatus обновляет статус задачи
func (ts *TaskStore) UpdateTaskStatus(taskID string, status TaskStatus) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("задача с ID %s не найдена", taskID)
	}

	task.Status = status
	return nil
}

// UpdateTaskResult обновляет результат и статус задачи на 'completed'
func (ts *TaskStore) UpdateTaskResult(taskID string, result []domain.User) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("задача с ID %s не найдена", taskID)
	}

	task.Status = TaskStatusCompleted
	task.Result = result
	return nil
}

// UpdateTaskError обновляет сообщение об ошибке и статус задачи на 'failed'
func (ts *TaskStore) UpdateTaskError(taskID string, errorMessage string) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("задача с ID %s не найдена", taskID)
	}

	task.Status = TaskStatusFailed
	task.ErrorMessage = errorMessage
	return nil
}

// GetTask извлекает задачу по ее ID
func (ts *TaskStore) GetTask(taskID string) (*Task, error) {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("задача с ID %s не найдена", taskID)
	}

	return task, nil
}

// CleanupExpired удаляет просроченные задачи из хранилища
func (ts *TaskStore) CleanupExpired() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	now := time.Now()
	for taskID, task := range ts.tasks {
		if now.After(task.ExpiresAt) {
			delete(ts.tasks, taskID)
		}
	}
}

// StartCleanupTicker запускает тикер для периодической очистки просроченных задач
func (ts *TaskStore) StartCleanupTicker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ts.CleanupExpired()
			}
		}
	}()
}
