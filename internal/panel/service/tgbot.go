package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/panel/compile"
	"panel/internal/panel/store"
	"panel/internal/panel/telegram"
)

// BotDeps is what the management bot can act on. Agent and Reboot may be nil.
type BotDeps struct {
	Notifier *Notifier
	Users    *Users
	Agent    *agent.Agent
	Nodes    *Nodes // nil: только этот сервер
	Reboot   func() error
	SubBase  string // subscription URL prefix, the token is appended
	Log      *slog.Logger
}

// Bot answers the admin's chat with a menu: server state, users (renew,
// reset traffic, switch on/off, subscription link), core restart, backup and
// reboot. It only ever talks to the chat saved in the Telegram settings, and
// only while "control" is switched on.
type Bot struct {
	d    BotDeps
	menu int64 // id of the live menu message, edited in place
}

func NewBot(d BotDeps) *Bot { return &Bot{d: d} }

const usersPerPage = 8

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
		if s.Token != token { // a new bot starts from a clean slate
			token, offset, b.menu = s.Token, 0, 0
		}
		bot := telegram.New(s.Token)
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
			return // strangers get no answer at all
		}
		text, kb := b.screen(ctx, "m")
		if id, err := bot.SendMenu(ctx, s.ChatID, text, kb); err == nil {
			b.menu = id
		}
	case u.Callback != nil && u.Callback.Message != nil:
		if u.Callback.Message.Chat.ID != s.ChatID {
			return
		}
		data := u.Callback.Data
		toast := b.act(ctx, bot, s, &data)
		bot.Answer(ctx, u.Callback.ID, toast)
		text, kb := b.screen(ctx, data)
		b.menu = u.Callback.Message.ID
		if err := bot.EditMenu(ctx, s.ChatID, b.menu, text, kb); err != nil {
			b.d.Log.Warn("telegram bot", "err", err)
		}
	}
}

