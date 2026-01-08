package bot

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/xuri/excelize/v2"

	"telegram-chat-parser/cmd/bot/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	startCommand = "start"
)

// Bot представляет собой основной объект Telegram-бота.
type Bot struct {
	api          *tgbotapi.BotAPI
	cfg          config.BotConfig
	serverClient *ServerClient
	taskStore    *TaskStore
	logger       *slog.Logger
}

// NewBot создает и инициализирует новый экземпляр бота.
func NewBot(cfg config.BotConfig, serverClient *ServerClient, taskStore *TaskStore, logger *slog.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot api: %w", err)
	}
	// api.Debug = true // Включаем для отладки

	logger.Info("Authorized on account", slog.String("username", api.Self.UserName))

	return &Bot{
		api:          api,
		cfg:          cfg,
		serverClient: serverClient,
		taskStore:    taskStore,
		logger:       logger,
	}, nil
}

// Start запускает основной цикл обработки обновлений от Telegram.
func (b *Bot) Start(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("Context cancelled, stopping bot...")
			b.api.StopReceivingUpdates()
			return
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			b.handleMessage(ctx, update.Message)
		}
	}
}

// handleMessage обрабатывает входящее сообщение.
func (b *Bot) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	if msg.IsCommand() {
		b.handleCommand(ctx, msg)
		return
	}

	if msg.Document != nil {
		b.handleDocument(ctx, msg)
		return
	}

	// Ответ на любые другие сообщения
	reply := tgbotapi.NewMessage(msg.Chat.ID, "Пожалуйста, отправьте мне JSON-файл с историей чата, выгруженный из Telegram.")
	b.sendMessage(reply)
}

// handleCommand обрабатывает команды.
func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch msg.Command() {
	case startCommand:
		replyText := "Добро пожаловать! Я бот для анализа истории чатов Telegram.\n\n" +
			"Просто отправьте мне один JSON-файл с историей, и я извлеку список участников.\n\n" +
			"Пожалуйста, обратите внимание:\n" +
			"• Я принимаю только один файл за раз.\n" +
			"• Файлы не сохраняются на сервере и обрабатываются на лету."
		reply := tgbotapi.NewMessage(msg.Chat.ID, replyText)
		b.sendMessage(reply)
	default:
		reply := tgbotapi.NewMessage(msg.Chat.ID, "Я не знаю такой команды.")
		b.sendMessage(reply)
	}
}

// handleDocument обрабатывает входящий документ (файл).
func (b *Bot) handleDocument(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	logger := b.logger.With(slog.Int64("chat_id", chatID))

	// 1. Проверяем, нет ли уже активной задачи.
	if _, ok := b.taskStore.Get(chatID); ok {
		logger.Warn("user tried to start a new task while another is active")
		reply := tgbotapi.NewMessage(chatID, "Пожалуйста, подождите завершения предыдущей задачи, прежде чем начинать новую.")
		b.sendMessage(reply)
		return
	}

	// 2. Скачиваем файл.
	fileURL, err := b.api.GetFileDirectURL(msg.Document.FileID)
	if err != nil {
		logger.Error("failed to get file direct url", slog.String("error", err.Error()))
		reply := tgbotapi.NewMessage(chatID, "Не удалось получить доступ к файлу. Попробуйте отправить его еще раз.")
		b.sendMessage(reply)
		return
	}

	resp, err := http.Get(fileURL)
	if err != nil {
		logger.Error("failed to download file", slog.String("error", err.Error()))
		reply := tgbotapi.NewMessage(chatID, "Не удалось скачать файл. Попробуйте отправить его еще раз.")
		b.sendMessage(reply)
		return
	}
	defer resp.Body.Close()

	// 3. Запускаем задачу на бэкенде.
	startResp, err := b.serverClient.StartTask(ctx, msg.Document.FileName, resp.Body)
	if err != nil {
		logger.Error("failed to start task on backend", slog.String("error", err.Error()))
		reply := tgbotapi.NewMessage(chatID, "Не удалось начать обработку файла на сервере. Пожалуйста, попробуйте позже.")
		b.sendMessage(reply)
		return
	}

	taskID := startResp.TaskID
	logger = logger.With(slog.String("task_id", taskID))
	logger.Info("task started on backend")

	// 4. Сохраняем task_id и запускаем опрос.
	b.taskStore.Set(chatID, taskID)
	go b.pollTaskStatus(context.Background(), chatID, taskID) // Используем новый контекст для фоновой задачи

	reply := tgbotapi.NewMessage(chatID, "✅ Файл получен и поставлен в очередь на обработку. Ожидайте результата.")
	b.sendMessage(reply)
}

func (b *Bot) sendMessage(msg tgbotapi.Chattable) {
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Error("failed to send message", slog.String("error", err.Error()))
	}
}

