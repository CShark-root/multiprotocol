package service

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"panel/internal/agent"
	"panel/internal/amnezia"
	"panel/internal/panel/compile"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
	"panel/internal/panel/telegram"
	"panel/internal/shared/version"
)

const usersPerPage = 8

var statusMark = map[Status]string{StatusActive: "🟢", StatusDisabled: "⚪", StatusExpired: "⌛", StatusLimited: "⛔"}
var statusName = map[Status]string{StatusActive: "активен", StatusDisabled: "выключен", StatusExpired: "срок истёк", StatusLimited: "трафик закончился"}

var back = func(to string) []telegram.Button {
	return []telegram.Button{btn("‹ Назад", to), btn("Меню", "m")}
}

// screen рисует экран по данным кнопки.
func (b *Bot) screen(ctx context.Context, data string) (string, telegram.Keyboard) {
	parts := strings.Split(data, ":")
	arg := func(i int) string {
		if len(parts) > i {
			return parts[i]
		}
		return ""
	}
	id, _ := strconv.ParseInt(arg(1), 10, 64)
	switch parts[0] {
	case "u":
		page, _ := strconv.Atoi(arg(2))
		return b.usersScreen(ctx, arg(1), page)
	case "c":
		return b.userScreen(ctx, id)
	case "l":
		sid := arg(1)
		return "Новый лимит трафика в месяц:", telegram.Keyboard{
			{btn("10 ГБ", "L:"+sid+":10"), btn("50 ГБ", "L:"+sid+":50"), btn("100 ГБ", "L:"+sid+":100")},
			{btn("200 ГБ", "L:"+sid+":200"), btn("500 ГБ", "L:"+sid+":500"), btn("Без лимита", "L:"+sid+":0")},
			{btn("‹ Назад", "c:"+sid)},
		}
	case "d":
		u, err := b.d.Users.Get(ctx, id)
		if err != nil {
			return "Пользователь не найден.", telegram.Keyboard{back("u:a:0")}
		}
		return "Удалить <b>" + esc(u.Name) + "</b>? Подписка перестанет работать, а его трафик останется в общей статистике. Это не отменить.",
			telegram.Keyboard{{btn("🗑 Да, удалить", "d!:"+arg(1)), btn("Отмена", "c:"+arg(1))}}
	case "nw":
		return "➕ <b>Новый пользователь</b>\n\nНапишите имя: буквы, цифры и _ . - без пробелов.", telegram.Keyboard{{btn("Отмена", "nx")}}
	case "nl":
		if b.wiz == nil {
			return b.mainScreen(ctx)
		}
		return "➕ <b>" + esc(b.wiz.name) + "</b>\n\nСколько трафика в месяц?", telegram.Keyboard{
			{btn("50 ГБ", "nl:50"), btn("100 ГБ", "nl:100"), btn("200 ГБ", "nl:200")},
			{btn("Без лимита", "nl:0")},
			{btn("Отмена", "nx")},
		}
	case "nt":
		if b.wiz == nil {
			return b.mainScreen(ctx)
		}
		limit := "без лимита"
		if b.wiz.limit > 0 {
			limit = gib(b.wiz.limit) + " в месяц"
		}
		return "➕ <b>" + esc(b.wiz.name) + "</b> · " + limit + "\n\nНа какой срок?", telegram.Keyboard{
			{btn("30 дней", "nt:30"), btn("90 дней", "nt:90"), btn("Год", "nt:365")},
			{btn("Бессрочно", "nt:0")},
			{btn("Отмена", "nx")},
		}
	case "sv":
		return b.serversScreen(ctx)
	case "nr":
		return "Перезапустить ядра на этом сервере? Подключения через него на пару секунд оборвутся.",
			telegram.Keyboard{{btn("Да, перезапустить", "nr!:"+arg(1)), btn("Отмена", "sv")}}
	case "tr":
		d, _ := strconv.Atoi(arg(1))
		return b.trafficScreen(ctx, d)
	case "am":
		return b.amneziaScreen(ctx)
	case "mt":
		return b.serviceScreen(ctx)
	case "rc":
		return "Перезапустить все ядра на этом сервере? Подключения на пару секунд оборвутся.",
			telegram.Keyboard{{btn("Да, перезапустить", "rc!"), btn("Отмена", "mt")}}
	case "up":
		return "Обновить панель до последней версии? Она перезапустится, VPN оборвётся на несколько секунд.",
			telegram.Keyboard{{btn("Да, обновить", "up!"), btn("Отмена", "mt")}}
	case "rb":
		return "Перезагрузить VPS? Панель и VPN будут недоступны около минуты.",
			telegram.Keyboard{{btn("Да, перезагрузить", "rb!"), btn("Отмена", "mt")}}
	}
	return b.mainScreen(ctx)
}

