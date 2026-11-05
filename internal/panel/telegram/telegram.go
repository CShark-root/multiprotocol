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

// Button is one inline keyboard button; Data comes back in a callback.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// Keyboard is rows of inline buttons.
type Keyboard [][]Button

func (b *Bot) post(ctx context.Context, method string, payload any, out any) error {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, b.url(method), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return b.do(req, out)
}

func markup(kb Keyboard) map[string]any {
	if kb == nil {
		kb = Keyboard{}
	}
	return map[string]any{"inline_keyboard": kb}
}

// SendMenu posts a message with inline buttons and returns its id.
func (b *Bot) SendMenu(ctx context.Context, chat int64, text string, kb Keyboard) (int64, error) {
	var msg struct {
		ID int64 `json:"message_id"`
	}
	err := b.post(ctx, "sendMessage", map[string]any{
		"chat_id": chat, "text": text, "disable_web_page_preview": true, "reply_markup": markup(kb),
	}, &msg)
	return msg.ID, err
}

// EditMenu replaces the text and buttons of a message in place.
func (b *Bot) EditMenu(ctx context.Context, chat, msgID int64, text string, kb Keyboard) error {
	err := b.post(ctx, "editMessageText", map[string]any{
		"chat_id": chat, "message_id": msgID, "text": text, "disable_web_page_preview": true, "reply_markup": markup(kb),
	}, nil)
	if err != nil && strings.Contains(err.Error(), "message is not modified") {
		return nil
	}
	return err
}

// Answer acknowledges a button press; text, when set, shows as a toast.
func (b *Bot) Answer(ctx context.Context, callbackID, text string) error {
	return b.post(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID, "text": text}, nil)
}

// Update is the part of a Telegram update the panel uses.
type Update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		ID   int64  `json:"message_id"`
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
	Callback *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			ID   int64 `json:"message_id"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// Updates long-polls for new updates after offset.
func (b *Bot) Updates(ctx context.Context, offset int64, wait time.Duration) ([]Update, error) {
	var ups []Update
	err := b.post(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": int(wait.Seconds()), "allowed_updates": []string{"message", "callback_query"},
	}, &ups)
	return ups, err
}
