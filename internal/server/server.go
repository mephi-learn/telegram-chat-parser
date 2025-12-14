package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"
	"telegram-chat-parser/internal/ratelimiter"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// ChatProcessor defines the interface for the use case that processes chats.
type ChatProcessor interface {
	ProcessChat(ctx context.Context, filePath string) ([]domain.User, error)
}

// Server represents the HTTP server
type Server struct {
	HTTPServer  *http.Server
	cfg         *config.Config
	taskStore   *TaskStore
	cacheStore  *cache.CacheStore
	processor   ChatProcessor
	rateLimiter *ratelimiter.AdaptiveRateLimiter
}

// New creates a new instance of Server
func New(cfg *config.Config, processor ChatProcessor, taskStore *TaskStore, cacheStore *cache.CacheStore) (*Server, error) {
	chiRouter := chi.NewRouter()

	// Middleware
	chiRouter.Use(middleware.Logger)
	chiRouter.Use(middleware.Recoverer)

	// Health check endpoint
	chiRouter.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Telegram API health is checked during startup via AuthManager.
		// If server is running, Telegram API is assumed to be OK.
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// API routes
	chiRouter.Route("/api/v1", func(r chi.Router) {
		// Endpoint to start a new processing task
		r.Post("/process", func(w http.ResponseWriter, r *http.Request) {
			// Parse multipart form
			err := r.ParseMultipartForm(10 << 20) // 10 MB max
			if err != nil {
				http.Error(w, "Unable to parse form", http.StatusBadRequest)
				return
			}

			file, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "Unable to retrieve file from form", http.StatusBadRequest)
				return
			}
			defer file.Close()

			// Generate a unique task ID
			taskID := uuid.NewString()

			// Create a temporary file to store the uploaded data
			tempDir := os.TempDir()
			tempFilePath := filepath.Join(tempDir, fmt.Sprintf("chat_%s.json", taskID))

			out, err := os.Create(tempFilePath)
			if err != nil {
				http.Error(w, "Unable to create temporary file", http.StatusInternalServerError)
				return
			}
			defer out.Close()

			_, err = io.Copy(out, file)
			if err != nil {
				http.Error(w, "Unable to save uploaded file", http.StatusInternalServerError)
				return
			}

			// Log the raw content of the uploaded file
			// Read the temporary file to log its content
			tempFileContent, readErr := os.ReadFile(tempFilePath)
			if readErr != nil {
				slog.Error("Failed to read temporary file for logging", "error", readErr, "path", tempFilePath)
			} else {
				// Define a local min function for slices/strings
				min := func(a, b int) int {
					if a < b {
						return a
					}
					return b
				}
				slog.Info("Raw content of uploaded file received by server", "file_path", tempFilePath, "content_length", len(tempFileContent), "content_preview", string(tempFileContent[:min(200, len(tempFileContent))]))
			}

			// Create a task in the store
			taskStore.CreateTask(taskID, 24*time.Hour) // TTL for the task record

			// Start processing in a goroutine
			go func() {
				// Update status to processing
				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Process the chat
				// Use context.Background() for the long-running task to avoid cancellation from the HTTP request context.
				result, err := processor.ProcessChat(context.Background(), tempFilePath)
				if err != nil {
					taskStore.UpdateTaskError(taskID, err.Error())
					// Clean up the temporary file on error
					os.Remove(tempFilePath)
					return
				}

				// Update task with result
				taskStore.UpdateTaskResult(taskID, result)

				// Clean up the temporary file on success
				os.Remove(tempFilePath)
			}()

			// Return task ID
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
		})

		// Endpoint to start a new processing task by hash
		r.Post("/process-by-hash", func(w http.ResponseWriter, r *http.Request) {
			// Parse request body
			var req struct {
				Hash string `json:"hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Unable to decode request body", http.StatusBadRequest)
				return
			}

			if req.Hash == "" {
				http.Error(w, "Hash is required", http.StatusBadRequest)
				return
			}

			// Generate a unique task ID
			taskID := uuid.NewString()

			// Create a task in the store
			taskStore.CreateTask(taskID, 24*time.Hour) // TTL for the task record

			// Start processing in a goroutine
			go func() {
				// Update status to processing
				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Try to get the result from cache
				if cachedItem, found := cacheStore.Get(req.Hash); found {
					// If found in cache, update the task with the cached result
					taskStore.UpdateTaskResult(taskID, cachedItem.Data)
					slog.Info("Cache hit for hash", "hash", req.Hash, "task_id", taskID)
					return
				}

				// If not found in cache, we would normally need the file to process.
				// For this implementation, we'll return an error if the hash is not in the cache.
				// In a more advanced implementation, you might store the file associated with the hash and process it here.
				taskStore.UpdateTaskError(taskID, "File not found in cache for the given hash")
				slog.Info("Cache miss for hash", "hash", req.Hash, "task_id", taskID)
			}()

			// Return task ID
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
		})

		// Endpoint to check task status
		r.Get("/tasks/{taskID}", func(w http.ResponseWriter, r *http.Request) {
			taskID := chi.URLParam(r, "taskID")

			task, err := taskStore.GetTask(taskID)
			if err != nil {
				http.Error(w, "Task not found", http.StatusNotFound)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"task_id":       task.ID,
				"status":        task.Status,
				"error_message": task.ErrorMessage,
			})
		})

		// Endpoint to get paginated task result
		r.Get("/tasks/{taskID}/result", func(w http.ResponseWriter, r *http.Request) {
			taskID := chi.URLParam(r, "taskID")

			task, err := taskStore.GetTask(taskID)
			if err != nil {
				http.Error(w, "Task not found", http.StatusNotFound)
				return
			}

			if task.Status != TaskStatusCompleted {
				http.Error(w, "Task is not completed", http.StatusBadRequest)
				return
			}

			// Get pagination parameters
			page := r.URL.Query().Get("page")
			pageSize := r.URL.Query().Get("page_size")

			// Parse page and pageSize, default to 1 and 50 respectively
			parsedPage := 1
			parsedPageSize := 50
			// In a real implementation, you would use strconv.Atoi and handle errors
			// For simplicity, we'll just check if the query param is not empty and try to parse it
			if page != "" {
				// parsedPage, _ = strconv.Atoi(page) // This is a simplification
				// For now, we'll just use the default value if parsing fails
			}
			if pageSize != "" {
				// parsedPageSize, _ = strconv.Atoi(pageSize) // This is a simplification
				// For now, we'll just use the default value if parsing fails
			}

			// Calculate offset
			offset := (parsedPage - 1) * parsedPageSize

			// Slice the result data according to pagination
			startIndex := offset
			endIndex := offset + parsedPageSize

			if startIndex >= len(task.Result) {
				// If startIndex is greater than or equal to the length of the result, return an empty slice
				startIndex = len(task.Result)
				endIndex = len(task.Result)
			}
			if endIndex > len(task.Result) {
				endIndex = len(task.Result)
			}

			paginatedData := task.Result[startIndex:endIndex]

			// Calculate pagination metadata
			totalItems := len(task.Result)
			totalPages := (totalItems + parsedPageSize - 1) / parsedPageSize // Ceiling division

			// Prepare response
			response := struct {
				Pagination struct {
					CurrentPage int `json:"current_page"`
					PageSize    int `json:"page_size"`
					TotalItems  int `json:"total_items"`
					TotalPages  int `json:"total_pages"`
				} `json:"pagination"`
				Data []domain.User `json:"data"`
			}{
				Pagination: struct {
					CurrentPage int `json:"current_page"`
					PageSize    int `json:"page_size"`
					TotalItems  int `json:"total_items"`
					TotalPages  int `json:"total_pages"`
				}{
					CurrentPage: parsedPage,
					PageSize:    parsedPageSize,
					TotalItems:  totalItems,
					TotalPages:  totalPages,
				},
				Data: paginatedData,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		})
	})

	httpServer := &http.Server{
		Addr:         cfg.Address(),
		Handler:      chiRouter,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s := &Server{
		HTTPServer: httpServer,
		cfg:        cfg,
		taskStore:  taskStore,
		cacheStore: cacheStore,
		processor:  processor,
	}

	// Start the cleanup ticker for expired tasks
	ctx, cancel := context.WithCancel(context.Background())
	s.taskStore.StartCleanupTicker(ctx, 1*time.Hour) // Clean up every hour

	// Start the cleanup ticker for expired cache items
	s.cacheStore.StartCleanupTicker(ctx, 1*time.Hour) // Clean up every hour

	// We need a way to stop the ticker on shutdown
	// This is a simplified approach; a more robust solution would manage this lifecycle better.
	// For now, we'll rely on the main function's context cancellation.
	go func() {
		<-ctx.Done()
		cancel() // This stops the ticker goroutine
	}()

	return s, nil
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe() error {
	return s.HTTPServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down HTTP server")
	return s.HTTPServer.Shutdown(ctx)
}