// online — кто из пользователей сейчас подключён: id → число адресов.
func (b *Bot) online(ctx context.Context) map[int64]int {
	out := map[int64]int{}
	if b.d.Agent == nil {
		return out
	}
	m, _ := b.d.Agent.Online(ctx)
	for email, e := range MergeOnline(m, b.d.Nodes) {
		if e.Connections == 0 && len(e.IPs) == 0 {
			continue
		}
		if id, ok := compile.UserIDFromEmail(email); ok {
			out[id] += max(1, len(e.IPs))
		}
	}
	return out
}

func today(now time.Time) string { return now.Local().Format(time.DateOnly) }

// warnings — на что обратить внимание: сроки, трафик, оплата, узлы.
func (b *Bot) warnings(ctx context.Context, list []store.User, nodes []NodeStatus) []string {
	now := b.d.Users.Now()
	var out []string
	for _, u := range list {
		if StatusOf(u, now) != StatusActive {
			continue
		}
		if u.ExpireAt != nil {
			if left := daysLeft(*u.ExpireAt, now); left <= 3 {
				out = append(out, fmt.Sprintf("%s: доступ кончится через %s", esc(u.Name), days(left)))
			}
		}
		if u.TrafficLimit > 0 && u.TrafficUsed*10 >= u.TrafficLimit*9 {
			out = append(out, fmt.Sprintf("%s: осталось %s трафика", esc(u.Name), gib(max(0, u.TrafficLimit-u.TrafficUsed))))
		}
	}
	for _, n := range nodes {
		if !n.LastSeen.IsZero() && !n.Online {
			out = append(out, "«"+esc(n.Name)+"» не на связи")
		}
		if n.PaidUntil != nil {
			if left := daysLeft(*n.PaidUntil, now); left <= 7 {
				out = append(out, "«"+esc(n.Name)+"»: оплата через "+days(left))
			}
		}
	}
	if b.d.Billing != nil {
		if m := b.d.Billing.Main(ctx); m.PaidUntil != nil {
			if left := daysLeft(*m.PaidUntil, now); left <= 7 {
				out = append(out, "Этот сервер: оплата через "+days(left))
			}
		}
	}
	return out
}

func (b *Bot) nodes(ctx context.Context) []NodeStatus {
	if b.d.Nodes == nil {
		return nil
	}
	list, _ := b.d.Nodes.List(ctx)
	return list
}

func (b *Bot) mainScreen(ctx context.Context) (string, telegram.Keyboard) {
	var sb strings.Builder
	now := b.d.Users.Now()
	fmt.Fprintf(&sb, "🖥 <b>%s</b> · <i>%s</i>\n", esc(b.d.Notifier.host), esc(version.Version))
	if b.d.Agent != nil {
		h := b.d.Agent.Health(ctx)
		sys := h.System
		if sys.CPUs > 0 {
			fmt.Fprintf(&sb, "Нагрузка %s %d%%\n", bar(loadPct(sys)), loadPct(sys))
		}
		if sys.MemTotal > 0 {
			fmt.Fprintf(&sb, "Память   %s %d%%\n", bar(memPct(sys)), memPct(sys))
		}
		if sys.NetIface != "" {
			fmt.Fprintf(&sb, "Сеть ↓ %s · ↑ %s\n", bitrate(sys.NetRxBps), bitrate(sys.NetTxBps))
		}
		var cores []string
		for _, c := range h.Cores {
			if c.Installed && c.Name != "sniproxy" {
				cores = append(cores, coreDot(c)+" "+c.Name)
			}
		}
		if len(cores) > 0 {
			sb.WriteString(strings.Join(cores, "  ") + "\n")
		}
	}
	nodes := b.nodes(ctx)
	if len(nodes) > 0 {
		sb.WriteString("<blockquote>")
		for i, n := range nodes {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(nodeLine(n))
		}
		sb.WriteString("</blockquote>")
	}
	list, _ := b.d.Users.List(ctx)
	on := b.online(ctx)
	var todayBytes int64
	if t, err := b.d.Store.TrafficByDay(ctx, 0, today(now)); err == nil && len(t) > 0 {
		todayBytes = t[len(t)-1].Bytes
	}
	fmt.Fprintf(&sb, "\n📊 Сегодня <b>%s</b> · онлайн <b>%d</b> из %d\n", gib(todayBytes), len(on), len(list))
	if w := b.warnings(ctx, list, nodes); len(w) > 0 {
		sb.WriteString("\n⚠️ " + strings.Join(w, "\n⚠️ "))
	}
	kb := telegram.Keyboard{
		{btn("👥 Пользователи", "u:a:0"), btn("➕ Новый", "nw0")},
		{btn("🌍 Серверы", "sv"), btn("📊 Трафик", "tr:7")},
	}
	row := []telegram.Button{btn("🛠 Обслуживание", "mt")}
	if b.d.Amnezia != nil {
		row = append([]telegram.Button{btn("🛡 Amnezia", "am")}, row...)
	}
	kb = append(kb, row, []telegram.Button{btn("🔄 Обновить", "m")})
	return sb.String(), kb
}

