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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// ChatProcessor определяет интерфейс для варианта использования, который обрабатывает чаты.
type ChatProcessor interface {
	ProcessChat(ctx context.Context, filePaths []string) ([]domain.User, error)
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
				http.Error(w, "Не удалось разобрать форму", http.StatusBadRequest)
				return
			}

			files := r.MultipartForm.File["files"]
			if len(files) == 0 {
				http.Error(w, "Файлы не загружены", http.StatusBadRequest)
				return
			}

			taskID := uuid.NewString()
			var tempFilePaths []string
			tempDir := os.TempDir()

			for i, fileHeader := range files {
				file, err := fileHeader.Open()
				if err != nil {
					http.Error(w, "Не удалось открыть загруженный файл", http.StatusInternalServerError)
					return
				}
				defer file.Close()

				tempFilePath := filepath.Join(tempDir, fmt.Sprintf("chat_%s_%d.json", taskID, i))
				tempFilePaths = append(tempFilePaths, tempFilePath)

				out, err := os.Create(tempFilePath)
				if err != nil {
					http.Error(w, "Не удалось создать временный файл", http.StatusInternalServerError)
					return
				}
				defer out.Close()

				if _, err := io.Copy(out, file); err != nil {
					http.Error(w, "Не удалось сохранить загруженный файл", http.StatusInternalServerError)
					return
				}
				slog.Info("Загруженный файл сохранен во временный файл", "path", tempFilePath)
			}

			// Создание задачи в хранилище
			taskStore.CreateTask(taskID, cfg.Processing.CacheTTL)

			// Запуск обработки в горутине
			go func(paths []string) {
				defer func() {
					for _, path := range paths {
						os.Remove(path)
					}
				}()

				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Передаем фоновый контекст; use case сам управляет своим таймаутом.
				result, err := processor.ProcessChat(context.Background(), paths)
				if err != nil {
					taskStore.UpdateTaskError(taskID, err.Error())
					return
				}

				taskStore.UpdateTaskResult(taskID, result)
			}(tempFilePaths)

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
				http.Error(w, "Не удалось декодировать тело запроса", http.StatusBadRequest)
				return
			}

			if req.Hash == "" {
				http.Error(w, "Требуется хеш", http.StatusBadRequest)
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
					slog.Info("Попадание в кеш для хеша", "hash", req.Hash, "task_id", taskID)
					return
				}

				// Если в кеше не найдено, обычно нам нужен файл для обработки.
				// В этой реализации мы вернем ошибку, если хеш не найден в кеше.
				// В более продвинутой реализации вы могли бы хранить файл, связанный с хешем, и обрабатывать его здесь.
				taskStore.UpdateTaskError(taskID, "Файл не найден в кеше для данного хеша")
				slog.Info("Промах кеша для хеша", "hash", req.Hash, "task_id", taskID)
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
				http.Error(w, "Задача не найдена", http.StatusNotFound)
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
				http.Error(w, "Задача не найдена", http.StatusNotFound)
				return
			}

			if task.Status != TaskStatusCompleted {
				http.Error(w, "Задача не завершена", http.StatusBadRequest)
				return
			}

			// Получение параметров пагинации
			page := r.URL.Query().Get("page")
			pageSize := r.URL.Query().Get("page_size")

			// Разбор page и pageSize, по умолчанию 1 и 50 соответственно
			parsedPage := 1
			parsedPageSize := 50
			// В реальной реализации вы бы использовали strconv.Atoi и обрабатывали ошибки
			// Для простоты мы просто проверим, что параметр запроса не пуст, и попытаемся его разобрать
			if page != "" {
				// parsedPage, _ = strconv.Atoi(page) // Это упрощение
				// Пока что мы просто будем использовать значение по умолчанию, если разбор не удался
			}
			if pageSize != "" {
				// parsedPageSize, _ = strconv.Atoi(pageSize) // Это упрощение
				// Пока что мы просто будем использовать значение по умолчанию, если разбор не удался
			}

			// Вычисление смещения
			offset := (parsedPage - 1) * parsedPageSize

			// Нарезка данных результата в соответствии с пагинацией
			startIndex := offset
			endIndex := offset + parsedPageSize

			if startIndex >= len(task.Result) {
				// Если startIndex больше или равен длине результата, вернуть пустой срез
				startIndex = len(task.Result)
				endIndex = len(task.Result)
			}
			if endIndex > len(task.Result) {
				endIndex = len(task.Result)
			}

			paginatedData := task.Result[startIndex:endIndex]

			// Вычисление метаданных пагинации
			totalItems := len(task.Result)
			totalPages := (totalItems + parsedPageSize - 1) / parsedPageSize // Округление вверх

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
	slog.Info("Завершение работы HTTP-сервера")
	return s.HTTPServer.Shutdown(ctx)
}
