// Package api exposes the panel's REST API and serves the web UI, all under
// the secret base path. Requests outside it never reach this package; see
// the stealth gate.
package api

import (
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"panel/internal/agent"
	"panel/internal/amnezia"
	"panel/internal/panel/auth"
	"panel/internal/panel/journal"
	"panel/internal/panel/service"
	"panel/internal/panel/store"
)

type Deps struct {
	BasePath   string // secret path segment, without slashes
	Auth       *auth.Service
	Users      *service.Users
	Inbounds   *service.Inbounds
	Syncer     *service.Syncer // nil without a local agent
	Events     *service.Events
	Journal    *journal.Hub
	Store      *store.Store // read-only views: logs, events, audit
	Scanner    *service.Scanner
	Routing    *service.Routing
	Domains    *service.Domains
	Amnezia    *amnezia.HelperClient // nil: no Amnezia tab data
	Reboot     func() error          // nil where rebooting is not possible (dev, tests)
	Panel      PanelConfig
	Agent      *agent.Agent // nil when the panel runs without a local agent
	Web        fs.FS        // built frontend (web/dist)
	Logger     *slog.Logger
	TLS        bool   // served over HTTPS: Secure cookies and HSTS
	PublicHost string // host clients connect to, for share links
	SubBaseURL string // e.g. https://203.0.113.5:41582/<subPath>/
	SubOverLAN bool   // subscriptions are served over plain HTTP on a LAN test setup
}

type server struct {
	Deps
	prefix       string // "/<basePath>"
	loginLimiter *rateLimiter
	onlineCache  *onlineCache
}

func New(d Deps) http.Handler {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	s := &server{Deps: d, prefix: "/" + d.BasePath, loginLimiter: newRateLimiter(10, time.Minute), onlineCache: &onlineCache{}}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Route(s.prefix, func(r chi.Router) {
		r.Route("/api", func(r chi.Router) {
			r.Use(noStore, s.logRequests, s.checkWrites)
			r.NotFound(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
			})
			r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			})

			r.Get("/health", s.health)
			r.Get("/auth/state", s.authState)
			r.With(s.limitLogin).Post("/auth/setup", s.setup)
			r.With(s.limitLogin).Post("/auth/login", s.login)
			r.Post("/auth/logout", s.logout)

			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Get("/system", s.system)
				r.Post("/system/restart-cores", s.restartCores)
				r.Post("/system/reboot", s.rebootServer)
				r.Post("/system/backup", s.downloadBackup)
				r.Get("/system/update", s.updateStatus)
				r.Post("/system/update", s.startUpdate)
				r.Route("/users", func(r chi.Router) {
					r.Get("/", s.listUsers)
					r.Post("/", s.createUser)
					r.Get("/{id}", s.getUser)
					r.Put("/{id}", s.updateUser)
					r.Delete("/{id}", s.deleteUser)
					r.Get("/{id}/links", s.userLinks)
					r.Get("/{id}/traffic", s.userTraffic)
					r.Post("/{id}/extend", s.extendUser)
					r.Post("/{id}/reset-traffic", s.resetUserTraffic)
				})
				r.Get("/online", s.online)
				r.Get("/amnezia", s.amneziaStatus)
				r.Get("/amnezia/logs", s.amneziaLogs)
				r.Get("/domain", s.getDomain)
				r.Put("/domain", s.saveDomain)
				r.Post("/domain/issue", s.issueCert)
				r.Delete("/domain", s.deleteDomain)
				r.Route("/routing", func(r chi.Router) {
					r.Get("/", s.getRouting)
					r.Put("/", s.saveRouting)
					r.Post("/warp", s.registerWarp)
					r.Post("/warp/license", s.warpLicense)
					r.Post("/warp/check", s.checkWarp)
					r.Delete("/warp", s.deleteWarp)
					r.Post("/geo/update", s.updateGeo)
					r.Get("/geo/codes", s.geoCodes)
					r.Get("/geo/countries", s.geoCountries)
					r.Post("/outbounds/{id}/check", s.checkOutbound)
				})
				r.Get("/stats/traffic", s.trafficStats)
				r.Get("/account", s.account)
				r.Post("/account/password", s.changePassword)
				r.Post("/account/totp/setup", s.totpSetup)
				r.Post("/account/totp/enable", s.totpEnable)
				r.Post("/account/totp/disable", s.totpDisable)
				r.Get("/account/sessions", s.listSessions)
				r.Delete("/account/sessions/{id}", s.revokeSession)
				r.Post("/account/sessions/revoke-others", s.revokeOtherSessions)
				r.Get("/settings/panel", s.panelSettings)
				r.Put("/settings/panel", s.savePanelSettings)
				r.Post("/settings/restart", s.restartPanel)
				r.Get("/logs", s.listLogs)
				r.Get("/logs/stream", s.streamLogs)
				r.Get("/events", s.listEvents)
				r.Get("/audit", s.listAudit)
				r.Route("/inbounds", func(r chi.Router) {
					r.Get("/", s.listInbounds)
					r.Post("/", s.createInbound)
					r.Get("/suggest", s.suggestInbound)
					r.Post("/test-kit", s.createTestKit)
					r.Post("/scan-targets", s.scanTargets)
					r.Delete("/test-kit", s.deleteTestKit)
					r.Get("/{id}", s.getInbound)
					r.Put("/{id}", s.updateInbound)
					r.Delete("/{id}", s.deleteInbound)
				})
			})
		})
		r.Handle("/*", http.StripPrefix(s.prefix, spaHandler(d.Web, s.prefix+"/")))
	})
	return r
}

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// The URL contains the secret path; never leak it to other sites.
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.TLS {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// checkWrites guards state-changing requests against CSRF: browsers cannot
// send a cross-site application/json request without a CORS preflight (and
// CORS is never enabled), and any Origin header must match our own host.
func (s *server) checkWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
					"requests that change data must be application/json")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "bad_origin", "cross-origin request rejected")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// Log the path without the secret prefix: logs end up in places
		// (journal, the panel's own log viewer) the URL should not.
		path := strings.TrimPrefix(r.URL.Path, s.prefix)
		if path == "/api/health" {
			return
		}
		s.Logger.Info("api request", "method", r.Method, "path", path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds(), "ip", clientIP(r))
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
