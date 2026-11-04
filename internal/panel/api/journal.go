package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"panel/internal/panel/store"
)

func logFilterFrom(r *http.Request) store.LogFilter {
	q := r.URL.Query()
	f := store.LogFilter{Source: q.Get("source"), MinLevel: q.Get("level"), Query: q.Get("q")}
	f.BeforeID, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	if len(f.Query) > 200 {
		f.Query = f.Query[:200]
	}
	return f
}

func (s *server) listLogs(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Store.QueryLogs(r.Context(), logFilterFrom(r))
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// streamLogs sends new log entries as Server-Sent Events until the client
// goes away. Filters work like listLogs.
func (s *server) streamLogs(w http.ResponseWriter, r *http.Request) {
	f := logFilterFrom(r)
	levels := store.LevelsAtLeast(f.MinLevel)
	match := func(e store.LogEntry) bool {
		return (f.Source == "" || e.Source == f.Source) &&
			(f.MinLevel == "" || slices.Contains(levels, e.Level)) &&
			(f.Query == "" || strings.Contains(strings.ToLower(e.Msg), strings.ToLower(f.Query)))
	}

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // the server's WriteTimeout would cut the stream
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	live, unsubscribe := s.Journal.Subscribe()
	defer unsubscribe()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-live:
			if !match(e) {
				continue
			}
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			rc.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			rc.Flush()
		}
	}
}

func (s *server) listEvents(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.Store.ListEvents(r.Context(), before, limit)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	records, err := s.Store.ListAudit(r.Context(), before, limit)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, records)
}
