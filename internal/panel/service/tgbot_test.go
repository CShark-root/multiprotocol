package service

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"panel/internal/panel/store"
	"panel/internal/panel/telegram"
)

// htmlOK проверяет, что теги в тексте закрыты: иначе Telegram отвергнет сообщение.
func htmlOK(t *testing.T, name, text string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader("<r>" + text + "</r>"))
	d.Strict = true
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s: broken HTML: %v\n%s", name, err, text)
		}
	}
}

func TestBotScreens(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ev := NewEvents(st)
	users := NewUsers(st, nil)
	exp := time.Now().AddDate(0, 0, 2)
	u, err := users.Create(ctx, 0, UserInput{Name: "user1", Note: "заметка <&>", Enabled: true, TrafficLimit: 10 << 30, ResetPeriod: "month", ExpireAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	st.AddTraffic(ctx, map[int64]int64{u.ID: 9500 << 20, 0: 3 << 30}, time.Now())
	b := NewBot(BotDeps{Notifier: NewNotifier(st, ev, t.TempDir(), "test.example", log), Users: users, Store: st,
		Billing: NewBilling(st, ev, log), SubBase: "https://x/sub/", Log: log})

	id := itoa64(u.ID)
	for _, data := range []string{"m", "u:a:0", "u:on:0", "u:w:0", "u:off:0", "c:" + id, "l:" + id, "d:" + id,
		"sv", "tr:1", "tr:7", "tr:30", "am", "mt", "rc", "rb", "up", "nw"} {
		text, kb := b.screen(ctx, data)
		htmlOK(t, data, text)
		for _, row := range kb {
			for _, k := range row {
				if len(k.Data) > 64 {
					t.Fatalf("%s: callback data too long: %q", data, k.Data)
				}
			}
		}
	}
	if text, _ := b.screen(ctx, "m"); !strings.Contains(text, "user1: доступ кончится через 2 дня") {
		t.Fatalf("main screen must warn about the expiry:\n%s", text)
	}
	if text, _ := b.screen(ctx, "c:"+id); !strings.Contains(text, "▰▰▰▰▰▰▰▰▰") || !strings.Contains(text, "заметка &lt;&amp;&gt;") {
		t.Fatalf("card must show the traffic bar:\n%s", text)
	}

	// Мастер: имя текстом → лимит → срок.
	b.onText(ctx, "/new")
	if text, _ := b.onText(ctx, "user two"); !strings.Contains(text, "без пробелов") {
		t.Fatalf("a name with a space must be asked again:\n%s", text)
	}
	text, _ := b.onText(ctx, "user2")
	htmlOK(t, "nl", text)
	data := "nl:50"
	b.act(ctx, telegram.New("1:x"), TelegramSettings{}, &data)
	if data != "nt" || b.wiz.limit != 50<<30 {
		t.Fatalf("limit step: %q %+v", data, b.wiz)
	}
	// Поиск по части имени открывает карточку.
	if text, _ := b.onText(ctx, "USER1"); !strings.Contains(text, "Трафик") {
		t.Fatalf("search must open the card:\n%s", text)
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
