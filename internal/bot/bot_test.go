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
		api:                nil, // Не используется напрямую благодаря мокам
		cfg:                cfg,
		serverClient:       serverClient,
		taskStore:          NewTaskStore(),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		pendingMediaGroups: make(map[string]*fileBatch),
		httpClient:         http.DefaultClient, // Будет заменен в тестах
	}
	// Инициализируем поля-функции пустышками, чтобы избежать nil pointer dereference.
	// В каждом тесте они будут заменены на нужные моки.
	bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
	bot.getFileDirectURLFunc = func(fileID string) (string, error) { return "", nil }

	// Для тестов переопределяем sendMessage, чтобы избежать асинхронности
	bot.sendMessageOverride = func(msg tgbotapi.Chattable) error {
		_, err := bot.sendMessageFunc(msg)
		return err
	}

	return bot
}

func TestBot_HandleDocument_MediaGroup(t *testing.T) {
	defaultConfig := config.BotConfig{
		MaxFilesPerMessage:     3,
		PollingIntervalSeconds: 1, // Положительное значение для тикера
	}

	ctx := context.Background()

	// Запускаем тестовый сервер, который будет имитировать API Telegram для скачивания файлов
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake file content"))
	}))
	defer ts.Close()

	t.Run("sends a media group batch after timeout", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(1)

		startTaskCalled := make(chan []DocumentFile, 1)

		mockClient := &mockServerClient{
			startTaskFunc: func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
				// Сортируем файлы по имени для предсказуемости теста
				sort.Slice(files, func(i, j int) bool {
					return files[i].Name < files[j].Name
				})
				startTaskCalled <- files
				wg.Done()
				return &StartTaskResponse{TaskID: "test-task"}, nil
			},
		}

		bot := newTestBot(t, defaultConfig, mockClient)
		bot.httpClient = ts.Client() // Внедряем клиент тестового сервера

		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
		bot.getFileDirectURLFunc = func(fileID string) (string, error) {
			return ts.URL + "/" + fileID, nil
		}

		mediaGroupID := "test-media-group-1"
		msg1 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 123}, MediaGroupID: mediaGroupID, Document: &tgbotapi.Document{FileID: "file1", FileName: "test1.json"}}
		msg2 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 123}, MediaGroupID: mediaGroupID, Document: &tgbotapi.Document{FileID: "file2", FileName: "test2.json"}}

		bot.handleDocument(ctx, msg1)
		time.Sleep(100 * time.Millisecond) // Небольшая задержка между сообщениями
		bot.handleDocument(ctx, msg2)

		// Ожидаем вызова StartTask после таймаута mediaGroupTimeout
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

	t.Run("sends a single file immediately", func(t *testing.T) {
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
		bot.httpClient = ts.Client()

		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) { return tgbotapi.Message{}, nil }
		bot.getFileDirectURLFunc = func(fileID string) (string, error) { return ts.URL + "/" + fileID, nil }

		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 456}, Document: &tgbotapi.Document{FileID: "fileA", FileName: "A.json"}}

		bot.handleDocument(ctx, msg) // Вызов без media group id

		wg.Wait()

		select {
		case files := <-startTaskCalled:
			assert.Len(t, files, 1)
			assert.Equal(t, "A.json", files[0].Name)
		case <-time.After(1 * time.Second):
			t.Fatal("timed out waiting for immediate StartTask call")
		}
	})

	t.Run("rejects new files if a task is already processing", func(t *testing.T) {
		done := make(chan bool, 1) // Канал для синхронизации

		bot := newTestBot(t, defaultConfig, &mockServerClient{})

		var receivedMessages []string
		var mu sync.Mutex // Мьютекс для защиты receivedMessages
		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) {
			m, ok := msg.(tgbotapi.MessageConfig)
			if ok {
				mu.Lock()
				receivedMessages = append(receivedMessages, m.Text)
				mu.Unlock()
				done <- true // Сигнализируем о завершении
			}
			return tgbotapi.Message{}, nil
		}

		chatID := int64(789)
		bot.taskStore.Set(chatID, "some-active-task-id")

		msg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, Document: &tgbotapi.Document{FileID: "fileX", FileName: "X.json"}}
		bot.handleDocument(ctx, msg)

		// Ждем получения сообщения или таймаут
		select {
		case <-done:
			// Сообщение получено
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for message to be sent")
		}

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, receivedMessages, 1)
		assert.Contains(t, receivedMessages[0], "Пожалуйста, подождите завершения предыдущей задачи")
	})

	t.Run("rejects files if media group limit is exceeded", func(t *testing.T) {
		done := make(chan bool, 1) // Канал для синхронизации

		limitConfig := defaultConfig
		limitConfig.MaxFilesPerMessage = 2
		bot := newTestBot(t, limitConfig, &mockServerClient{})

		var receivedMessages []string
		var mu sync.Mutex // Мьютекс для защиты receivedMessages
		var startTaskCalled bool

		bot.sendMessageFunc = func(msg tgbotapi.Chattable) (tgbotapi.Message, error) {
			m, ok := msg.(tgbotapi.MessageConfig)
			if ok {
				mu.Lock()
				receivedMessages = append(receivedMessages, m.Text)
				mu.Unlock()
				done <- true // Сигнализируем о завершении
			}
			return tgbotapi.Message{}, nil
		}
		bot.serverClient = &mockServerClient{
			startTaskFunc: func(ctx context.Context, files []DocumentFile) (*StartTaskResponse, error) {
				mu.Lock()
				startTaskCalled = true
				mu.Unlock()
				return nil, nil
			},
		}

		chatID := int64(999)
		mediaGroupID := "limit-exceed-group"

		// Отправляем 3 файла, когда лимит 2
		msg1 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, MediaGroupID: mediaGroupID, Document: &tgbotapi.Document{FileID: "file1"}}
		msg2 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, MediaGroupID: mediaGroupID, Document: &tgbotapi.Document{FileID: "file2"}}
		msg3 := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: chatID}, MediaGroupID: mediaGroupID, Document: &tgbotapi.Document{FileID: "file3"}}

		bot.handleDocument(ctx, msg1)
		bot.handleDocument(ctx, msg2)
		bot.handleDocument(ctx, msg3)

		// Ждем получения сообщения или таймаут
		select {
		case <-done:
			// Сообщение получено
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for message to be sent")
		}

		mu.Lock()
		defer mu.Unlock()

		require.Len(t, receivedMessages, 1)
		assert.Contains(t, receivedMessages[0], "Превышен лимит файлов")
		assert.Contains(t, receivedMessages[0], "Вы отправили 3, а разрешено 2")

		// Проверяем, что обработка не была запущена
		assert.False(t, startTaskCalled, "StartTask не должен был быть вызван")

		// Проверяем, что пачка была удалена
		bot.pendingFilesMutex.Lock()
		_, exists := bot.pendingMediaGroups[mediaGroupID]
		bot.pendingFilesMutex.Unlock()
		assert.False(t, exists, "Пачка медиагруппы должна быть удалена после обработки")
	})
}

func TestBot_ProcessFileBatch_Sorting(t *testing.T) {
	defaultConfig := config.BotConfig{
		MaxFilesPerMessage:     3,
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

	// Документы для обработки
	docs := []*tgbotapi.Document{
		{FileID: "file2", FileName: "file2.json"},
		{FileID: "file1", FileName: "file1.json"},
		{FileID: "file3", FileName: "file3.json"},
	}

	go bot.processFileBatch(ctx, 123, docs)

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
