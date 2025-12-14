package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"time"
)

type TaskStatusResponse struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message,omitempty"`
}

func main() {
	var serverAddr, filePath string
	flag.StringVar(&serverAddr, "server", "http://localhost:8080", "Server address")
	flag.StringVar(&filePath, "file", "", "Path to the chat export file")
	flag.Parse()

	if filePath == "" {
		log.Fatal("File path is required")
	}

	// Чтение файла
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Не удалось прочитать файл: %v", err)
	}

	// Создание многочастной формы для загрузки файла
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "chat.json")
	if err != nil {
		log.Fatalf("Не удалось создать файл формы: %v", err)
	}
	_, err = part.Write(fileData)
	if err != nil {
		log.Fatalf("Не удалось записать данные файла: %v", err)
	}
	writer.Close()

	// Отправка файла на сервер
	resp, err := http.Post(serverAddr+"/api/v1/process", writer.FormDataContentType(), &body)
	if err != nil {
		log.Fatalf("Не удалось отправить запрос: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		log.Fatalf("Сервер вернул статус: %d", resp.StatusCode)
	}

	// Разбор идентификатора задачи из ответа
	var taskResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		log.Fatalf("Не удалось декодировать ответ: %v", err)
	}
	taskID := taskResp["task_id"]
	if taskID == "" {
		log.Fatal("Идентификатор задачи не найден в ответе")
	}

	fmt.Printf("Задача создана с идентификатором: %s\n", taskID)

	// Опрос о статусе задачи
	for {
		time.Sleep(5 * time.Second) // Ожидание 5 секунд перед следующим опросом

		resp, err := http.Get(fmt.Sprintf("%s/api/v1/tasks/%s", serverAddr, taskID))
		if err != nil {
			log.Fatalf("Не удалось опросить статус задачи: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Fatalf("Сервер вернул статус: %d", resp.StatusCode)
		}

		var statusResp TaskStatusResponse
		if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
			log.Fatalf("Не удалось декодировать ответ статуса: %v", err)
		}

		fmt.Printf("Статус задачи: %s\n", statusResp.Status)

		switch statusResp.Status {
		case "completed":
			fmt.Println("Задача выполнена успешно.")
			// Получение и вывод результата.
			resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/tasks/%s/result", serverAddr, taskID))
			if err != nil {
				log.Fatalf("Не удалось получить результат: %v", err)
			}
			defer resultResp.Body.Close()

			if resultResp.StatusCode != http.StatusOK {
				log.Fatalf("Сервер вернул статус для результата: %d", resultResp.StatusCode)
			}

			var resultData []byte
			resultData, err = io.ReadAll(resultResp.Body)
			if err != nil {
				log.Fatalf("Не удалось прочитать тело результата: %v", err)
			}

			fmt.Println("Результат задачи:")
			fmt.Println(string(resultData))
			return
		case "failed":
			fmt.Printf("Задача не выполнена: %s\n", statusResp.ErrorMessage)
			os.Exit(1)
		case "pending", "processing":
			// Продолжение опроса
			continue
		default:
			log.Fatalf("Неизвестный статус задачи: %s", statusResp.Status)
		}
	}
}
