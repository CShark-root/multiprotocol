package service

import (
	"bytes"
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"rsc.io/qr"

	"panel/internal/agent"
)

// Оформление сообщений бота. Текст — HTML Telegram, поэтому всё, что
// пришло извне (имена, ошибки, адреса), проходит через esc.

func esc(s string) string { return html.EscapeString(s) }

// bar — полоска заполнения из 10 делений: ▰▰▰▱▱▱▱▱▱▱.
func bar(pct int) string {
	n := max(0, min(10, int(math.Round(float64(pct)/10))))
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

func percent(part, total float64) int {
	if total <= 0 {
		return 0
	}
	return int(math.Round(part / total * 100))
}

// loadPct — средняя нагрузка в процентах от числа ядер, как в панели.
func loadPct(s agent.SystemInfo) int {
	if s.CPUs <= 0 {
		return 0
	}
	return percent(s.Load1, float64(s.CPUs))
}

func memPct(s agent.SystemInfo) int {
	return percent(float64(s.MemTotal-s.MemAvailable), float64(s.MemTotal))
}

func gib(b int64) string {
	switch {
	case b >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f ГБ", float64(b)/(1<<30)), ".", ",", 1)
	case b >= 1<<20:
		return fmt.Sprintf("%.0f МБ", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d КБ", b>>10)
	}
}

// bitrate — байты в секунду как скорость в битах, как в панели.
func bitrate(bytesPerSec float64) string {
	bits := max(0, bytesPerSec) * 8
	units := []string{"бит/с", "Кбит/с", "Мбит/с", "Гбит/с"}
	i := 0
	for bits >= 1000 && i < len(units)-1 {
		bits /= 1000
		i++
	}
	if bits < 10 {
		return strings.Replace(fmt.Sprintf("%.1f %s", bits, units[i]), ".", ",", 1)
	}
	return fmt.Sprintf("%.0f %s", bits, units[i])
}

func uptime(s int64) string {
	d := time.Duration(s) * time.Second
	if days := int(d.Hours()) / 24; days > 0 {
		return fmt.Sprintf("%d дн. %d ч", days, int(d.Hours())%24)
	}
	return fmt.Sprintf("%d ч %d мин", int(d.Hours()), int(d.Minutes())%60)
}

// plural выбирает форму слова: 1 день, 2 дня, 5 дней.
func plural(n int, one, few, many string) string {
	m := n % 100
	switch {
	case m >= 11 && m <= 14:
		return many
	case m%10 == 1:
		return one
	case m%10 >= 2 && m%10 <= 4:
		return few
	}
	return many
}

func days(n int) string { return fmt.Sprintf("%d %s", n, plural(n, "день", "дня", "дней")) }

// ago — «5 мин назад», «вчера 21:40», «03.11 12:00».
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "только что"
	case d < time.Hour:
		return fmt.Sprintf("%d мин назад", int(d.Minutes()))
	case t.Local().YearDay() == now.Local().YearDay() && t.Year() == now.Year():
		return "сегодня " + t.Local().Format("15:04")
	case t.Local().YearDay() == now.Local().AddDate(0, 0, -1).YearDay():
		return "вчера " + t.Local().Format("15:04")
	}
	return t.Local().Format("02.01 15:04")
}

// appName — короткое имя приложения из User-Agent: «v2RayTun/5.1 (…)» → «v2RayTun».
func appName(ua string) string {
	ua = strings.TrimSpace(ua)
	if i := strings.IndexAny(ua, "/ ;("); i > 0 {
		ua = ua[:i]
	}
	if len(ua) > 24 {
		ua = ua[:24]
	}
	if ua == "" {
		return "неизвестно"
	}
	return ua
}

func coreDot(c agent.CoreHealth) string {
	switch {
	case c.Running:
		return "🟢"
	case c.Message != "":
		return "🔴"
	}
	return "⚪"
}

// qrPNG — QR-код ссылки картинкой.
func qrPNG(text string) ([]byte, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	c.Scale = 8
	return bytes.Clone(c.PNG()), nil
}