func nodeLine(n NodeStatus) string {
	switch {
	case n.LastSeen.IsZero():
		return "<b>" + esc(n.Name) + "</b> ⚪ ждёт подключения"
	case !n.Online:
		return "<b>" + esc(n.Name) + "</b> 🔴 нет связи с " + n.LastSeen.Local().Format("02.01 15:04")
	}
	line := "<b>" + esc(n.Name) + "</b> 🟢"
	if n.Health != nil && n.Health.System.CPUs > 0 {
		line += fmt.Sprintf(" нагрузка %d%%", loadPct(n.Health.System))
	}
	return line + fmt.Sprintf(" · онлайн %d", n.Users)
}

// filters списка пользователей: a — все, on — онлайн, w — требуют внимания,
// off — без доступа.
var userFilters = []struct{ key, name string }{{"a", "Все"}, {"on", "Онлайн"}, {"w", "Внимание"}, {"off", "Без доступа"}}

func (b *Bot) usersScreen(ctx context.Context, filter string, page int) (string, telegram.Keyboard) {
	list, err := b.d.Users.List(ctx)
	if err != nil {
		return "Не удалось прочитать пользователей: " + esc(err.Error()), telegram.Keyboard{back("m")}
	}
	if filter == "" {
		filter = "a"
	}
	now := b.d.Users.Now()
	on := b.online(ctx)
	var shown []store.User
	for _, u := range list {
		st := StatusOf(u, now)
		keep := true
		switch filter {
		case "on":
			keep = on[u.ID] > 0
		case "off":
			keep = st != StatusActive
		case "w":
			keep = st == StatusActive && ((u.ExpireAt != nil && daysLeft(*u.ExpireAt, now) <= 7) ||
				(u.TrafficLimit > 0 && u.TrafficUsed*5 >= u.TrafficLimit*4))
		}
		if keep {
			shown = append(shown, u)
		}
	}
	var kb telegram.Keyboard
	var tabs []telegram.Button
	for _, f := range userFilters {
		name := f.name
		if f.key == filter {
			name = "· " + name + " ·"
		}
		tabs = append(tabs, btn(name, "u:"+f.key+":0"))
	}
	kb = append(kb, tabs)
	pages := max(1, (len(shown)+usersPerPage-1)/usersPerPage)
	page = max(0, min(page, pages-1))
	for _, u := range shown[page*usersPerPage : min(len(shown), (page+1)*usersPerPage)] {
		kb = append(kb, []telegram.Button{btn(userButton(u, now, on[u.ID]), "c:"+strconv.FormatInt(u.ID, 10))})
	}
	var nav []telegram.Button
	if page > 0 {
		nav = append(nav, btn("‹", "u:"+filter+":"+strconv.Itoa(page-1)))
	}
	nav = append(nav, btn("Меню", "m"))
	if page < pages-1 {
		nav = append(nav, btn("›", "u:"+filter+":"+strconv.Itoa(page+1)))
	}
	kb = append(kb, nav)
	text := fmt.Sprintf("👥 <b>Пользователи</b> · %d из %d", len(shown), len(list))
	if pages > 1 {
		text += fmt.Sprintf(" · стр. %d/%d", page+1, pages)
	}
	if len(shown) == 0 {
		text += "\n\nЗдесь никого."
	}
	return text + "\n\n<i>Чтобы найти человека, просто напишите его имя.</i>", kb
}

