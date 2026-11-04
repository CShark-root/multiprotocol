package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/panel/compile"
	"panel/internal/panel/store"
)

type onlineDTO struct {
	UserID      int64    `json:"userId"`
	IPs         []string `json:"ips"`
	Connections int      `json:"connections"`
}

// onlineCache keeps the cores from being asked more than once every few
// seconds when several panel tabs poll at the same time.
type onlineCache struct {
	mu   sync.Mutex
	at   time.Time
	list []onlineDTO
}

const onlineTTL = 3 * time.Second

func (s *server) online(w http.ResponseWriter, r *http.Request) {
	list, err := s.onlineNow(r)
	if err != nil {
		s.Logger.Warn("online users", "err", err)
	}
	writeJSON(w, http.StatusOK, list)
}

// onlineNow returns who is connected right now. A core that fails to answer
// is logged and skipped, so one broken core does not hide the others.
func (s *server) onlineNow(r *http.Request) ([]onlineDTO, error) {
	if s.Agent == nil {
		return []onlineDTO{}, nil
	}
	c := s.onlineCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < onlineTTL && c.list != nil {
		return c.list, nil
	}
	m, err := s.Agent.Online(r.Context())
	c.list, c.at = toOnlineDTO(m), time.Now()
	return c.list, err
}

func toOnlineDTO(m map[string]agent.OnlineEntry) []onlineDTO {
	out := make([]onlineDTO, 0, len(m))
	for email, e := range m {
		id, ok := compile.UserIDFromEmail(email)
		if !ok {
			continue
		}
		ips := e.IPs
		if ips == nil {
			ips = []string{}
		}
		out = append(out, onlineDTO{UserID: id, IPs: ips, Connections: max(e.Connections, len(ips))})
	}
	return out
}

func (s *server) trafficStats(w http.ResponseWriter, r *http.Request) {
	s.writeDaily(w, r, 0)
}

func (s *server) userTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.Users.Get(r.Context(), id); err != nil {
		s.handleError(w, r, err)
		return
	}
	s.writeDaily(w, r, id)
}

// writeDaily answers with one entry per day, oldest first, zero-filled so
// a chart has no gaps. ?days= picks the window (1-366, default 30).
func (s *server) writeDaily(w http.ResponseWriter, r *http.Request, userID int64) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 366 {
			writeError(w, http.StatusBadRequest, "bad_days", "days must be 1-366")
			return
		}
		days = n
	}
	now := s.Users.Now().Local()
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	rows, err := s.Store.TrafficByDay(r.Context(), userID, first.Format(time.DateOnly))
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, fillDays(rows, first, days))
}

func fillDays(rows []store.DayTraffic, first time.Time, days int) []store.DayTraffic {
	by := make(map[string]int64, len(rows))
	for _, d := range rows {
		by[d.Day] = d.Bytes
	}
	out := make([]store.DayTraffic, days)
	for i := range out {
		day := first.AddDate(0, 0, i).Format(time.DateOnly)
		out[i] = store.DayTraffic{Day: day, Bytes: by[day]}
	}
	return out
}

type extendDTO struct {
	Days int `json:"days"`
}

func (s *server) extendUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in extendDTO
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := s.Users.Extend(r.Context(), adminFrom(r.Context()).ID, id, in.Days)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toUserDTO(u))
}

func (s *server) resetUserTraffic(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.ResetTraffic(r.Context(), adminFrom(r.Context()).ID, id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toUserDTO(u))
}
