package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// ServerClient — клиент для взаимодействия с API бэкенд-сервера.
type ServerClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewServerClient создает новый экземпляр ServerClient.
func NewServerClient(baseURL string) *ServerClient {
	return &ServerClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Общий таймаут для запросов
		},
	}
}

// API-ответы
type StartTaskResponse struct {
	TaskID string `json:"task_id"`
}

type TaskStatusResponse struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// PaginationDTO представляет собой объект пагинации из ответа сервера.
type PaginationDTO struct {
	CurrentPage int `json:"current_page"`
	PageSize    int `json:"page_size"`
	TotalItems  int `json:"total_items"`
	TotalPages  int `json:"total_pages"`
}

// UserDTO представляет собой объект пользователя из ответа сервера.
type UserDTO struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Bio      string `json:"bio"`
	Channel  string `json:"channel,omitempty"`
}

type TaskResultResponse struct {
	Pagination PaginationDTO `json:"pagination"`
	Data       []UserDTO     `json:"data"`
}

// DocumentFile представляет файл для загрузки.
type DocumentFile struct {
	Name    string
	Content io.Reader
}

// StartTask отправляет один или несколько файлов на сервер для начала обработки.
func (c *ServerClient) StartTask(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	for _, file := range files {
		fw, err := w.CreateFormFile("files", file.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create form file for %s: %w", file.Name, err)
		}
		if _, err = io.Copy(fw, file.Content); err != nil {
			return nil, fmt.Errorf("failed to copy file content for %s: %w", file.Name, err)
		}
	}

	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/process", &b)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result StartTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetTaskStatus запрашивает статус задачи.
func (c *ServerClient) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/tasks/"+taskID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result TaskStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetTaskResult запрашивает результат выполненной задачи.
func (c *ServerClient) GetTaskResult(ctx context.Context, taskID string, page, pageSize int) (*TaskResultResponse, error) {
	url := fmt.Sprintf("%s/api/v1/tasks/%s/result?page=%d&page_size=%d", c.baseURL, taskID, page, pageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result TaskResultResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
