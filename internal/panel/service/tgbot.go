package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/amnezia"
	"panel/internal/nodeproto"
	"panel/internal/panel/store"
	"panel/internal/panel/telegram"
)

// BotDeps — то, чем управляет бот. Agent, Nodes, Routing, Billing, Amnezia и
// Reboot могут быть nil: тогда соответствующих экранов нет.
type BotDeps struct {
	Notifier *Notifier
	Users    *Users
	Store    *store.Store
	Agent    *agent.Agent
	Nodes    *Nodes
	Routing  *Routing
	Billing  *Billing
	Amnezia  *amnezia.HelperClient
	Reboot   func() error
	DataDir  string
	SubBase  string // начало ссылки подписки, к нему добавляется токен
	Log      *slog.Logger
}

// Bot отвечает в чат админа меню: сводка, пользователи, серверы, трафик,
// Amnezia, обслуживание. Говорит только с чатом из настроек Telegram и только
// пока включено «Управление через бота».
type Bot struct {
	d       BotDeps
	menu    int64   // id живого сообщения с меню, его правим на месте
	wiz     *wizard // мастер нового пользователя, nil — не идёт
	cmdsFor string  // для какого токена уже задано меню команд
}

// wizard — новый пользователь в три шага: имя (текстом), лимит, срок.
type wizard struct {
	name  string
	limit int64 // байты, 0 — без лимита
}

func NewBot(d BotDeps) *Bot { return &Bot{d: d} }

// notifyPrefix помечает кнопки под уведомлениями: экран после них приходит
// новым сообщением, а само уведомление остаётся в истории.
const notifyPrefix = "n|"

var botCommands = []telegram.Command{
	{Command: "menu", Description: "Сводка"},
	{Command: "users", Description: "Пользователи"},
	{Command: "new", Description: "Новый пользователь"},
	{Command: "servers", Description: "Серверы"},
	{Command: "traffic", Description: "Трафик"},
	{Command: "service", Description: "Обслуживание"},
}