// pollTaskStatus асинхронно опрашивает статус задачи на бэкенд-сервере.
func (b *Bot) pollTaskStatus(ctx context.Context, chatID int64, taskID string) {
	logger := b.logger.With(slog.Int64("chat_id", chatID), slog.String("task_id", taskID))
	defer b.taskStore.Delete(chatID) // Гарантированно удаляем задачу по завершении.

	ticker := time.NewTicker(time.Duration(b.cfg.PollingIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Warn("polling cancelled by context")
			return
		case <-ticker.C:
			logger.Debug("polling task status")
			status, err := b.serverClient.GetTaskStatus(ctx, taskID)
			if err != nil {
				logger.Error("failed to get task status", slog.String("error", err.Error()))
				// Можно добавить логику ретраев или просто прекратить опрос
				continue
			}

			switch status.Status {
			case "completed":
				logger.Info("task completed")
				b.processCompletedTask(ctx, chatID, taskID)
				return // Завершаем опрос
			case "failed":
				logger.Warn("task failed", slog.String("reason", status.ErrorMessage))
				reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("Произошла ошибка при обработке файла: %s", status.ErrorMessage))
				b.sendMessage(reply)
				return // Завершаем опрос
			case "pending", "processing":
				logger.Debug("task is in progress", slog.String("status", status.Status))
				// Продолжаем опрос
			default:
				logger.Warn("unknown task status", slog.String("status", status.Status))
			}
		}
	}
}

// processCompletedTask обрабатывает успешно завершенную задачу.
func (b *Bot) processCompletedTask(ctx context.Context, chatID int64, taskID string) {
	logger := b.logger.With(slog.Int64("chat_id", chatID), slog.String("task_id", taskID))
	logger.Info("fetching results for completed task")

	users, err := b.fetchAllResults(ctx, taskID)
	if err != nil {
		logger.Error("failed to fetch all results", slog.String("error", err.Error()))
		reply := tgbotapi.NewMessage(chatID, "Не удалось получить результаты для выполненной задачи. Пожалуйста, попробуйте позже.")
		b.sendMessage(reply)
		return
	}

	logger.Info("successfully fetched all results", slog.Int("user_count", len(users)))

	if len(users) == 0 {
		reply := tgbotapi.NewMessage(chatID, "Не удалось найти участников в предоставленном файле.")
		b.sendMessage(reply)
		return
	}

	// Логика ветвления в зависимости от количества участников
	if len(users) >= b.cfg.ExcelThreshold {
		logger.Info("user count is over threshold, sending excel file")
		b.sendMessage(tgbotapi.NewMessage(chatID, fmt.Sprintf("Найдено %d участников. Формирую Excel-файл...", len(users))))
		b.sendExcelResult(chatID, users)
	} else {
		logger.Info("user count is under threshold, sending text message")
		b.sendTextResult(chatID, users)
	}
}

// fetchAllResults собирает все страницы с результатами для данной задачи.
func (b *Bot) fetchAllResults(ctx context.Context, taskID string) ([]UserDTO, error) {
	var allUsers []UserDTO
	page := 1
	pageSize := 100 // Запрашиваем по 100, чтобы уменьшить количество запросов

	for {
		result, err := b.serverClient.GetTaskResult(ctx, taskID, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("failed to get task result page %d: %w", page, err)
		}

		allUsers = append(allUsers, result.Data...)

		if page >= result.Pagination.TotalPages {
			break // Все страницы собраны
		}
		page++
	}

	return allUsers, nil
}

func (b *Bot) sendExcelResult(chatID int64, users []UserDTO) {
	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			b.logger.Error("failed to close excel file", slog.String("error", err.Error()))
		}
	}()

	sheetName := "Участники"
	index, _ := f.NewSheet(sheetName)
	f.SetActiveSheet(index)

	// Заголовки
	headers := []string{"Дата экспорта", "Username", "Имя и фамилия", "Описание (Bio)"}
	showChannel := hasChannelData(users)
	if showChannel {
		headers = append(headers, "Канал")
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
	}

	// Данные
	exportDate := time.Now().Format(time.RFC3339)
	for i, user := range users {
		row := i + 2
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), exportDate)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), user.Username)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), user.Name)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), user.Bio)
		if showChannel {
			f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), user.Channel)
		}
	}

	// Запись в буфер
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		b.logger.Error("failed to write excel to buffer", slog.String("error", err.Error()))
		b.sendMessage(tgbotapi.NewMessage(chatID, "Не удалось сгенерировать Excel-файл."))
		return
	}

	// Отправка файла
	fileName := fmt.Sprintf("chat_participants_%s.xlsx", time.Now().Format("2006-01-02_15-04-05"))
	fileBytes := tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	}

	msg := tgbotapi.NewDocument(chatID, fileBytes)
	msg.Caption = fmt.Sprintf("Анализ завершен. Найдено %d участников.", len(users))
	b.sendMessage(msg)
}

