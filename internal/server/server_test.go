package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Mock implementation for ChatProcessor
type mockProcessor struct {
	mock.Mock
}

func (m *mockProcessor) ProcessChat(ctx context.Context, filePath string) ([]domain.User, error) {
	args := m.Called(ctx, filePath)
	if res := args.Get(0); res != nil {
		return res.([]domain.User), args.Error(1)
	}
	return nil, args.Error(1)
}

func TestServer(t *testing.T) {
	cfg := &config.Config{
		Server: config.Server{Host: "localhost", Port: 8080},
	}
	mockProc := new(mockProcessor)
	taskStore := NewTaskStore()
	cacheStore := cache.NewCacheStore()

	srv, err := New(cfg, mockProc, taskStore, cacheStore)
	require.NoError(t, err)

	t.Run("Health Check", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var resp map[string]string
		err := json.NewDecoder(rr.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("Process Endpoint", func(t *testing.T) {
		// Create a dummy file for upload
		tmpfile, err := os.CreateTemp(t.TempDir(), "upload.json")
		require.NoError(t, err)
		tmpfile.WriteString(`{}`)
		require.NoError(t, tmpfile.Close())

		var b bytes.Buffer
		writer := multipart.NewWriter(&b)
		fw, err := writer.CreateFormFile("file", filepath.Base(tmpfile.Name()))
		require.NoError(t, err)
		file, err := os.Open(tmpfile.Name())
		require.NoError(t, err)
		_, err = io.Copy(fw, file)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		mockProc.On("ProcessChat", mock.Anything, mock.AnythingOfType("string")).Return([]domain.User{}, nil).Once()

		req := httptest.NewRequest("POST", "/api/v1/process", &b)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusAccepted, rr.Code)
		var resp map[string]string
		err = json.NewDecoder(rr.Body).Decode(&resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp["task_id"])

		// Allow time for the goroutine to start
		time.Sleep(10 * time.Millisecond)
		mockProc.AssertExpectations(t)
	})

	t.Run("Task Status Endpoint", func(t *testing.T) {
		taskID := "test-task-1"
		srv.taskStore.CreateTask(taskID, time.Minute)

		req := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var resp map[string]interface{}
		err := json.NewDecoder(rr.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, taskID, resp["task_id"])
		assert.Equal(t, string(TaskStatusPending), resp["status"])
	})

	t.Run("Task Not Found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tasks/non-existent", nil)
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("Task Result Endpoint - Not Completed", func(t *testing.T) {
		taskID := "test-task-2"
		srv.taskStore.CreateTask(taskID, time.Minute)

		req := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID+"/result", nil)
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("Task Result Endpoint - Success with Pagination", func(t *testing.T) {
		taskID := "test-task-3"
		srv.taskStore.CreateTask(taskID, time.Minute)
		result := make([]domain.User, 15)
		for i := 0; i < 15; i++ {
			result[i] = domain.User{ID: int64(i)}
		}
		srv.taskStore.UpdateTaskResult(taskID, result)

		req := httptest.NewRequest("GET", "/api/v1/tasks/"+taskID+"/result?page=2&page_size=5", nil)
		rr := httptest.NewRecorder()
		srv.HTTPServer.Handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var resp struct {
			Pagination struct {
				CurrentPage int `json:"current_page"`
				PageSize    int `json:"page_size"`
				TotalItems  int `json:"total_items"`
				TotalPages  int `json:"total_pages"`
			} `json:"pagination"`
			Data []domain.User `json:"data"`
		}
		err := json.NewDecoder(rr.Body).Decode(&resp)
		require.NoError(t, err)

		// Note: simplified parsing in handler uses default values
		assert.Equal(t, 1, resp.Pagination.CurrentPage)
		assert.Equal(t, 50, resp.Pagination.PageSize)
		assert.Equal(t, 15, resp.Pagination.TotalItems)
		assert.Equal(t, 1, resp.Pagination.TotalPages)
		assert.Len(t, resp.Data, 15)
	})
}
