package bot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
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

	t.Run("rejects new files if file limit is exceeded", func(t *testing.T) {
		bot := newTestBot(t, defaultConfig, &mockServerClient{})

		var receivedMessages []string
		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) {
			m, ok := msg.(tgbotapi.MessageConfig)
			if ok {
				receivedMessages = append(receivedMessages, m.Text)
			}
			return tgbotapi.Message{}, nil
		}

		chatID := int64(999)

		// Добавляем файлы в пачку до лимита
		bot.pendingFilesMutex.Lock()
		bot.pendingFiles[chatID] = &fileBatch{
			docs: []*tgbotapi.Document{
				{FileID: "file1", FileName: "file1.json"},
				{FileID: "file2", FileName: "file2.json"},
				{FileID: "file3", FileName: "file3.json"},
			},
			// Создаем таймер, который не будет срабатывать в рамках теста
			timer: time.NewTimer(time.Hour),
		}
		bot.pendingFilesMutex.Unlock()

		// Пытаемся добавить еще один файл, превышая лимит
		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, Document: &tgbotapi.Document{FileID: "file4", FileName: "file4.json"}}
		bot.handleDocument(ctx, msg)

		require.Len(t, receivedMessages, 1)
		assert.Contains(t, receivedMessages[0], "Превышен лимит файлов в одном сообщении")
		assert.Contains(t, receivedMessages[0], "3 файлов")

		// Проверяем, что пачка файлов была удалена после превышения лимита
		bot.pendingFilesMutex.Lock()
		_, exists := bot.pendingFiles[chatID]
		bot.pendingFilesMutex.Unlock()
		assert.False(t, exists, "Пачка файлов должна быть удалена после превышения лимита")
	})
}

func TestBot_ProcessFileBatch_Sorting(t *testing.T) {
	defaultConfig := config.BotConfig{
		MaxFilesPerMessage:     3,
		FileBatchTimeoutSecs:   1,
		PollingIntervalSeconds: 1,
	}

	ctx := context.Background()

	// Создаем тестовые сервера для разных файлов
	content1 := []byte(`{"name":"chat1","messages":[{"id":1,"text":"Hello"}]}`)
	content2 := []byte(`{"name":"chat2","messages":[{"id":1,"text":"World"}]}`)
	content3 := []byte(`{"name":"chat3","messages":[{"id":1,"text":"!"}]}`)

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content1)
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content2)
	}))
	defer ts2.Close()

	ts3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content3)
	}))
	defer ts3.Close()

	var wg sync.WaitGroup
	wg.Add(1)

	var receivedFiles []DocumentFile
	mockClient := &mockServerClient{
		startTaskFunc: func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
			receivedFiles = files
			wg.Done()
			return &StartTaskResponse{TaskID: "test-task"}, nil
		},
	}

	bot := newTestBot(t, defaultConfig, mockClient)

	// Создаем клиент, который возвращает разные URL для разных FileID
	bot.httpClient = &http.Client{}
	bot.getFileDirectURLFunc = func(fileID string) (string, error) {
		switch fileID {
		case "file1":
			return ts1.URL, nil
		case "file2":
			return ts2.URL, nil
		case "file3":
			return ts3.URL, nil
		default:
			return "", nil
		}
	}

	// Создаем пачку вручную, как это делает handleDocument
	chatID := int64(123)
	bot.pendingFilesMutex.Lock()
	bot.pendingFiles[chatID] = &fileBatch{
		docs: []*tgbotapi.Document{
			{FileID: "file2", FileName: "file2.json"},
			{FileID: "file1", FileName: "file1.json"},
			{FileID: "file3", FileName: "file3.json"},
		},
	}
	bot.pendingFilesMutex.Unlock()

	go func() {
		// Даем немного времени, чтобы горутина запустилась
		time.Sleep(100 * time.Millisecond)
		bot.processFileBatch(ctx, chatID)
	}()

	wg.Wait()

	// Проверяем, что файлы были отправлены в отсортированном порядке по хешу содержимого
	require.Len(t, receivedFiles, 3)

	// Проверим, что содержимое соответствует именам файлов
	contentMap := map[string][]byte{
		"file1.json": content1,
		"file2.json": content2,
		"file3.json": content3,
	}

	for _, file := range receivedFiles {
		expectedContent, ok := contentMap[file.Name]
		if !assert.True(t, ok, "Unexpected file name: %s", file.Name) {
			continue
		}
		fileContent, err := io.ReadAll(file.Content)
		assert.NoError(t, err)
		assert.Equal(t, expectedContent, fileContent, "Content mismatch for file %s", file.Name)
	}

	// Теперь проверим, что порядок соответствует сортировке по хешу.
	// Для этого вычислим хеши содержимого и проверим, что они идут в возрастающем порядке.
	type fileHash struct {
		name string
		hash string
	}

	var hashes []fileHash
	for name, content := range contentMap {
		h := sha256.New()
		h.Write(content)
		hash := fmt.Sprintf("%x", h.Sum(nil))
		hashes = append(hashes, fileHash{name: name, hash: hash})
	}

	// Сортируем наши эталонные хеши
	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i].hash < hashes[j].hash
	})

	// Проверяем, что полученные файлы идут в том же порядке, что и отсортированные хеши
	for i, expected := range hashes {
		assert.Equal(t, expected.name, receivedFiles[i].Name, "File at position %d should be %s based on hash sort", i, expected.name)
	}
}