// sendTextResult форматирует и отправляет результат в виде текстового сообщения HTML.
func (b *Bot) sendTextResult(chatID int64, users []UserDTO) {
	if len(users) == 0 {
		reply := tgbotapi.NewMessage(chatID, "Не найдено ни одного пользователя.")
		b.sendMessage(reply)
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Найдено %d участников. Вот список:\n", len(users)))
	sb.WriteString("<pre><code>") // Используем HTML для надежного форматирования

	// Получаем ширину колонок из конфигурации
	userColWidth := b.cfg.Render.User
	nameColWidth := b.cfg.Render.Name
	bioColWidth := b.cfg.Render.Bio
	channelColWidth := b.cfg.Render.Channel

	showChannel := hasChannelData(users)

	// Формируем заголовок
	headerUser := "Username"
	headerName := "Name"
	headerBio := "Bio"
	headerChannel := "Channel"

	headerLine := fmt.Sprintf("| %s%s | %s%s | %s%s ",
		headerUser, strings.Repeat(" ", userColWidth-len(headerUser)),
		headerName, strings.Repeat(" ", nameColWidth-len(headerName)),
		headerBio, strings.Repeat(" ", bioColWidth-len(headerBio)),
	)
	if showChannel {
		headerLine += fmt.Sprintf("| %s%s ", headerChannel, strings.Repeat(" ", channelColWidth-len(headerChannel)))
	}
	headerLine += "|\n"
	sb.WriteString(headerLine)

	// Формируем разделитель
	separatorLine := fmt.Sprintf("|%s|%s|%s",
		strings.Repeat("-", userColWidth+2),
		strings.Repeat("-", nameColWidth+2),
		strings.Repeat("-", bioColWidth+2),
	)
	if showChannel {
		separatorLine += fmt.Sprintf("|%s", strings.Repeat("-", channelColWidth+2))
	}
	separatorLine += "|\n"
	sb.WriteString(separatorLine)

	for _, user := range users {
		username := "n/a"
		if user.Username != "" {
			username = "@" + user.Username
		}

		// 1. Очищаем данные
		cleanName := strings.ToValidUTF8(user.Name, "")
		cleanBio := strings.ToValidUTF8(user.Bio, "")

		// 2. Экранируем и убираем исходные переносы
		name := html.EscapeString(cleanName)
		name = strings.ReplaceAll(name, "\n", " ")
		bio := html.EscapeString(cleanBio)
		bio = strings.ReplaceAll(bio, "\n", " ")

		// 3. Разбиваем строки на несколько с переносом слов
		usernameLines := wrapString(username, userColWidth)
		nameLines := wrapString(name, nameColWidth)
		bioLines := wrapString(bio, bioColWidth)
		var channelLines []string
		if showChannel {
			cleanChannel := strings.ToValidUTF8(user.Channel, "")
			channel := html.EscapeString(cleanChannel)
			channel = strings.ReplaceAll(channel, "\n", " ")
			channelLines = wrapString(channel, channelColWidth)
		}

		maxLines := len(usernameLines)
		if len(nameLines) > maxLines {
			maxLines = len(nameLines)
		}
		if len(bioLines) > maxLines {
			maxLines = len(bioLines)
		}
		if len(channelLines) > maxLines {
			maxLines = len(channelLines)
		}

		// 4. Печатаем строки для текущего пользователя
		for i := 0; i < maxLines; i++ {
			userPart := ""
			if i < len(usernameLines) {
				userPart = usernameLines[i]
			}

			namePart := ""
			if i < len(nameLines) {
				namePart = nameLines[i]
			}

			bioPart := ""
			if i < len(bioLines) {
				bioPart = bioLines[i]
			}

			channelPart := ""
			if i < len(channelLines) {
				channelPart = channelLines[i]
			}

			// Добиваем пробелами до нужной ширины
			padUser := generatePadding(userPart, userColWidth)
			padName := generatePadding(namePart, nameColWidth)
			padBio := generatePadding(bioPart, bioColWidth)

			line := fmt.Sprintf("| %s%s | %s%s | %s%s ", userPart, padUser, namePart, padName, bioPart, padBio)
			if showChannel {
				padChannel := generatePadding(channelPart, channelColWidth)
				line += fmt.Sprintf("| %s%s ", channelPart, padChannel)
			}
			line += "|\n"
			sb.WriteString(line)
		}
	}
	sb.WriteString("</code></pre>")

	text := sb.String()
	reply := tgbotapi.NewMessage(chatID, text)
	reply.ParseMode = tgbotapi.ModeHTML

	// Проверка на максимальную длину сообщения в Telegram (4096 символов)
	if len(text) > 4096 {
		b.logger.Warn("сгенерированный текст слишком длинный, отправка в виде файла", "length", len(text))
		b.sendResultAsTextFile(chatID, users)
		return
	}

	if _, err := b.api.Send(reply); err != nil {
		b.logger.Error("не удалось отправить текстовый результат", "error", err.Error())
	}
}

// generatePadding вычисляет отступ для строки с учетом поправки на CJK-символы.
func generatePadding(s string, colWidth int) string {
	paddingNeeded := colWidth - runewidth.StringWidth(s)

	// Прагматическая поправка: если в строке есть CJK-символы, добавляем один пробел,
	// чтобы компенсировать ошибку рендеринга в некоторых клиентах.
	hasCJK := false
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			hasCJK = true
			break
		}
	}

	if hasCJK && paddingNeeded >= 0 {
		paddingNeeded++
	}

	if paddingNeeded > 0 {
		return strings.Repeat(" ", paddingNeeded)
	}
	return ""
}