// act performs the action behind a button and may change which screen
// follows. It returns a short confirmation for the toast.
func (b *Bot) act(ctx context.Context, bot *telegram.Bot, s TelegramSettings, data *string) string {
	parts := strings.Split(*data, ":")
	id := int64(0)
	if len(parts) > 1 {
		id, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	users := b.d.Users
	switch parts[0] {
	case "x": // x:<id>:<days>
		days := 30
		if len(parts) > 2 {
			days, _ = strconv.Atoi(parts[2])
		}
		u, err := users.Get(ctx, id)
		if err == nil && u.ExpireAt == nil {
			*data = "c:" + parts[1]
			return "Срок не ограничен"
		}
		*data = "c:" + parts[1]
		if _, err := users.Extend(ctx, 0, id, days); err != nil {
			return "Не получилось: " + err.Error()
		}
		return fmt.Sprintf("Продлено на %d дн.", days)
	case "r":
		*data = "c:" + parts[1]
		if _, err := users.ResetTraffic(ctx, 0, id); err != nil {
			return "Не получилось: " + err.Error()
		}
		return "Трафик сброшен"
	case "t":
		*data = "c:" + parts[1]
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
	case "s":
		*data = "c:" + parts[1]
		u, err := users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден"
		}
		// A separate message: easy to copy or forward to the person.
		bot.Send(ctx, s.ChatID, "Подписка «"+u.Name+"»:\n"+b.d.SubBase+u.SubToken)
		return ""
	case "rc!":
		*data = "m"
		if b.d.Agent == nil {
			return "Ядра здесь не управляются"
		}
		if err := b.d.Agent.RestartCores(); err != nil {
			return "Ошибка: " + err.Error()
		}
		return "Ядра перезапущены"
	case "bk":
		*data = "m"
		if err := b.d.Notifier.BackupNow(ctx); err != nil {
			return "Копия не отправлена: " + err.Error()
		}
		return "Копия отправлена"
	case "rb!":
		*data = "m"
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

func inputOf(u store.User) UserInput {
	return UserInput{
		Name: u.Name, Note: u.Note, Enabled: u.Enabled, TrafficLimit: u.TrafficLimit, ResetPeriod: u.ResetPeriod,
		ExpireAt: u.ExpireAt, AmneziaKey: u.AmneziaKey, SubHidden: u.SubHidden,
	}
}

func btn(text, data string) telegram.Button { return telegram.Button{Text: text, Data: data} }

// screen renders the menu for callback data.
func (b *Bot) screen(ctx context.Context, data string) (string, telegram.Keyboard) {
	parts := strings.Split(data, ":")
	arg := ""
	if len(parts) > 1 {
		arg = parts[1]
	}
	switch parts[0] {
	case "u":
		page, _ := strconv.Atoi(arg)
		return b.usersScreen(ctx, page)
	case "c":
		id, _ := strconv.ParseInt(arg, 10, 64)
		return b.userScreen(ctx, id)
	case "rc":
		return "Перезапустить все ядра? Подключения на пару секунд оборвутся.",
			telegram.Keyboard{{btn("Да, перезапустить", "rc!"), btn("Отмена", "m")}}
	case "rb":
		return "Перезагрузить VPS? Панель и VPN будут недоступны около минуты.",
			telegram.Keyboard{{btn("Да, перезагрузить", "rb!"), btn("Отмена", "m")}}
	}
	return b.mainScreen(ctx)
}

func (b *Bot) mainScreen(ctx context.Context) (string, telegram.Keyboard) {
	var sb strings.Builder
	sb.WriteString("🖥 Сервер " + b.d.Notifier.host + "\n")
	if b.d.Agent != nil {
		h := b.d.Agent.Health(ctx)
		sys := h.System
		fmt.Fprintf(&sb, "Нагрузка %.2f · ОЗУ %s из %s · работает %s\n",
			sys.Load1, gib(int64(sys.MemTotal-sys.MemAvailable)), gib(int64(sys.MemTotal)), uptime(sys.UptimeSeconds))
		if sys.NetIface != "" {
			fmt.Fprintf(&sb, "Сеть: ↓ %s/с · ↑ %s/с\n", rate(sys.NetRxBps), rate(sys.NetTxBps))
		}
		for _, c := range h.Cores {
			if !c.Installed {
				continue
			}
			mark := "⚪"
			switch {
			case c.Running:
				mark = "🟢"
			case c.Message != "":
				mark = "🔴"
			}
			sb.WriteString(mark + " " + c.Name + "\n")
		}
	}
	if list, err := b.d.Users.List(ctx); err == nil {
		active := 0
		for _, u := range list {
			if StatusOf(u, b.d.Users.Now()) == StatusActive {
				active++
			}
		}
		fmt.Fprintf(&sb, "👥 Сейчас подключены: %s\n", b.onlineNames(ctx, list))
		fmt.Fprintf(&sb, "Учёток: %d, из них с действующим доступом %d", len(list), active)
	}
	kb := telegram.Keyboard{
		{btn("👥 Пользователи", "u:0"), btn("🔄 Обновить", "m")},
		{btn("⚙️ Перезапустить ядра", "rc"), btn("🗄 Резервная копия", "bk")},
		{btn("♻️ Перезагрузить VPS", "rb")},
	}
	return sb.String(), kb
}

var statusMark = map[Status]string{StatusActive: "🟢", StatusDisabled: "⚪", StatusExpired: "⌛", StatusLimited: "⛔"}

func (b *Bot) usersScreen(ctx context.Context, page int) (string, telegram.Keyboard) {
	list, err := b.d.Users.List(ctx)
	if err != nil {
		return "Не удалось прочитать пользователей: " + err.Error(), telegram.Keyboard{{btn("← Назад", "m")}}
	}
	if len(list) == 0 {
		return "Пользователей пока нет — добавьте их в панели.", telegram.Keyboard{{btn("← Назад", "m")}}
	}
	pages := (len(list) + usersPerPage - 1) / usersPerPage
	page = max(0, min(page, pages-1))
	var kb telegram.Keyboard
	for _, u := range list[page*usersPerPage : min(len(list), (page+1)*usersPerPage)] {
		kb = append(kb, []telegram.Button{btn(statusMark[StatusOf(u, b.d.Users.Now())]+" "+u.Name+" · "+gib(u.TrafficUsed), "c:"+strconv.FormatInt(u.ID, 10))})
	}
	var nav []telegram.Button
	if page > 0 {
		nav = append(nav, btn("‹", "u:"+strconv.Itoa(page-1)))
	}
	nav = append(nav, btn("← Меню", "m"))
	if page < pages-1 {
		nav = append(nav, btn("›", "u:"+strconv.Itoa(page+1)))
	}
	kb = append(kb, nav)
	return fmt.Sprintf("👥 Пользователи (%d), страница %d из %d", len(list), page+1, pages), kb
}

func (b *Bot) userScreen(ctx context.Context, id int64) (string, telegram.Keyboard) {
	u, err := b.d.Users.Get(ctx, id)
	if err != nil {
		return "Пользователь не найден.", telegram.Keyboard{{btn("← Назад", "u:0")}}
	}
	st := StatusOf(u, b.d.Users.Now())
	names := map[Status]string{StatusActive: "активен", StatusDisabled: "выключен", StatusExpired: "срок истёк", StatusLimited: "трафик закончился"}
	var sb strings.Builder
	sb.WriteString(statusMark[st] + " " + u.Name + " — " + names[st] + "\n")
	if u.Note != "" {
		sb.WriteString(u.Note + "\n")
	}
	limit := "без лимита"
	if u.TrafficLimit > 0 {
		limit = "из " + gib(u.TrafficLimit)
	}
	sb.WriteString("Трафик: " + gib(u.TrafficUsed) + " " + limit + "\n")
	if u.ExpireAt != nil {
		sb.WriteString("Доступ до " + u.ExpireAt.Local().Format("02.01.2006"))
	} else {
		sb.WriteString("Срок не ограничен")
	}
	sid := strconv.FormatInt(u.ID, 10)
	toggle := "⏸ Выключить"
	if !u.Enabled {
		toggle = "▶️ Включить"
	}
	kb := telegram.Keyboard{}
	if u.ExpireAt != nil {
		kb = append(kb, []telegram.Button{btn("+7 дней", "x:"+sid+":7"), btn("+30 дней", "x:"+sid+":30"), btn("+90 дней", "x:"+sid+":90")})
	}
	kb = append(kb,
		[]telegram.Button{btn("↺ Сбросить трафик", "r:"+sid), btn(toggle, "t:"+sid)},
		[]telegram.Button{btn("🔗 Ссылка подписки", "s:"+sid)},
		[]telegram.Button{btn("← К списку", "u:0"), btn("Меню", "m")},
	)
	return sb.String(), kb
}

func gib(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f ГБ", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f МБ", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d КБ", b>>10)
	}
}

func rate(bps float64) string {
	if bps >= 1<<20 {
		return fmt.Sprintf("%.1f МБ", bps/(1<<20))
	}
	return fmt.Sprintf("%.0f КБ", bps/(1<<10))
}

func uptime(s int64) string {
	d := time.Duration(s) * time.Second
	if days := int(d.Hours()) / 24; days > 0 {
		return fmt.Sprintf("%d дн. %d ч", days, int(d.Hours())%24)
	}
	return fmt.Sprintf("%d ч %d мин", int(d.Hours()), int(d.Minutes())%60)
}

// onlineNames lists who is connected right now, as the panel's online
// marks do.
func (b *Bot) onlineNames(ctx context.Context, list []store.User) string {
	if b.d.Agent == nil {
		return "—"
	}
	m, _ := b.d.Agent.Online(ctx)
	m = MergeOnline(m, b.d.Nodes)
	names := map[int64]string{}
	for _, u := range list {
		names[u.ID] = u.Name
	}
	var on []string
	for email, e := range m {
		if e.Connections == 0 && len(e.IPs) == 0 {
			continue
		}
		if id, ok := compile.UserIDFromEmail(email); ok && names[id] != "" {
			on = append(on, names[id])
		}
	}
	if len(on) == 0 {
		return "никого"
	}
	slices.Sort(on)
	return fmt.Sprintf("%d (%s)", len(on), strings.Join(on, ", "))
}