func userButton(u store.User, now time.Time, devices int) string {
	s := statusMark[StatusOf(u, now)] + " " + u.Name
	if u.TrafficLimit > 0 {
		s += fmt.Sprintf(" · %d%%", percent(float64(u.TrafficUsed), float64(u.TrafficLimit)))
	} else {
		s += " · " + gib(u.TrafficUsed)
	}
	if devices > 0 {
		s += " · 📱" + strconv.Itoa(devices)
	}
	return s
}

func (b *Bot) searchScreen(ctx context.Context, q string) (string, telegram.Keyboard) {
	list, _ := b.d.Users.List(ctx)
	q = strings.ToLower(q)
	var found []store.User
	for _, u := range list {
		if strings.Contains(strings.ToLower(u.Name), q) || (u.Note != "" && strings.Contains(strings.ToLower(u.Note), q)) {
			found = append(found, u)
		}
	}
	if len(found) == 1 {
		return b.userScreen(ctx, found[0].ID)
	}
	if len(found) == 0 {
		return "Никого с «" + esc(q) + "» в имени не нашлось.", telegram.Keyboard{{btn("👥 Все пользователи", "u:a:0"), btn("Меню", "m")}}
	}
	now := b.d.Users.Now()
	on := b.online(ctx)
	var kb telegram.Keyboard
	for _, u := range found[:min(len(found), 12)] {
		kb = append(kb, []telegram.Button{btn(userButton(u, now, on[u.ID]), "c:"+strconv.FormatInt(u.ID, 10))})
	}
	kb = append(kb, []telegram.Button{btn("Меню", "m")})
	return fmt.Sprintf("🔎 Нашлось %d по «%s»:", len(found), esc(q)), kb
}