// wrapString wraps a given string to a specified width using runewidth.
// It prioritizes wrapping on word boundaries (spaces). If a single word is
// longer than the width, it will be broken mid-word.
func wrapString(s string, width int) []string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return []string{s}
	}

	var lines []string
	words := strings.Fields(s)

	if len(words) == 0 { // Handles strings with only spaces or empty strings
		runes := []rune(s)
		for len(runes) > 0 {
			i := 0
			currentWidth := 0
			for i < len(runes) {
				runeWidth := runewidth.RuneWidth(runes[i])
				if currentWidth+runeWidth > width {
					break
				}
				currentWidth += runeWidth
				i++
			}
			lines = append(lines, string(runes[:i]))
			runes = runes[i:]
		}
		if len(lines) == 0 {
			return []string{""}
		}
		return lines
	}

	var currentLine strings.Builder
	for _, word := range words {
		wordWidth := runewidth.StringWidth(word)

		// Handle words longer than the entire width
		if wordWidth > width {
			if currentLine.Len() > 0 {
				lines = append(lines, currentLine.String())
				currentLine.Reset()
			}

			runes := []rune(word)
			for len(runes) > 0 {
				i := 0
				currentWidth := 0
				for i < len(runes) {
					runeWidth := runewidth.RuneWidth(runes[i])
					if currentWidth+runeWidth > width {
						break
					}
					currentWidth += runeWidth
					i++
				}
				lines = append(lines, string(runes[:i]))
				runes = runes[i:]
			}
			continue
		}

		// If the word doesn't fit on the current line, start a new one
		lineLen := runewidth.StringWidth(currentLine.String())
		if lineLen > 0 && lineLen+1+wordWidth > width {
			lines = append(lines, currentLine.String())
			currentLine.Reset()
		}

		if currentLine.Len() > 0 {
			currentLine.WriteString(" ")
		}
		currentLine.WriteString(word)
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	return lines
}

// hasChannelData проверяет, есть ли в срезе пользователей хотя бы одна запись с непустым полем Channel.
func hasChannelData(users []UserDTO) bool {
	for _, user := range users {
		if user.Channel != "" {
			return true
		}
	}
	return false
}

// sendResultAsTextFile отправляет список пользователей в виде текстового файла.
func (b *Bot) sendResultAsTextFile(chatID int64, users []UserDTO) {
	var buf bytes.Buffer
	showChannel := hasChannelData(users)

	// Заголовки для файла
	headers := []string{"Username", "Name", "Bio"}
	if showChannel {
		headers = append(headers, "Channel")
	}
	buf.WriteString(strings.Join(headers, ","))
	buf.WriteString("\n")

	for _, user := range users {
		// Форматируем как CSV для простоты
		record := []string{
			fmt.Sprintf("\"@%s\"", user.Username),
			fmt.Sprintf("\"%s\"", strings.ReplaceAll(user.Name, "\"", "\"\"")),
			fmt.Sprintf("\"%s\"", strings.ReplaceAll(user.Bio, "\"", "\"\"")),
		}
		if showChannel {
			record = append(record, fmt.Sprintf("\"%s\"", strings.ReplaceAll(user.Channel, "\"", "\"\"")))
		}
		buf.WriteString(strings.Join(record, ","))
		buf.WriteString("\n")
	}

	fileName := fmt.Sprintf("chat_participants_%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	fileBytes := tgbotapi.FileBytes{
		Name:  fileName,
		Bytes: buf.Bytes(),
	}

	msg := tgbotapi.NewDocument(chatID, fileBytes)
	msg.Caption = fmt.Sprintf("Анализ завершен. Найдено %d участников. Список слишком большой для одного сообщения, поэтому он прикреплен в виде файла.", len(users))
	b.sendMessage(msg)
}
