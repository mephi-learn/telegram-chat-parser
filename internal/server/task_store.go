package server

import (
	"context"
	"fmt"
	"sync"
	"telegram-chat-parser/internal/domain"
	"time"
)

// TaskStatus represents the status of a processing task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

// Task represents a single processing task
type Task struct {
	ID           string
	Status       TaskStatus
	Result       []domain.User
	ErrorMessage string
	CreatedAt    time.Time
	ExpiresAt    time.Time // For automatic cleanup
}

// TaskStore manages the storage and retrieval of tasks
type TaskStore struct {
	tasks map[string]*Task
	mutex sync.RWMutex
}

// NewTaskStore creates a new instance of TaskStore
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]*Task),
	}
}

// CreateTask creates a new task with status 'pending'
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

// UpdateTaskStatus updates the status of a task
func (ts *TaskStore) UpdateTaskStatus(taskID string, status TaskStatus) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	task.Status = status
	return nil
}

// UpdateTaskResult updates the result and status of a task to 'completed'
func (ts *TaskStore) UpdateTaskResult(taskID string, result []domain.User) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	task.Status = TaskStatusCompleted
	task.Result = result
	return nil
}

// UpdateTaskError updates the error message and status of a task to 'failed'
func (ts *TaskStore) UpdateTaskError(taskID string, errorMessage string) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	task.Status = TaskStatusFailed
	task.ErrorMessage = errorMessage
	return nil
}

// GetTask retrieves a task by its ID
func (ts *TaskStore) GetTask(taskID string) (*Task, error) {
	ts.mutex.RLock()
	defer ts.mutex.RUnlock()

	task, exists := ts.tasks[taskID]
	if !exists {
		return nil, fmt.Errorf("task with ID %s not found", taskID)
	}

	return task, nil
}

// CleanupExpired removes expired tasks from the store
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

// StartCleanupTicker starts a ticker to periodically clean up expired tasks
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