func (b *Bot) userScreen(ctx context.Context, id int64) (string, telegram.Keyboard) {
	u, err := b.d.Users.Get(ctx, id)
	if err != nil {
		return "Пользователь не найден.", telegram.Keyboard{back("u:a:0")}
	}
	now := b.d.Users.Now()
	st := StatusOf(u, now)
	var sb strings.Builder
	sb.WriteString(statusMark[st] + " <b>" + esc(u.Name) + "</b> — " + statusName[st] + "\n")
	if u.Note != "" {
		sb.WriteString("<i>" + esc(u.Note) + "</i>\n")
	}
	sb.WriteString("\n")
	if u.TrafficLimit > 0 {
		p := percent(float64(u.TrafficUsed), float64(u.TrafficLimit))
		fmt.Fprintf(&sb, "Трафик %s %d%%\n%s из %s", bar(p), p, gib(u.TrafficUsed), gib(u.TrafficLimit))
	} else {
		sb.WriteString("Трафик " + gib(u.TrafficUsed) + ", без лимита")
	}
	if r := resetName[u.ResetPeriod]; r != "" {
		sb.WriteString(" · сброс " + r)
	}
	sb.WriteString("\n")
	if u.ExpireAt != nil {
		left := daysLeft(*u.ExpireAt, now)
		sb.WriteString("Доступ до " + u.ExpireAt.Local().Format("02.01.2006"))
		if left >= 0 {
			sb.WriteString(" (ещё " + days(left) + ")")
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Срок не ограничен\n")
	}
	if n := b.online(ctx)[u.ID]; n > 0 {
		fmt.Fprintf(&sb, "Сейчас онлайн: %d %s\n", n, plural(n, "устройство", "устройства", "устройств"))
	}
	if f, err := b.d.Store.ListSubFetches(ctx, u.ID); err == nil && len(f) > 0 {
		sb.WriteString("Подписка обновлена " + ago(f[0].At, now) + " · " + esc(appName(f[0].App)) + "\n")
	}
	if links, err := b.d.Store.ListAmneziaLinks(ctx); err == nil {
		n := 0
		for _, l := range links {
			if l.UserID == u.ID {
				n++
			}
		}
		if n > 0 {
			fmt.Fprintf(&sb, "AmneziaWG: %d %s\n", n, plural(n, "клиент", "клиента", "клиентов"))
		}
	}
	sid := strconv.FormatInt(u.ID, 10)
	toggle := "⏸ Выключить"
	if !u.Enabled {
		toggle = "▶️ Включить"
	}
	kb := telegram.Keyboard{}
	if u.ExpireAt != nil {
		kb = append(kb, []telegram.Button{btn("+7 дн.", "x:"+sid+":7"), btn("+30 дн.", "x:"+sid+":30"), btn("+90 дн.", "x:"+sid+":90")})
	}
	kb = append(kb,
		[]telegram.Button{btn("↺ Сбросить трафик", "r:"+sid), btn(toggle, "t:"+sid)},
		[]telegram.Button{btn("🔗 QR и ссылка", "s:"+sid), btn("📦 Лимит", "l:"+sid)},
		[]telegram.Button{btn("🗑 Удалить", "d:"+sid)},
		[]telegram.Button{btn("‹ К списку", "u:a:0"), btn("Меню", "m")},
	)
	return sb.String(), kb
}

var resetName = map[string]string{"day": "каждый день", "week": "каждую неделю", "month": "каждый месяц"}

func (b *Bot) serversScreen(ctx context.Context) (string, telegram.Keyboard) {
	var sb strings.Builder
	now := b.d.Users.Now()
	sb.WriteString("🌍 <b>Серверы</b>\n\n")
	sb.WriteString("<b>" + esc(b.d.Notifier.host) + "</b> · главный\n")
	if b.d.Agent != nil {
		sys := b.d.Agent.Health(ctx).System
		sb.WriteString(sysLine(sys) + "\n")
	}
	if b.d.Billing != nil {
		m := b.d.Billing.Main(ctx)
		sb.WriteString(billingLine(m.PaidUntil, m.Price, now))
	}
	var kb telegram.Keyboard
	for _, n := range b.nodes(ctx) {
		sb.WriteString("\n" + nodeLine(n) + "\n")
		if n.Online && n.Health != nil {
			sb.WriteString(sysLine(n.Health.System))
			if n.AgentVersion != "" {
				sb.WriteString(" · " + esc(n.AgentVersion))
			}
			sb.WriteString("\n")
			if n.ApplyError != "" {
				sb.WriteString("⚠️ " + esc(n.ApplyError) + "\n")
			}
		}
		sb.WriteString(billingLine(n.PaidUntil, n.Price, now))
		if n.Online {
			kb = append(kb, []telegram.Button{btn("⚙️ Перезапустить ядра: "+n.Name, "nr:"+strconv.FormatInt(n.ID, 10))})
		}
	}
	if b.d.Nodes == nil || len(b.nodes(ctx)) == 0 {
		sb.WriteString("\n<i>Других серверов нет. Их добавляют в панели на вкладке «Серверы».</i>")
	}
	kb = append(kb, []telegram.Button{btn("🔄 Обновить", "sv"), btn("Меню", "m")})
	return sb.String(), kb
}

func sysLine(sys agent.SystemInfo) string {
	parts := []string{}
	if sys.CPUs > 0 {
		parts = append(parts, fmt.Sprintf("нагрузка %d%%", loadPct(sys)))
	}
	if sys.MemTotal > 0 {
		parts = append(parts, fmt.Sprintf("память %d%%", memPct(sys)))
	}
	if sys.NetIface != "" {
		parts = append(parts, "↓ "+bitrate(sys.NetRxBps))
	}
	if sys.UptimeSeconds > 0 {
		parts = append(parts, "работает "+uptime(sys.UptimeSeconds))
	}
	return strings.Join(parts, " · ")
}

func billingLine(paid *time.Time, price string, now time.Time) string {
	if paid == nil {
		return ""
	}
	s := "💳 Оплачен до " + paid.Local().Format("02.01.2006")
	if left := daysLeft(*paid, now); left < 0 {
		s += " — <b>просрочено</b>"
	} else if left <= 7 {
		s += " — ещё " + days(left)
	}
	if price != "" {
		s += " · " + esc(price)
	}
	return s + "\n"
}

// spark — столбики по дням: ▁▂▃▄▅▆▇█.
func spark(vals []int64) string {
	const ticks = "▁▂▃▄▅▆▇█"
	r := []rune(ticks)
	var top int64
	for _, v := range vals {
		top = max(top, v)
	}
	var sb strings.Builder
	for _, v := range vals {
		i := 0
		if top > 0 {
			i = int(v * int64(len(r)-1) / top)
		}
		sb.WriteRune(r[i])
	}
	return sb.String()
}

var weekday = []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}

