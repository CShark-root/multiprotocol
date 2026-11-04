package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"panel/internal/panel/geo"
	"panel/internal/panel/outbound"
	"panel/internal/panel/routing"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

// warpDTO shows the device without its keys.
type warpDTO struct {
	Mode         string     `json:"mode"`
	Registered   bool       `json:"registered"`
	WarpPlus     bool       `json:"warpPlus"`
	Address4     string     `json:"address4,omitempty"`
	Address6     string     `json:"address6,omitempty"`
	Endpoint     string     `json:"endpoint,omitempty"`
	RegisteredAt *time.Time `json:"registeredAt,omitempty"`
}

type ruleDTO struct {
	routing.Rule
	Support routing.Support `json:"support"`
}

type outDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type routingDTO struct {
	Presets   routing.Presets     `json:"presets"`
	Rules     []ruleDTO           `json:"rules"`
	WARP      warpDTO             `json:"warp"`
	DNS       routing.DNSSettings `json:"dns"`
	Geo       routing.GeoSettings `json:"geo"`
	Services  []string            `json:"services"`
	Outbounds []outDTO            `json:"outbounds"`
	Client    string              `json:"client"`
	GeoState  struct {
		service.GeoStatus
		Files []geo.Info `json:"files"`
	} `json:"geoState"`
}

// routingInput is what the UI may change.
type routingInput struct {
	Presets routing.Presets `json:"presets"`
	Rules   []routing.Rule  `json:"rules"`
	WARP    struct {
		Mode string `json:"mode"`
	} `json:"warp"`
	DNS    routing.DNSSettings `json:"dns"`
	Geo    routing.GeoSettings `json:"geo"`
	Client string              `json:"client"`
	// Exits: existing ones by id (their link stays on the server), new ones
	// with a link and no id.
	Outbounds []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Link string `json:"link"`
	} `json:"outbounds"`
}

func (s *server) toRoutingDTO(r *http.Request, st routing.Settings) routingDTO {
	d := routingDTO{Presets: st.Presets, DNS: st.DNS, Geo: st.Geo, Rules: make([]ruleDTO, len(st.Rules))}
	for i, rule := range st.Rules {
		d.Rules[i] = ruleDTO{Rule: rule, Support: rule.Support()}
	}
	d.WARP.Mode = st.WARP.Mode
	if a := st.WARP.Account; a != nil {
		at := a.RegisteredAt
		d.WARP = warpDTO{Mode: st.WARP.Mode, Registered: true, WarpPlus: a.WarpPlus, Address4: a.Address4, Address6: a.Address6, Endpoint: a.Endpoint, RegisteredAt: &at}
	}
	for _, sv := range routing.Services {
		d.Services = append(d.Services, sv.Key)
	}
	d.Client = st.Client
	d.Outbounds = []outDTO{}
	for _, o := range st.Outbounds {
		summary := ""
		if p, err := outbound.Parse(o.Link); err == nil {
			summary = p.Describe()
		}
		d.Outbounds = append(d.Outbounds, outDTO{ID: o.ID, Name: o.Name, Summary: summary})
	}
	d.GeoState.GeoStatus = s.Routing.GeoStatus(r.Context())
	d.GeoState.Files = geo.Installed(s.Routing.GeoDir())
	if d.GeoState.Files == nil {
		d.GeoState.Files = []geo.Info{}
	}
	return d
}

func (s *server) getRouting(w http.ResponseWriter, r *http.Request) {
	st, err := s.Routing.Get(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, st))
}

func (s *server) saveRouting(w http.ResponseWriter, r *http.Request) {
	var in routingInput
	if !decodeJSON(w, r, &in) {
		return
	}
	st := routing.Settings{Presets: in.Presets, Rules: in.Rules, DNS: in.DNS, Geo: in.Geo}
	st.WARP.Mode = in.WARP.Mode
	st.Client = in.Client
	for _, o := range in.Outbounds {
		st.Outbounds = append(st.Outbounds, routing.CustomOutbound{ID: o.ID, Name: o.Name, Link: o.Link})
	}
	saved, err := s.Routing.Save(r.Context(), adminFrom(r.Context()).ID, st)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, saved))
}

// External calls (Cloudflare, GitHub) fail for reasons outside the panel;
// they come back as 502 with the reason rather than as a server error.
func (s *server) upstreamError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, service.ErrNoWarp) {
		writeError(w, http.StatusConflict, "no_warp", err.Error())
		return
	}
	var ve *validate.Error
	if errors.As(err, &ve) {
		s.handleError(w, r, err)
		return
	}
	writeError(w, http.StatusBadGateway, "upstream", err.Error())
}

func (s *server) registerWarp(w http.ResponseWriter, r *http.Request) {
	st, err := s.Routing.RegisterWarp(r.Context(), adminFrom(r.Context()).ID)
	if err != nil {
		s.upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, st))
}

func (s *server) warpLicense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		License string `json:"license"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	st, err := s.Routing.SetWarpLicense(r.Context(), adminFrom(r.Context()).ID, strings.TrimSpace(in.License))
	if err != nil {
		s.upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, st))
}

func (s *server) deleteWarp(w http.ResponseWriter, r *http.Request) {
	st, err := s.Routing.DeleteWarp(r.Context(), adminFrom(r.Context()).ID)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, st))
}

func (s *server) checkWarp(w http.ResponseWriter, r *http.Request) {
	through, direct, err := s.Routing.CheckWarp(r.Context())
	out := map[string]any{"direct": direct}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["warp"] = through
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) updateGeo(w http.ResponseWriter, r *http.Request) {
	changed, err := s.Routing.UpdateGeo(r.Context())
	if err != nil {
		s.upstreamError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"changed": changed})
}

// geoCodes suggests categories for the rule editor.
func (s *server) geoCodes(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	sites, ips := s.Routing.GeoCodes()
	var out []string
	add := func(prefix string, list []string) {
		for _, c := range list {
			if len(out) >= 40 {
				return
			}
			if q == "" || strings.Contains(prefix+c, q) {
				out = append(out, prefix+c)
			}
		}
	}
	add("geosite:", sites)
	add("geoip:", ips)
	if out == nil {
		out = []string{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) geoCountries(w http.ResponseWriter, r *http.Request) {
	list, ruBlocked := s.Routing.Countries()
	writeJSON(w, http.StatusOK, map[string]any{"countries": list, "ruBlocked": ruBlocked})
}

func (s *server) checkOutbound(w http.ResponseWriter, r *http.Request) {
	tr, err := s.Routing.CheckOutbound(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrNotFound) {
		s.handleError(w, r, err)
		return
	}
	out := map[string]any{}
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["trace"] = tr
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) routingHistory(w http.ResponseWriter, r *http.Request) {
	type item struct {
		At      time.Time `json:"at"`
		Changed []string  `json:"changed"`
	}
	out := []item{}
	for _, v := range s.Routing.History(r.Context()) {
		out = append(out, item{At: v.At, Changed: v.Changed})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) routingRollback(w http.ResponseWriter, r *http.Request) {
	i, err := strconv.Atoi(chi.URLParam(r, "i"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_index", "bad version")
		return
	}
	st, err := s.Routing.Rollback(r.Context(), adminFrom(r.Context()).ID, i)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toRoutingDTO(r, st))
}
