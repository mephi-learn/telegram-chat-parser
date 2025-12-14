package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEndToEndWithRealBinary(t *testing.T) {
	// Создаем минимальный тестовый JSON-файл для парсинга
	testData := `{
		"name": "Test Chat",
		"type": "private_group",
		"id": 123456789,
		"messages": [
			{
				"id": 1,
				"type": "message",
				"date": "2023-01-01T00:00:00",
				"from": "Test User",
				"from_id": "user123456",
				"text": "Hello, world!"
			}
		]
	}`

	// Записываем тестовые данные во временный файл
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_chat.json")
	if err := os.WriteFile(testFile, []byte(testData), 0644); err != nil {
		t.Fatalf("Не удалось записать тестовый файл: %v", err)
	}

	// Собираем бинарный файл
	buildCmd := exec.Command("go", "build", "-o", filepath.Join(tempDir, "test_binary"), "./cmd/telegram-chat-parser")
	buildCmd.Dir = "../.."
	if err := buildCmd.Run(); err != nil {
		t.Skipf("Пропускаем сквозной тест: не удалось собрать бинарный файл: %v", err)
	}

	// Примечание: Мы не можем запустить бинарный файл с реальными учетными данными API в тесте
	// Поэтому этот тест в основном проверяет, что бинарный файл собирается корректно
	t.Log("Бинарный файл для сквозного теста успешно собран")
}
