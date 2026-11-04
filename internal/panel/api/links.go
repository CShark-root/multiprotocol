package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"panel/internal/panel/config"
	"panel/internal/panel/routing"
	"panel/internal/panel/service"
	"panel/internal/panel/stealth"
	"panel/internal/panel/store"
	"panel/internal/panel/subscription"
)

type linkDTO struct {
	InboundID int64    `json:"inboundId"`
	Remark    string   `json:"remark"`
	Protocol  string   `json:"protocol"`
	Transport string   `json:"transport"`
	Apps      []string `json:"apps"` // client app families that can use it
	URL       string   `json:"url"`  // "" when the protocol has no share-link format
}

func (s *server) userLinks(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u, err := s.Users.Get(r.Context(), id)
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	inbounds, err := s.Inbounds.List(r.Context())
	if err != nil {
		s.handleError(w, r, err)
		return
	}
	links := []linkDTO{}
	for _, ib := range inbounds {
		if ib.Enabled {
			links = append(links, linkDTO{
				InboundID: ib.ID, Remark: ib.Remark, Protocol: ib.Protocol, Transport: ib.Transport,
				Apps: service.ClientApps(ib.Protocol), URL: subscription.Link(ib, u, s.PublicHost),
			})
		}
	}
	sub := s.SubBaseURL + u.SubToken
	writeJSON(w, http.StatusOK, map[string]any{
		"subscriptionUrl": sub,
		"singboxUrl":      sub + "?format=singbox",
		"overLan":         s.SubOverLAN, // plain HTTP, test VM only
		"links":           links,
	})
}

// rand.Text() tokens: 26 characters of base32.
var subTokenRe = regexp.MustCompile(`^[A-Z2-7]{26}$`)

// SubscriptionDeps is what the public subscription endpoint needs. It is a
// separate handler from the panel API: knowing a subscription link must not
// reveal where the panel lives.
type SubscriptionDeps struct {
	SubPath    string
	Store      *store.Store
	PublicHost string
	Decoy      config.DecoyMode
}

// wantsSingBox picks the sing-box JSON format for ?format=singbox and for
// the official sing-box apps (SFA/SFI/SFM/SFT user agents).
func wantsSingBox(r *http.Request) bool {
	if f := r.URL.Query().Get("format"); f != "" {
		return f == "singbox"
	}
	ua := strings.ToLower(r.UserAgent())
	for _, s := range []string{"sing-box", "sfa/", "sfi/", "sfm/", "sft/"} {
		if strings.Contains(ua, s) {
			return true
		}
	}
	return false
}

func NewSubscription(d SubscriptionDeps) http.Handler {
	prefix := "/" + d.SubPath + "/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, prefix)
		if r.Method != http.MethodGet || !subTokenRe.MatchString(token) {
			stealth.Decoy(w, d.Decoy)
			return
		}
		u, err := d.Store.GetUserBySubToken(r.Context(), token)
		if err != nil {
			// Unknown and failing tokens look the same as any other miss.
			stealth.Decoy(w, d.Decoy)
			return
		}
		inbounds, err := d.Store.ListInbounds(r.Context())
		if err != nil {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Profile-Title", subscription.ProfileTitle(u))
		h.Set("Subscription-Userinfo", subscription.UserInfo(u))
		h.Set("Profile-Update-Interval", strconv.Itoa(int(subscription.UpdateInterval.Hours())))
		if wantsSingBox(r) {
			rs := routing.Defaults()
			d.Store.GetSetting(r.Context(), "routing", &rs)
			body, err := subscription.SingBoxConfig(inbounds, u, d.PublicHost, rs.Client)
			if err != nil {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			h.Set("Content-Type", "application/json; charset=utf-8")
			w.Write(body)
			return
		}
		h.Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(subscription.Body(subscription.Links(inbounds, u, d.PublicHost))))
	})
}
