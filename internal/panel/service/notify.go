package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/backup"
	"panel/internal/panel/store"
	"panel/internal/panel/telegram"
	"panel/internal/panel/validate"
)

const telegramKey = "telegram"

// TelegramSettings: the bot, the chat it writes to, which events it
// reports, and the daily encrypted backup.
type TelegramSettings struct {
	Token          string    `json:"token"` // never sent back to the UI
	ChatID         int64     `json:"chatId"`
	ChatName       string    `json:"chatName"`
	Events         []string  `json:"events"`
	DailyBackup    bool      `json:"dailyBackup"`
	Control        bool      `json:"control"`        // the bot answers with a management menu
	BackupPassword string    `json:"backupPassword"` // never sent back
	LastBackup     time.Time `json:"lastBackup"`
	LastError      string    `json:"lastError"`
}

// notifyAlias: событие → переключатель, которым оно включается.
var notifyAlias = map[string]string{
	EventWarpRecovered: EventWarpRestarted,
	EventCoresUpdated:  EventCoreUp,
	EventCoresRollback: EventCoreDown,
	EventNodeDown:      EventCoreDown,
	EventNodeUp:        EventCoreUp,
}

// NotifyEvents are the event types a bot can report, in display order.
var NotifyEvents = []string{
	EventCoreDown, EventCoreUp, EventUserLimited, EventUserExpired, EventUserRestored,
	EventLogin, EventLoginFailed, EventLoginThrottle, EventCertIssued, EventCertFailed,
	EventGeoFailed, EventHopFailed, EventAmneziaDown, EventAmneziaUp, EventWarpRestarted,
}

var defaultNotify = []string{EventCoreDown, EventCoreUp, EventUserLimited, EventUserExpired, EventLogin, EventLoginThrottle, EventCertFailed, EventGeoFailed, EventHopFailed, EventAmneziaDown, EventAmneziaUp, EventWarpRestarted}

// Notifier sends events to Telegram and the daily backup.
type Notifier struct {
	store   *store.Store
	dataDir string
	host    string // shown in messages, to tell servers apart
	log     *slog.Logger
	mu      sync.Mutex
}

func NewNotifier(st *store.Store, events *Events, dataDir, host string, log *slog.Logger) *Notifier {
	n := &Notifier{store: st, dataDir: dataDir, host: host, log: log}
	events.Listen(n.onEvent)
	return n
}

func (n *Notifier) Get(ctx context.Context) TelegramSettings {
	s := TelegramSettings{Events: defaultNotify}
	n.store.GetSetting(ctx, telegramKey, &s)
	return s
}

// Save stores the settings; empty token or backup password keep the saved
// ones.
func (n *Notifier) Save(ctx context.Context, adminID int64, in TelegramSettings) (TelegramSettings, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	cur := n.Get(ctx)
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" {
		in.Token = cur.Token
	}
	if in.Token != "" && !telegram.ValidToken(in.Token) {
		return cur, validate.Fail("token", "a token from @BotFather looks like 123456789:AAE…")
	}
	if in.BackupPassword == "" {
		in.BackupPassword = cur.BackupPassword
	}
	if in.DailyBackup && len(in.BackupPassword) < 8 {
		return cur, validate.Fail("backupPassword", "at least 8 characters: Telegram copies are encrypted with it")
	}
	var evs []string
	for _, e := range in.Events {
		if slices.Contains(NotifyEvents, e) && !slices.Contains(evs, e) {
			evs = append(evs, e)
		}
	}
	in.Events = evs
	in.LastBackup, in.LastError = cur.LastBackup, cur.LastError
	if in.ChatID == 0 {
		in.ChatID, in.ChatName = cur.ChatID, cur.ChatName
	}
	if err := n.store.SetSetting(ctx, telegramKey, in); err != nil {
		return cur, err
	}
	writeAudit(ctx, n.store, time.Now(), adminID, "telegram.update", "telegram", 0, map[string]any{"events": len(evs), "dailyBackup": in.DailyBackup})
	return in, nil
}

// FindChat learns the chat id from the newest message to the bot.
func (n *Notifier) FindChat(ctx context.Context, token string) (*telegram.Chat, error) {
	if token == "" {
		token = n.Get(ctx).Token
	}
	if !telegram.ValidToken(token) {
		return nil, validate.Fail("token", "enter the bot token first")
	}
	return telegram.New(token).FindChat(ctx)
}

func (n *Notifier) ready(s TelegramSettings) bool { return s.Token != "" && s.ChatID != 0 }

func (n *Notifier) send(ctx context.Context, s TelegramSettings, text string) error {
	return telegram.New(s.Token).Send(ctx, s.ChatID, "MultiProtocol · "+n.host+"\n"+text)
}

// Test sends a test message.
func (n *Notifier) Test(ctx context.Context) error {
	s := n.Get(ctx)
	if !n.ready(s) {
		return validate.Fail("chatId", "save the token and find the chat first")
	}
	return n.send(ctx, s, "✅ Уведомления работают.")
}