func (b *Bot) trafficScreen(ctx context.Context, period int) (string, telegram.Keyboard) {
	if period != 1 && period != 30 {
		period = 7
	}
	now := b.d.Users.Now()
	from := now.AddDate(0, 0, -(period - 1))
	rows, _ := b.d.Store.TrafficByDay(ctx, 0, today(from))
	byDay := map[string]int64{}
	var total int64
	for _, r := range rows {
		byDay[r.Day] = r.Bytes
		total += r.Bytes
	}
	var sb strings.Builder
	names := map[int]string{1: "сегодня", 7: "7 дней", 30: "30 дней"}
	fmt.Fprintf(&sb, "📊 <b>Трафик за %s</b>: %s\n", names[period], gib(total))
	// Сравнение с предыдущим таким же периодом.
	if prev, err := b.d.Store.TrafficByDay(ctx, 0, today(from.AddDate(0, 0, -period))); err == nil {
		var p int64
		for _, r := range prev {
			if r.Day < today(from) {
				p += r.Bytes
			}
		}
		if p > 0 {
			d := percent(float64(total-p), float64(p))
			sign := "+"
			if d < 0 {
				sign = ""
			}
			fmt.Fprintf(&sb, "<i>%s%d%% к прошлому периоду</i>\n", sign, d)
		}
	}
	switch period {
	case 7:
		var top int64
		for _, v := range byDay {
			top = max(top, v)
		}
		sb.WriteString("<pre>")
		for i := 0; i < 7; i++ {
			d := from.AddDate(0, 0, i)
			v := byDay[today(d)]
			n := 0
			if top > 0 {
				n = int(v * 12 / top)
			}
			fmt.Fprintf(&sb, "%s %s %-12s %s\n", weekday[d.Weekday()], d.Format("02.01"), strings.Repeat("█", n), gib(v))
		}
		sb.WriteString("</pre>")
	case 30:
		vals := make([]int64, 30)
		for i := range vals {
			vals[i] = byDay[today(from.AddDate(0, 0, i))]
		}
		sb.WriteString("<pre>" + spark(vals) + "</pre>\n" + from.Format("02.01") + " … " + now.Format("02.01") + "\n")
	}
	if top, err := b.d.Store.TrafficTop(ctx, today(from)); err == nil && len(top) > 0 {
		names := map[int64]string{}
		if list, err := b.d.Users.List(ctx); err == nil {
			for _, u := range list {
				names[u.ID] = u.Name
			}
		}
		sb.WriteString("\n<b>Больше всех</b>\n")
		for i, t := range top[:min(len(top), 5)] {
			name := names[t.UserID]
			if t.UserID == 0 || name == "" {
				name = "без пользователя (Amnezia и удалённые)"
			}
			fmt.Fprintf(&sb, "%d. %s — %s\n", i+1, esc(name), gib(t.Bytes))
		}
	}
	tab := func(d int, name string) telegram.Button {
		if d == period {
			name = "· " + name + " ·"
		}
		return btn(name, "tr:"+strconv.Itoa(d))
	}
	return sb.String(), telegram.Keyboard{{tab(1, "Сегодня"), tab(7, "7 дней"), tab(30, "30 дней")}, {btn("Меню", "m")}}
}

