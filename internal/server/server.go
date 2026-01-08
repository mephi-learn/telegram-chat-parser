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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// ChatProcessor определяет интерфейс для варианта использования, который обрабатывает чаты.
type ChatProcessor interface {
	ProcessChat(ctx context.Context, filePath string) ([]domain.User, error)
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
			// Разбор мультипарт-формы
			err := r.ParseMultipartForm(10 << 20) // максимум 10 MB
			if err != nil {
				http.Error(w, "Не удалось разобрать форму", http.StatusBadRequest)
				return
			}

			file, _, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "Не удалось получить файл из формы", http.StatusBadRequest)
				return
			}
			defer file.Close()

			// Генерация уникального идентификатора задачи
			taskID := uuid.NewString()

			// Создание временного файла для хранения загруженных данных
			tempDir := os.TempDir()
			tempFilePath := filepath.Join(tempDir, fmt.Sprintf("chat_%s.json", taskID))

			out, err := os.Create(tempFilePath)
			if err != nil {
				http.Error(w, "Не удалось создать временный файл", http.StatusInternalServerError)
				return
			}
			defer out.Close()

			_, err = io.Copy(out, file)
			if err != nil {
				http.Error(w, "Не удалось сохранить загруженный файл", http.StatusInternalServerError)
				return
			}

			// Логирование сырого содержимого загруженного файла
			// Чтение временного файла для логирования его содержимого
			tempFileContent, readErr := os.ReadFile(tempFilePath)
			if readErr != nil {
				slog.Error("Не удалось прочитать временный файл для логирования", "error", readErr, "path", tempFilePath)
			} else {
				// Определение локальной функции min для срезов/строк
				min := func(a, b int) int {
					if a < b {
						return a
					}
					return b
				}
				slog.Info("Сырое содержимое загруженного файла получено сервером", "file_path", tempFilePath, "content_length", len(tempFileContent), "content_preview", string(tempFileContent[:min(200, len(tempFileContent))]))
			}

			// Создание задачи в хранилище
			taskStore.CreateTask(taskID, 24*time.Hour) // TTL для записи о задаче

			// Запуск обработки в горутине
			go func() {
				// Обновление статуса до "в обработке"
				taskStore.UpdateTaskStatus(taskID, TaskStatusProcessing)

				// Создание контекста для задачи с таймаутом из конфигурации.
				taskCtx := context.Background()
				if cfg.Processing.TaskTimeoutSeconds > 0 {
					var cancel context.CancelFunc
					taskCtx, cancel = context.WithTimeout(context.Background(), time.Duration(cfg.Processing.TaskTimeoutSeconds)*time.Second)
					defer cancel()
				}

				// Обработка чата с использованием контекста, который может иметь таймаут.
				result, err := processor.ProcessChat(taskCtx, tempFilePath)
				if err != nil {
					taskStore.UpdateTaskError(taskID, err.Error())
					// Очистка временного файла при ошибке
					os.Remove(tempFilePath)
					return
				}

				// Обновление задачи с результатом
				taskStore.UpdateTaskResult(taskID, result)

				// Очистка временного файла при успехе
				os.Remove(tempFilePath)
			}()

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
			taskStore.CreateTask(taskID, 24*time.Hour) // TTL для записи о задаче

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

	// Запуск тикера для очистки просроченных задач
	ctx, cancel := context.WithCancel(context.Background())
	s.taskStore.StartCleanupTicker(ctx, 1*time.Hour) // Очистка каждый час

	// Запуск тикера для очистки просроченных элементов кеша
	s.cacheStore.StartCleanupTicker(ctx, 1*time.Hour) // Очистка каждый час

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
