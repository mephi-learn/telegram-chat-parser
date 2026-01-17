package parser

import (
	"bytes"
	"fmt"
	"strings"
	"telegram-chat-parser/internal/domain"
	"telegram-chat-parser/internal/ports"

	"golang.org/x/net/html"
)

// HtmlParser реализует интерфейс Parser для разбора HTML данных.
type HtmlParser struct{}

// NewHtmlParser создает новый экземпляр HtmlParser.
func NewHtmlParser() ports.Parser {
	return &HtmlParser{}
}

// Parse преобразует срез байт с HTML в "сырой" список участников.
func (p *HtmlParser) Parse(data []byte) ([]domain.RawParticipant, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse html: %w", err)
	}

	participants := make(map[string]struct{})
	mentions := make(map[string]struct{})

	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Извлечение имен авторов сообщений
			if n.Data == "div" && hasClass(n, "from_name") {
				var nameBuilder strings.Builder
				// Итерируемся только по прямым дочерним узлам, чтобы не захватить
				// текст из вложенных элементов вроде <span> с датой.
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						nameBuilder.WriteString(c.Data)
					}
				}
				name := strings.TrimSpace(nameBuilder.String())
				if name != "" && name != "Deleted Account" {
					participants[name] = struct{}{}
				}
			}

			// Извлечение имен из сервисных сообщений (например, "User joined group...")
			if n.Data == "div" && hasClass(n, "body") && hasClass(n, "details") {
				text := strings.TrimSpace(extractText(n))
				// Очень упрощенная логика: предполагаем, что имя идет до " joined group"
				if strings.Contains(text, " joined group by link from ") {
					parts := strings.Split(text, " joined group by link from ")
					if len(parts) > 0 {
						name := parts[0]
						if name != "" && name != "Deleted Account" {
							participants[name] = struct{}{}
						}
					}
				}
			}

			// Извлечение упоминаний (mentions)
			if n.Data == "a" {
				href := getAttr(n, "href")
				if strings.HasPrefix(href, "https://t.me/") {
					// Проверяем, что это упоминание, а не ссылка на канал/сообщение
					isMention := false
					for _, attr := range n.Attr {
						if attr.Key == "href" {
							// Упоминания в HTML часто ведут на профиль пользователя
							// и начинаются с @ в тексте.
							linkText := strings.TrimSpace(extractText(n))
							if strings.HasPrefix(linkText, "@") {
								isMention = true
								break
							}
						}
					}

					if isMention {
						username := strings.TrimSpace(extractText(n))
						if username != "" {
							mentions[username] = struct{}{}
						}
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}

	f(doc)

	var rawParticipants []domain.RawParticipant
	for name := range participants {
		rawParticipants = append(rawParticipants, domain.RawParticipant{Name: name})
	}
	for username := range mentions {
		rawParticipants = append(rawParticipants, domain.RawParticipant{Username: username})
	}

	return rawParticipants, nil
}

// hasClass проверяет, есть ли у ноды указанный CSS класс.
func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			classes := strings.Fields(a.Val)
			for _, c := range classes {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// getAttr получает значение атрибута по ключу.
func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// extractText рекурсивно извлекает весь текст из ноды и ее дочерних элементов.
func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type != html.ElementNode {
		return ""
	}
	var buf bytes.Buffer
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		buf.WriteString(extractText(c))
	}
	return buf.String()
}