func (b *Bot) amneziaScreen(ctx context.Context) (string, telegram.Keyboard) {
	kb := telegram.Keyboard{{btn("🔄 Обновить", "am"), btn("Меню", "m")}}
	if b.d.Amnezia == nil {
		return "Amnezia на этом сервере не найдена.", kb
	}
	st, err := b.d.Amnezia.Status(ctx)
	if err != nil {
		return "Помощник Amnezia не отвечает: " + esc(err.Error()), kb
	}
	if !st.Docker || len(st.Containers) == 0 {
		return "🛡 Контейнеров Amnezia нет.", kb
	}
	now := time.Now()
	var sb strings.Builder
	sb.WriteString("🛡 <b>Amnezia</b>\n")
	for _, c := range st.Containers {
		dot := "🔴"
		if c.State == "running" {
			dot = "🟢"
		}
		name := c.Kind
		if c.Version != "" {
			name += " " + c.Version
		}
		fmt.Fprintf(&sb, "\n%s <b>%s</b> · %s\n", dot, esc(c.Name), esc(name))
		if c.State == "running" {
			fmt.Fprintf(&sb, "процессор %.0f%% · память %s\n", c.CPU, gib(int64(c.MemBytes)))
		}
		if len(c.Clients) == 0 {
			continue
		}
		clients := slices.Clone(c.Clients)
		slices.SortFunc(clients, func(a, b amnezia.Client) int { return cmp.Compare(b.RxBytes+b.TxBytes, a.RxBytes+a.TxBytes) })
		active := 0
		for _, cl := range clients {
			if cl.LastHandshake != nil && now.Sub(*cl.LastHandshake) < 3*time.Minute {
				active++
			}
		}
		fmt.Fprintf(&sb, "Клиентов %d, на связи %d\n<blockquote>", len(clients), active)
		for i, cl := range clients[:min(len(clients), 10)] {
			if i > 0 {
				sb.WriteString("\n")
			}
			dot := "⚪"
			if cl.LastHandshake != nil && now.Sub(*cl.LastHandshake) < 3*time.Minute {
				dot = "🟢"
			}
			name := cl.Name
			if name == "" {
				name = cl.ID
			}
			if utf8.RuneCountInString(name) > 28 {
				name = string([]rune(name)[:28]) + "…"
			}
			fmt.Fprintf(&sb, "%s %s · %s", dot, esc(name), gib(cl.RxBytes+cl.TxBytes))
		}
		sb.WriteString("</blockquote>")
	}
	return sb.String(), kb
}

func (b *Bot) serviceScreen(ctx context.Context) (string, telegram.Keyboard) {
	var sb strings.Builder
	sb.WriteString("🛠 <b>Обслуживание</b>\n\n")
	sb.WriteString("Панель " + esc(version.Version))
	latest, _ := LatestRelease(ctx, false)
	canUpdate := latest != nil && latest.Tag != version.Version && UpdaterInstalled()
	if canUpdate {
		sb.WriteString(" → есть <b>" + esc(latest.Tag) + "</b>")
	} else if latest != nil {
		sb.WriteString(" · последняя")
	}
	sb.WriteString("\n")
	if b.d.Agent != nil {
		var cores []string
		for _, c := range b.d.Agent.Health(ctx).Cores {
			if c.Installed && c.Name != "sniproxy" {
				cores = append(cores, coreDot(c)+" "+c.Name+" "+esc(c.Version))
			}
		}
		sb.WriteString(strings.Join(cores, "\n") + "\n")
	}
	if b.d.Routing != nil {
		if rs, err := b.d.Routing.Get(ctx); err == nil {
			sb.WriteString("\n")
			switch {
			case rs.WARP.Account == nil:
				sb.WriteString("WARP не зарегистрирован")
			case rs.WARP.Mode == routing.WarpOff:
				sb.WriteString("WARP выключен")
			default:
				sb.WriteString("WARP включён · " + esc(rs.WARP.Account.Endpoint))
			}
			sb.WriteString("\n")
		}
	}
	s := b.d.Notifier.Get(ctx)
	if !s.LastBackup.IsZero() {
		sb.WriteString("Последняя копия " + ago(s.LastBackup, time.Now()) + "\n")
	}
	var kb telegram.Keyboard
	if canUpdate {
		kb = append(kb, []telegram.Button{btn("⬆️ Обновить панель до "+latest.Tag, "up")})
	}
	if b.d.Routing != nil {
		kb = append(kb, []telegram.Button{btn("🌐 Проверить WARP", "wc")})
	}
	kb = append(kb,
		[]telegram.Button{btn("⚙️ Перезапустить ядра", "rc"), btn("🗄 Копия в чат", "bk")},
		[]telegram.Button{btn("♻️ Перезагрузить VPS", "rb")},
		[]telegram.Button{btn("Меню", "m")},
	)
	return sb.String(), kb
}
