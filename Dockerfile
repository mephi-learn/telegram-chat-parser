# Dockerfile for telegram-chat-parser

# Этап сборки
FROM golang:1.25-alpine AS builder

# Устанавливаем рабочую директорию
WORKDIR /app

# Копируем файлы go.mod и go.sum для кэширования зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем остальной исходный код
COPY . .

# Собираем бинарник сервера со статичной линковкой и без отладочной информации
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /server ./cmd/server/main.go

# Собираем бинарник бота
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /bot ./cmd/bot/main.go


# Финальный этап для сервера
FROM alpine:latest AS server
WORKDIR /app
# Копируем бинарник из этапа сборки
COPY --from=builder /server .
# Копируем CA-сертификаты для HTTPS-запросов
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Команда для запуска
CMD ["/app/server"]


# Финальный этап для бота
FROM alpine:latest AS bot
WORKDIR /app
# Копируем бинарник из этапа сборки
COPY --from=builder /bot .
# Копируем CA-сертификаты для HTTPS-запросов
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Команда для запуска
CMD ["/app/bot"]