// Run long-polls Telegram until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	var offset int64
	token := ""
	for ctx.Err() == nil {
		s := b.d.Notifier.Get(ctx)
		if !s.Control || !b.d.Notifier.ready(s) {
			sleep(ctx, 20*time.Second)
			continue
		}
		if s.Token != token { // новый бот начинает с чистого листа
			token, offset, b.menu, b.wiz = s.Token, 0, 0, nil
		}
		bot := telegram.New(s.Token)
		if b.cmdsFor != s.Token {
			if err := bot.SetCommands(ctx, botCommands); err == nil {
				b.cmdsFor = s.Token
			}
		}
		ups, err := bot.Updates(ctx, offset, 50*time.Second)
		if err != nil {
			if ctx.Err() == nil {
				b.d.Log.Warn("telegram bot", "err", err)
				sleep(ctx, 15*time.Second)
			}
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			b.handle(ctx, bot, s, u)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (b *Bot) handle(ctx context.Context, bot *telegram.Bot, s TelegramSettings, u telegram.Update) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch {
	case u.Message != nil:
		if u.Message.Chat.ID != s.ChatID {
			return // чужим не отвечаем вообще
		}
		text, kb := b.onText(ctx, strings.TrimSpace(u.Message.Text))
		if id, err := bot.SendMenu(ctx, s.ChatID, text, kb); err == nil {
			b.menu = id
		} else {
			b.d.Log.Warn("telegram bot", "err", err)
		}
	case u.Callback != nil && u.Callback.Message != nil:
		if u.Callback.Message.Chat.ID != s.ChatID {
			return
		}
		data, fromNotice := strings.CutPrefix(u.Callback.Data, notifyPrefix)
		toast := b.act(ctx, bot, s, &data)
		bot.Answer(ctx, u.Callback.ID, toast)
		text, kb := b.screen(ctx, data)
		if fromNotice {
			if id, err := bot.SendMenu(ctx, s.ChatID, text, kb); err == nil {
				b.menu = id
			}
			return
		}
		b.menu = u.Callback.Message.ID
		if err := bot.EditMenu(ctx, s.ChatID, b.menu, text, kb); err != nil {
			b.d.Log.Warn("telegram bot", "err", err)
		}
	}
}

// onText — команда, шаг мастера (имя) или поиск пользователя по имени.
func (b *Bot) onText(ctx context.Context, text string) (string, telegram.Keyboard) {
	if cmd, ok := strings.CutPrefix(text, "/"); ok {
		cmd, _, _ = strings.Cut(cmd, "@") // /users@my_bot в группах
		cmd, _, _ = strings.Cut(cmd, " ")
		b.wiz = nil
		switch cmd {
		case "users":
			return b.screen(ctx, "u:a:0")
		case "new":
			b.wiz = &wizard{}
			return b.screen(ctx, "nw")
		case "servers":
			return b.screen(ctx, "sv")
		case "traffic":
			return b.screen(ctx, "tr:7")
		case "service":
			return b.screen(ctx, "mt")
		}
		return b.screen(ctx, "m")
	}
	if b.wiz != nil && b.wiz.name == "" {
		if !userNameRe.MatchString(text) {
			return "В имени можно буквы, цифры и знаки _ . - без пробелов, до 64 символов. Напишите ещё раз:", telegram.Keyboard{{btn("Отмена", "nx")}}
		}
		b.wiz.name = text
		return b.screen(ctx, "nl")
	}
	return b.searchScreen(ctx, text)
}

// act выполняет действие кнопки и может сменить экран, который покажется
// после. Возвращает короткий ответ для всплывашки.
func (b *Bot) act(ctx context.Context, bot *telegram.Bot, s TelegramSettings, data *string) string {
	parts := strings.Split(*data, ":")
	id := int64(0)
	if len(parts) > 1 {
		id, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	arg := func(i int) int {
		if len(parts) > i {
			v, _ := strconv.Atoi(parts[i])
			return v
		}
		return 0
	}
	card := "c:" + strconv.FormatInt(id, 10)
	users := b.d.Users
	switch parts[0] {
	case "x": // x:<id>:<дни>
		*data = card
		u, err := users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден"
		}
		if u.ExpireAt == nil {
			return "Срок не ограничен"
		}
		if _, err := users.Extend(ctx, 0, id, arg(2)); err != nil {
			return "Не получилось: " + err.Error()
		}
		return "Продлено на " + days(arg(2))
	case "r":
		*data = card
		if _, err := users.ResetTraffic(ctx, 0, id); err != nil {
			return "Не получилось: " + err.Error()
		}
		return "Трафик сброшен"
	case "t":
		*data = card
		u, err := users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден"
		}
		in := inputOf(u)
		in.Enabled = !u.Enabled
		if _, err := users.Update(ctx, 0, id, in); err != nil {
			return "Не получилось: " + err.Error()
		}
		if in.Enabled {
			return "Включён"
		}
		return "Выключен"
	case "L": // L:<id>:<ГБ>, 0 — без лимита
		*data = card
		u, err := users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден"
		}
		in := inputOf(u)
		in.TrafficLimit = int64(arg(2)) << 30
		if _, err := users.Update(ctx, 0, id, in); err != nil {
			return "Не получилось: " + err.Error()
		}
		if in.TrafficLimit == 0 {
			return "Лимит снят"
		}
		return fmt.Sprintf("Лимит %d ГБ", arg(2))
	case "s":
		*data = card
		u, err := users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден"
		}
		b.sendSubscription(ctx, bot, s, u)
		return ""
	case "d!":
		*data = "u:a:0"
		if err := users.Delete(ctx, 0, id); err != nil {
			return "Не получилось: " + err.Error()
		}
		return "Удалён"
	case "nl": // nl:<ГБ> — шаг мастера: лимит
		if b.wiz == nil {
			*data = "m"
			return "Мастер устарел, начните заново: /new"
		}
		b.wiz.limit = int64(arg(1)) << 30
		*data = "nt"
		return ""
	case "nt": // nt:<дни>, 0 — бессрочно: создаём
		w := b.wiz
		if w == nil || w.name == "" {
			*data = "m"
			return "Мастер устарел, начните заново: /new"
		}
		in := UserInput{Name: w.name, Enabled: true, TrafficLimit: w.limit, ResetPeriod: "none"}
		if w.limit > 0 {
			in.ResetPeriod = "month"
		}
		if d := arg(1); d > 0 {
			t := users.Now().AddDate(0, 0, d)
			in.ExpireAt = &t
		}
		u, err := users.Create(ctx, 0, in)
		if err != nil {
			*data = "nt"
			return "Не получилось: " + err.Error()
		}
		b.wiz = nil
		*data = "c:" + strconv.FormatInt(u.ID, 10)
		b.sendSubscription(ctx, bot, s, u)
		return "Создан"
	case "nw0":
		b.wiz = &wizard{}
		*data = "nw"
		return ""
	case "nx":
		b.wiz = nil
		*data = "m"
		return "Отменено"
	case "rc!":
		*data = "mt"
		if b.d.Agent == nil {
			return "Ядра здесь не управляются"
		}
		if err := b.d.Agent.RestartCores(); err != nil {
			return "Ошибка: " + err.Error()
		}
		return "Ядра перезапущены"
	case "nr!":
		*data = "sv"
		if b.d.Nodes == nil {
			return ""
		}
		b.d.Nodes.Command(id, nodeproto.CmdRestartCores)
		return "Команда отправлена"
	case "wc":
		*data = "mt"
		return b.checkWarp(ctx)
	case "bk":
		*data = "mt"
		if err := b.d.Notifier.BackupNow(ctx); err != nil {
			return "Копия не отправлена: " + err.Error()
		}
		return "Копия отправлена"
	case "up!":
		*data = "mt"
		if err := RequestUpdate(b.d.DataDir, false); err != nil {
			return "Ошибка: " + err.Error()
		}
		return "Обновление запущено, панель перезапустится через минуту"
	case "rb!":
		*data = "mt"
		if b.d.Reboot == nil {
			return "Перезагрузка недоступна"
		}
		if err := b.d.Reboot(); err != nil {
			return "Ошибка: " + err.Error()
		}
		return "Сервер перезагружается"
	}
	return ""
}

