package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"telegram-chat-parser/internal/cache"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/pkg/config"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// ChatProcessor определяет интерфейс для варианта использования, который обрабатывает чаты.
type ChatProcessor interface {
	ProcessChat(ctx context.Context, filePaths []string) ([]domain.User, error)
	ProcessChatFromData(ctx context.Context, fileDataList [][]byte) ([]domain.User, error)
}

// Server представляет HTTP-сервер
type Server struct {
	HTTPServer *http.Server
	cfg        *config.Config
	taskStore  *TaskStore
	cacheStore *cache.CacheStore
	processor  ChatProcessor
}

// New создает новый экземпляр Server
func New(cfg *config.Config, processor ChatProcessor, taskStore *TaskStore, cacheStore *cache.CacheStore) (*Server, error) {
	chiRouter := chi.NewRouter()

	// Промежуточное ПО
	chiRouter.Use(middleware.Logger)
	chiRouter.Use(middleware.Recoverer)

	// Конечная точка для проверки работоспособности
	chiRouter.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Работоспособность Telegram API проверяется при запуске через AuthManager.
		// Если сервер запущен, предполагается, что Telegram API в порядке.
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// Маршруты API
	chiRouter.Route("/api/v1", func(r chi.Router) {
		// Конечная точка для запуска новой задачи обработки
		r.Post("/process", func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(cfg.Server.MaxUploadSizeMB << 20); err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}

			files := r.MultipartForm.File["files"]
			if len(files) == 0 {
				http.Error(w, "No files uploaded", http.StatusBadRequest)
				return
			}

			taskID := uuid.NewString()
			var fileDataList [][]byte

			for i, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					http.Error(w, "Failed to open uploaded file", http.StatusInternalServerError)
					return
				}
				defer file.Close()

				data, err := io.ReadAll(file)
				if err != nil {
					http.Error(w, "Failed to read uploaded file", http.StatusInternalServerError)
					return
				}
				fileDataList = append(fileDataList, data)
				slog.Info("Uploaded file read into memory", "size", len(data), "index", i)
			}

			// Создание задачи в хранилище
			taskStore.CreateTask(taskID, cfg.Processing.CacheTTL)

			// Запуск обработки в горутине
			go func(dataList [][]byte) {
				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Передаем фоновый контекст; use case сам управляет своим таймаутом.
				result, err := processor.ProcessChatFromData(context.Background(), dataList)
				if err != nil {
					taskStore.UpdateTaskError(taskID, err.Error())
					return
				}

				taskStore.UpdateTaskResult(taskID, result)
			}(fileDataList)

			// Возврат идентификатора задачи
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
		})

		// Конечная точка для запуска новой задачи обработки по хешу
		r.Post("/process-by-hash", func(w http.ResponseWriter, r *http.Request) {
			// Разбор тела запроса
			var req struct {
				Hash string `json:"hash"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Failed to decode request body", http.StatusBadRequest)
				return
			}

			if req.Hash == "" {
				http.Error(w, "Hash is required", http.StatusBadRequest)
				return
			}

			// Генерация уникального идентификатора задачи
			taskID := uuid.NewString()

			// Создание задачи в хранилище
			taskStore.CreateTask(taskID, cfg.Processing.CacheTTL)

			// Запуск обработки в горутине
			go func() {
				// Обновление статуса до "в обработке"
				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Попытка получить результат из кеша
				if cachedItem, found := cacheStore.Get(req.Hash); found {
					// Если найдено в кеше, обновить задачу кешированным результатом
					taskStore.UpdateTaskResult(taskID, cachedItem.Data)
					slog.Info("Cache hit for hash", "hash", req.Hash, "task_id", taskID)
					return
				}

				// Если в кеше не найдено, обычно нам нужен файл для обработки.
				// В этой реализации мы вернем ошибку, если хеш не найден в кеше.
				// В более продвинутой реализации вы могли бы хранить файл, связанный с хешем, и обрабатывать его здесь.
				taskStore.UpdateTaskError(taskID, "File not found in cache for this hash")
				slog.Info("Cache miss for hash", "hash", req.Hash, "task_id", taskID)
			}()

			// Возврат идентификатора задачи
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
		})

		// Конечная точка для проверки статуса задачи
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

		// Конечная точка для получения результата задачи с пагинацией
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

			// Получение параметров пагинации
			page := r.URL.Query().Get("page")
			pageSize := r.URL.Query().Get("page_size")

			// Разбор page и pageSize, по умолчанию 1 и 50 соответственно
			parsedPage := 1
			if p, err := strconv.Atoi(page); err == nil && p > 0 {
				parsedPage = p
			}

			parsedPageSize := 50
			if ps, err := strconv.Atoi(pageSize); err == nil && ps > 0 {
				parsedPageSize = ps
			}

			// Вычисление смещения и нарезка данных
			var paginatedData []domain.User
			totalItems := len(task.Result)
			offset := (parsedPage - 1) * parsedPageSize

			if offset < totalItems {
				endIndex := offset + parsedPageSize
				if endIndex > totalItems {
					endIndex = totalItems
				}
				paginatedData = task.Result[offset:endIndex]
			} else {
				// Если смещение за пределами данных, возвращаем пустой срез
				paginatedData = []domain.User{}
			}

			// Вычисление метаданных пагинации
			totalPages := 0
			if totalItems > 0 {
				totalPages = (totalItems + parsedPageSize - 1) / parsedPageSize // Округление вверх
			}

			// Подготовка ответа
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
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	s := &Server{
		HTTPServer: httpServer,
		cfg:        cfg,
		taskStore:  taskStore,
		cacheStore: cacheStore,
		processor:  processor,
	}

	// Запуск тикера для очистки просроченных задач
	ctx, cancel := context.WithCancel(context.Background())
	s.taskStore.StartCleanupTicker(ctx, cfg.Server.CleanupInterval)

	// Запуск тикера для очистки просроченных элементов кеша
	s.cacheStore.StartCleanupTicker(ctx, cfg.Server.CleanupInterval)

	// Нам нужен способ остановить тикер при завершении работы
	// Это упрощенный подход; более надежное решение лучше бы управляло этим жизненным циклом.
	// Пока что мы будем полагаться на отмену контекста основной функции.
	go func() {
		<-ctx.Done()
		cancel() // Это останавливает горутину тикера
	}()

	return s, nil
}

// ListenAndServe запускает HTTP-сервер
func (s *Server) ListenAndServe() error {
	return s.HTTPServer.ListenAndServe()
}

// Shutdown корректно завершает работу HTTP-сервера
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down HTTP server")
	return s.HTTPServer.Shutdown(ctx)
}
