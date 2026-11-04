// Package telegram is a minimal Telegram Bot API client: send a message or a
// file, and find the chat that wrote to the bot (to learn its id).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tokenRe = regexp.MustCompile(`^\d{5,12}:[A-Za-z0-9_-]{30,50}$`)

// ValidToken checks the shape of a token from @BotFather.
func ValidToken(t string) bool { return tokenRe.MatchString(t) }

type Bot struct {
	Token string
	http  *http.Client
}

func New(token string) *Bot { return &Bot{Token: token, http: &http.Client{Timeout: 60 * time.Second}} }

func (b *Bot) url(method string) string {
	return "https://api.telegram.org/bot" + b.Token + "/" + method
}

type reply struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (b *Bot) do(req *http.Request, out any) error {
	resp, err := b.http.Do(req)
	if err != nil {
		// The token is part of the URL: keep it out of error messages.
		return errors.New("telegram: " + strings.ReplaceAll(err.Error(), b.Token, "***"))
	}
	defer resp.Body.Close()
	var r reply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	if !r.OK {
		return errors.New("telegram: " + r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// Send posts a plain-text message.
func (b *Bot) Send(ctx context.Context, chat int64, text string) error {
	body, _ := json.Marshal(map[string]any{"chat_id": chat, "text": text, "disable_web_page_preview": true})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.url("sendMessage"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return b.do(req, nil)
}

// SendFile uploads a document with a caption.
func (b *Bot) SendFile(ctx context.Context, chat int64, name string, data []byte, caption string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	w.WriteField("caption", caption)
	fw, _ := w.CreateFormFile("document", name)
	fw.Write(data)
	w.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.url("sendDocument"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return b.do(req, nil)
}

// Chat is who last wrote to the bot.
type Chat struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// FindChat looks at recent updates for the newest private chat, so the admin
// only has to send /start to the bot.
func (b *Bot) FindChat(ctx context.Context) (*Chat, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.url("getUpdates"), nil)
	var updates []struct {
		Message *struct {
			Chat struct {
				ID        int64  `json:"id"`
				Type      string `json:"type"`
				FirstName string `json:"first_name"`
				Username  string `json:"username"`
			} `json:"chat"`
		} `json:"message"`
	}
	if err := b.do(req, &updates); err != nil {
		return nil, err
	}
	for i := len(updates) - 1; i >= 0; i-- {
		if m := updates[i].Message; m != nil && m.Chat.Type == "private" {
			name := m.Chat.FirstName
			if m.Chat.Username != "" {
				name += " (@" + m.Chat.Username + ")"
			}
			return &Chat{ID: m.Chat.ID, Name: strings.TrimSpace(name)}, nil
		}
	}
	return nil, errors.New("no message yet: open the bot in Telegram and send /start")
}
