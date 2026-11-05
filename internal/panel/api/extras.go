package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/service"
	"panel/internal/panel/store"
	"panel/internal/panel/subscription"
	"panel/internal/panel/validate"
)

// --- настройки подписки ---

func (s *server) subSettings(w http.ResponseWriter, r *http.Request) {
	ss := subscription.DefaultSettings()
	s.Store.GetSetting(r.Context(), subscription.SettingsKey, &ss)
	writeJSON(w, http.StatusOK, ss)
}

func (s *server) saveSubSettings(w http.ResponseWriter, r *http.Request) {
	var in subscription.Settings
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Title, in.Announce, in.SupportURL = strings.TrimSpace(in.Title), strings.TrimSpace(in.Announce), strings.TrimSpace(in.SupportURL)
	bad := func(v string, n int) bool { return len([]rune(v)) > n || strings.ContainsAny(v, "\x00\r\n") }
	switch {
	case bad(in.Title, 60):
		s.handleError(w, r, validate.Fail("title", "up to 60 characters"))
		return
	case bad(in.Announce, 200):
		s.handleError(w, r, validate.Fail("announce", "up to 200 characters, one line"))
		return
	case bad(in.SupportURL, 200) || in.SupportURL != "" && !strings.HasPrefix(in.SupportURL, "https://") && !strings.HasPrefix(in.SupportURL, "tg://"):
		s.handleError(w, r, validate.Fail("supportUrl", "https://… or tg://…"))
		return
	case in.Interval < 1 || in.Interval > 168:
		s.handleError(w, r, validate.Fail("interval", "1-168 hours"))
		return
	}
	if err := s.Store.SetSetting(r.Context(), subscription.SettingsKey, in); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.Store.AddAudit(r.Context(), store.AuditEntry{AdminID: adminFrom(r.Context()).ID, Action: "subscription.settings", TargetType: "system",
		Diff: map[string]any{"title": in.Title, "interval": in.Interval, "announce": in.Announce != ""}}, time.Now())
	writeJSON(w, http.StatusOK, in)
}

// --- обновления подписки пользователя ---

func (s *server) userFetches(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	list, err := s.Store.ListSubFetches(r.Context(), id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// lastFetches — последнее обновление подписки у каждого пользователя.
func (s *server) lastFetches(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.LastSubFetches(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	out := map[string]store.SubFetch{}
	for id, f := range m {
		out[strconv.FormatInt(id, 10)] = f
	}
	writeJSON(w, http.StatusOK, out)
}

// --- оплата VPS ---

type billingInput struct {
	PaidUntil string `json:"paidUntil"` // "2026-12-01" или ""
	Price     string `json:"price"`
}

func (s *server) mainBilling(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Billing.Main(r.Context()))
}

func (s *server) saveMainBilling(w http.ResponseWriter, r *http.Request) {
	var in billingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	paid, price, err := service.ParseBilling(in.PaidUntil, in.Price)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	b := service.ServerBilling{PaidUntil: paid, Price: price}
	if err := s.Billing.SetMain(r.Context(), b); err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) saveNodeBilling(w http.ResponseWriter, r *http.Request) {
	id, ok := nodeID(r)
	var in billingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such server")
		return
	}
	paid, price, err := service.ParseBilling(in.PaidUntil, in.Price)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	if err := s.Store.SetNodeBilling(r.Context(), id, paid, price); err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, service.ServerBilling{PaidUntil: paid, Price: price})
}