func (n *Notifier) onEvent(e store.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := n.Get(ctx)
	// Парные события включаются тем же переключателем, что и основное.
	typ := e.Type
	if v, ok := notifyAlias[typ]; ok {
		typ = v
	}
	// Напоминания об оплате админ включил сам, задав дату, — шлём всегда.
	if typ == EventBillingDue {
		typ = ""
	}
	if !n.ready(s) || (typ != "" && !slices.Contains(s.Events, typ)) {
		return
	}
	name := ""
	if v, ok := e.Data["name"].(string); ok {
		name = v
	} else if e.UserID > 0 {
		if u, err := n.store.GetUser(ctx, e.UserID); err == nil {
			name = u.Name
		}
	}
	if err := n.send(ctx, s, eventText(e, name)); err != nil {
		n.log.Warn("telegram notification", "err", err)
	}
}

func str(d map[string]any, k string) string {
	if v, ok := d[k]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// eventText is the message for an event, in Russian (the panel's audience).
func eventText(e store.Event, name string) string {
	d := e.Data
	switch e.Type {
	case EventCoreDown:
		return "⚠️ Ядро " + str(d, "core") + " остановилось: " + e.Message
	case EventCoreUp:
		return "✅ Ядро " + str(d, "core") + " снова работает"
	case EventUserLimited:
		return "⛔ " + name + ": закончился трафик, доступ приостановлен"
	case EventUserExpired:
		return "⌛ " + name + ": истёк срок доступа"
	case EventUserRestored:
		return "✅ " + name + ": доступ снова работает"
	case EventLogin:
		return "🔑 Вход в панель: " + str(d, "admin") + " с " + str(d, "ip")
	case EventLoginFailed:
		return "❗ Неудачный вход как «" + str(d, "username") + "» с " + str(d, "ip")
	case EventLoginThrottle:
		return "🚫 Подбор пароля с " + str(d, "ip") + " — вход временно заблокирован"
	case EventCertIssued:
		return "🔒 Сертификат " + str(d, "domain") + " выпущен, действует до " + str(d, "until")
	case EventCertFailed:
		return "⚠️ Не удалось получить сертификат " + str(d, "domain") + ": " + str(d, "error")
	case EventGeoFailed:
		return "⚠️ Не обновились гео-базы: " + str(d, "error")
	case EventHopFailed:
		return "⚠️ Port hopping не включён: " + str(d, "error")
	case EventAmneziaDown:
		return "⚠️ Контейнер Amnezia " + str(d, "container") + " остановился: " + e.Message
	case EventWarpRestarted:
		if ep := str(d, "endpoint"); ep != "" {
			return "🔄 WARP не отвечал — переключаю на " + ep + ": " + e.Message
		}
		return "🔄 WARP не отвечал — Xray перезапущен: " + e.Message
	case EventBillingDue:
		return billingText(d)
	case EventNodeDown:
		return "⚠️ Сервер «" + str(d, "node") + "» не выходит на связь"
	case EventNodeUp:
		return "✅ Сервер «" + str(d, "node") + "» снова на связи"
	case EventCoresUpdated:
		return "⬆️ " + str(d, "core") + " обновлён: " + str(d, "from") + " → " + str(d, "to")
	case EventCoresRollback:
		return "↩️ " + str(d, "core") + " " + str(d, "to") + " не запустился — возвращена " + str(d, "from")
	case EventWarpRecovered:
		return "✅ WARP снова работает (" + str(d, "endpoint") + ", перерыв " + str(d, "minutes") + " мин)"
	case EventAmneziaUp:
		return "✅ Контейнер Amnezia " + str(d, "container") + " снова работает"
	}
	return e.Type
}

// BackupNow sends an encrypted backup to the chat.
func (n *Notifier) BackupNow(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.Get(ctx)
	if !n.ready(s) {
		return validate.Fail("chatId", "save the token and find the chat first")
	}
	if len(s.BackupPassword) < 8 {
		return validate.Fail("backupPassword", "set a backup password first")
	}
	var buf bytes.Buffer
	if err := backup.Write(ctx, n.store, n.dataDir, &buf); err != nil {
		return err
	}
	enc, err := backup.Encrypt(buf.Bytes(), s.BackupPassword)
	if err == nil {
		name := strings.TrimSuffix(backup.Name(time.Now()), ".tar.gz") + ".mpbk"
		caption := "🗄 Резервная копия (зашифрована паролем копий). Восстановление: panel restore -data-dir /var/lib/panel " + name
		err = telegram.New(s.Token).SendFile(ctx, s.ChatID, name, enc, caption)
	}
	s.LastError = ""
	if err != nil {
		s.LastError = err.Error()
	} else {
		s.LastBackup = time.Now().UTC()
	}
	n.store.SetSetting(ctx, telegramKey, s)
	return err
}

// Run sends the daily backup when it is due (checked hourly).
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTimer(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		t.Reset(time.Hour)
		s := n.Get(ctx)
		if !s.DailyBackup || !n.ready(s) || time.Since(s.LastBackup) < 23*time.Hour {
			continue
		}
		if err := n.BackupNow(ctx); err != nil {
			n.log.Warn("telegram backup", "err", err)
		}
	}
}