// sendSubscription отправляет отдельным сообщением QR-код и ссылку подписки:
// его удобно переслать человеку.
func (b *Bot) sendSubscription(ctx context.Context, bot *telegram.Bot, s TelegramSettings, u store.User) {
	link := b.d.SubBase + u.SubToken
	caption := "🔗 Подписка «" + esc(u.Name) + "»\n<code>" + esc(link) + "</code>\n\nОдна ссылка для v2RayTun, Hiddify, FlClash и других: формат выберется сам."
	if png, err := qrPNG(link); err == nil {
		if _, err := bot.SendPhoto(ctx, s.ChatID, png, caption, nil); err == nil {
			return
		}
	}
	bot.Send(ctx, s.ChatID, caption)
}

func (b *Bot) checkWarp(ctx context.Context) string {
	if b.d.Routing == nil {
		return "WARP здесь недоступен"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	through, _, err := b.d.Routing.CheckWarp(ctx)
	if err != nil {
		return "WARP не отвечает: " + err.Error()
	}
	if through.Warp == "on" || through.Warp == "plus" {
		return "WARP работает, выход " + through.IP
	}
	return "Через WARP не пошло (warp=" + through.Warp + ")"
}

func inputOf(u store.User) UserInput {
	return UserInput{
		Name: u.Name, Note: u.Note, Enabled: u.Enabled, TrafficLimit: u.TrafficLimit, ResetPeriod: u.ResetPeriod,
		ExpireAt: u.ExpireAt, AmneziaKey: u.AmneziaKey, SubHidden: u.SubHidden,
	}
}

func btn(text, data string) telegram.Button { return telegram.Button{Text: text, Data: data} }
