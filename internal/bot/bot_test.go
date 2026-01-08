package bot

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"telegram-chat-parser/cmd/bot/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServerClient — это мок для ServerAPI.
type mockServerClient struct {
	startTaskFunc func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error)
}

func (m *mockServerClient) StartTask(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
	if m.startTaskFunc != nil {
		return m.startTaskFunc(ctx, files)
	}
	return &StartTaskResponse{TaskID: "mock-task-id"}, nil
}

func (m *mockServerClient) GetTaskStatus(ctx context.Context, taskID string) (*TaskStatusResponse, error) {
	return &TaskStatusResponse{Status: "completed"}, nil
}

func (m *mockServerClient) GetTaskResult(ctx context.Context, taskID string, page, pageSize int) (*TaskResultResponse, error) {
	return &TaskResultResponse{Data: []UserDTO{}}, nil
}

// newTestBot создает бота с моками для тестирования.
func newTestBot(t *testing.T, cfg config.BotConfig, serverClient ServerAPI) *Bot {
	bot := &Bot{
		api:          nil, // Не используется напрямую благодаря мокам
		cfg:          cfg,
		serverClient: serverClient,
		taskStore:    NewTaskStore(),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		pendingFiles: make(map[int64]*fileBatch),
		httpClient:   http.DefaultClient, // Будет заменен в тестах
	}
	// Инициализируем поля-функции пустышками, чтобы избежать nil pointer dereference.
	// В каждом тесте они будут заменены на нужные моки.
	bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
	bot.getFileDirectURLFunc = func(fileID string) (string, error) { return "", nil }
	return bot
}

func TestBot_HandleDocument_Batching(t *testing.T) {
	defaultConfig := config.BotConfig{
		MaxFilesPerMessage:     3,
		FileBatchTimeoutSecs:   1, // Короткий таймаут для теста
		PollingIntervalSeconds: 1, // Положительное значение для тикера
	}

	ctx := context.Background()

	// Запускаем тестовый сервер, который будет имитировать API Telegram для скачивания файлов
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake file content"))
	}))
	defer ts.Close()

	t.Run("sends a batch with two files after timeout", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)

		startTaskCalled := make(chan []DocumentFile, 1)

		mockClient := &mockServerClient{
			startTaskFunc: func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
				startTaskCalled <- files
				wg.Done()
				return &StartTaskResponse{TaskID: "test-task"}, nil
			},
		}

		bot := newTestBot(t, defaultConfig, mockClient)
		bot.httpClient = ts.Client() // Внедряем клиент тестового сервера

		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
		bot.getFileDirectURLFunc = func(fileID string) (string, error) {
			// Возвращаем URL нашего тестового сервера
			return ts.URL + "/" + fileID, nil
		}

		msg1 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 123}, Document: &tgbotapi.Document{FileID: "file1", FileName: "test1.json"}}
		msg2 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 123}, Document: &tgbotapi.Document{FileID: "file2", FileName: "test2.json"}}

		bot.handleDocument(ctx, msg1)
		time.Sleep(500 * time.Millisecond)
		bot.handleDocument(ctx, msg2)

		wg.Wait()

		select {
		case files := <-startTaskCalled:
			assert.Len(t, files, 2)
			assert.Equal(t, "test1.json", files[0].Name)
			assert.Equal(t, "test2.json", files[1].Name)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for StartTask to be called")
		}
	})

	t.Run("sends a batch immediately when file limit is reached", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)

		startTaskCalled := make(chan []DocumentFile, 1)

		mockClient := &mockServerClient{
			startTaskFunc: func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
				startTaskCalled <- files
				wg.Done()
				return &StartTaskResponse{TaskID: "test-task"}, nil
			},
		}

		limitConfig := defaultConfig
		limitConfig.MaxFilesPerMessage = 2
		bot := newTestBot(t, limitConfig, mockClient)
		bot.httpClient = ts.Client()

		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
		bot.getFileDirectURLFunc = func(fileID string) (string, error) { return ts.URL + "/" + fileID, nil }

		msg1 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 456}, Document: &tgbotapi.Document{FileID: "fileA", FileName: "A.json"}}
		msg2 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 456}, Document: &tgbotapi.Document{FileID: "fileB", FileName: "B.json"}}

		bot.handleDocument(ctx, msg1)
		bot.handleDocument(ctx, msg2)

		wg.Wait()

		select {
		case files := <-startTaskCalled:
			assert.Len(t, files, 2)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for immediate StartTask call")
		}
	})

	t.Run("rejects new files if a task is already processing", func(t *testing.T) {
		bot := newTestBot(t, defaultConfig, &mockServerClient{})

		var receivedMessages []string
		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) {
			m, ok := msg.(tgbotapi.MessageConfig)
			if ok {
				receivedMessages = append(receivedMessages, m.Text)
			}
			return tgbotapi.Message{}, nil
		}

		chatID := int64(789)
		bot.taskStore.Set(chatID, "some-active-task-id")

		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, Document: &tgbotapi.Document{FileID: "fileX", FileName: "X.json"}}
		bot.handleDocument(ctx, msg)

		require.Len(t, receivedMessages, 1)
		assert.Contains(t, receivedMessages[0], "Пожалуйста, подождите завершения предыдущей задачи")
	})
}
