package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

// Оплата VPS: до какого дня оплачен каждый сервер. Бот напоминает за 3 дня,
// накануне, в последний день и на следующий день, если продлить забыли.

const (
	EventBillingDue = "billing.due"

	mainBillingKey = "billing.main"
	billingSentKey = "billing.sent"
)

// remindDays — за сколько дней до конца оплаты напоминать (-1 — уже просрочено).
var remindDays = []int{3, 1, 0, -1}

type ServerBilling struct {
	PaidUntil *time.Time `json:"paidUntil"`
	Price     string     `json:"price"`
}

type Billing struct {
	store  *store.Store
	events *Events
	log    *slog.Logger
	now    func() time.Time
}

func NewBilling(st *store.Store, events *Events, log *slog.Logger) *Billing {
	return &Billing{store: st, events: events, log: log, now: time.Now}
}

// ParseBilling проверяет ввод: дата "2026-12-01" или пусто, цена до 40 символов.
func ParseBilling(date, price string) (*time.Time, string, error) {
	price = strings.TrimSpace(price)
	if len(price) > 40 || strings.ContainsAny(price, "\x00\r\n") {
		return nil, "", validate.Fail("price", "up to 40 characters")
	}
	date = strings.TrimSpace(date)
	if date == "" {
		return nil, price, nil
	}
	t, err := time.ParseInLocation(time.DateOnly, date, time.Local)
	if err != nil {
		return nil, "", validate.Fail("paidUntil", "a date like 2026-12-01")
	}
	return &t, price, nil
}

func (b *Billing) Main(ctx context.Context) ServerBilling {
	var s ServerBilling
	b.store.GetSetting(ctx, mainBillingKey, &s)
	return s
}

func (b *Billing) SetMain(ctx context.Context, s ServerBilling) error {
	return b.store.SetSetting(ctx, mainBillingKey, s)
}

func day(t time.Time) time.Time {
	y, m, d := t.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// daysLeft — сколько дней до конца оплаты (0 — сегодня последний день).
func daysLeft(paid, now time.Time) int {
	return int(math.Round(day(paid).Sub(day(now)).Hours() / 24))
}

// Run раз в час (после 10 утра по часам сервера) проверяет сроки оплаты.
func (b *Billing) Run(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if b.now().Hour() >= 10 {
			b.check(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (b *Billing) check(ctx context.Context) {
	type srv struct {
		name string
		s    ServerBilling
	}
	list := []srv{{"Этот сервер", b.Main(ctx)}}
	if nodes, err := b.store.ListNodes(ctx); err == nil {
		for _, n := range nodes {
			list = append(list, srv{n.Name, ServerBilling{PaidUntil: n.PaidUntil, Price: n.Price}})
		}
	}
	var sent []string
	b.store.GetSetting(ctx, billingSentKey, &sent)
	changed := false
	now := b.now()
	for _, s := range list {
		if s.s.PaidUntil == nil {
			continue
		}
		left := daysLeft(*s.s.PaidUntil, now)
		if !slices.Contains(remindDays, left) {
			continue
		}
		key := fmt.Sprintf("%s|%s|%d", s.name, s.s.PaidUntil.Local().Format(time.DateOnly), left)
		if slices.Contains(sent, key) {
			continue
		}
		b.events.Add(ctx, EventBillingDue, "warning", 0, "", map[string]any{
			"server": s.name, "date": s.s.PaidUntil.Local().Format("02.01.2006"), "days": left, "price": s.s.Price,
		})
		sent = append(sent, key)
		changed = true
	}
	if changed {
		if len(sent) > 200 {
			sent = sent[len(sent)-200:]
		}
		b.store.SetSetting(ctx, billingSentKey, sent)
	}
}

// billingText — текст напоминания для бота.
func billingText(d map[string]any) string {
	days, _ := d["days"].(int)
	if f, ok := d["days"].(float64); ok {
		days = int(f)
	}
	when := ""
	switch {
	case days > 1:
		when = fmt.Sprintf("оплата через %d дня", days)
	case days == 1:
		when = "оплата завтра"
	case days == 0:
		when = "сегодня последний оплаченный день"
	default:
		when = "срок оплаты прошёл"
	}
	out := "💳 «" + str(d, "server") + "»: " + when + " (до " + str(d, "date")
	if p := str(d, "price"); p != "" {
		out += ", " + p
	}
	return out + ")"
}
