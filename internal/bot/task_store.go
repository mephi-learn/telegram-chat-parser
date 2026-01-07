package bot

import "sync"

// TaskStore — это потокобезопасное in-memory хранилище для сопоставления
// идентификатора чата Telegram с идентификатором задачи на бэкенд-сервере.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[int64]string // map[chatID]taskID
}

// NewTaskStore создает новый экземпляр TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[int64]string),
	}
}

// Set сохраняет сопоставление chatID и taskID.
// Если для данного chatID уже существует задача, она будет перезаписана.
func (s *TaskStore) Set(chatID int64, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[chatID] = taskID
}

// Get извлекает taskID для указанного chatID.
// Возвращает taskID и true, если задача найдена, иначе — пустую строку и false.
func (s *TaskStore) Get(chatID int64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	taskID, ok := s.tasks[chatID]
	return taskID, ok
}

// Delete удаляет задачу для указанного chatID.
func (s *TaskStore) Delete(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, chatID)
}
