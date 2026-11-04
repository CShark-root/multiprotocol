package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/compile"
	"panel/internal/panel/geo"
	"panel/internal/panel/routing"
	"panel/internal/panel/store"
	"panel/internal/panel/validate"
	"panel/internal/panel/warp"
)

const (
	routingKey   = "routing"
	geoStatusKey = "geo_status"
	geoEvery     = 24 * time.Hour

	EventGeoUpdated = "geo.updated"
	EventGeoFailed  = "geo.failed"
)

var ErrNoWarp = errors.New("WARP is not registered")

// GeoStatus is what the panel remembers about database updates.
type GeoStatus struct {
	LastCheck   time.Time `json:"lastCheck"`
	LastUpdate  time.Time `json:"lastUpdate"`
	LastError   string    `json:"lastError"`
	LastSource  string    `json:"lastSource"`
	UpdateCount int       `json:"updateCount"`
}

// Routing owns the routing settings, the WARP device and the geo
// databases, and prepares the routing input for the compilers.
type Routing struct {
	store   *store.Store
	events  *Events
	geoDir  string
	changed func()
	restart func() error // restarts cores that read the geo files at start
	log     *slog.Logger
	now     func() time.Time

	mu        sync.Mutex // serializes settings writes and geo updates
	cacheKey  string
	cacheSite map[string]geo.Site
	cacheIP   map[string][]netip.Prefix
	codesKey  string
	codes     map[string]bool // "geosite:openai", "geoip:ru" present in the databases

	listMu    sync.Mutex
	listKey   string
	listSites []string
	listIPs   []string
}

func NewRouting(st *store.Store, events *Events, geoDir string, changed func(), restart func() error, log *slog.Logger) *Routing {
	if changed == nil {
		changed = func() {}
	}
	if restart == nil {
		restart = func() error { return nil }
	}
	return &Routing{store: st, events: events, geoDir: geoDir, changed: changed, restart: restart, log: log, now: time.Now}
}

func (r *Routing) GeoDir() string { return r.geoDir }

// Get returns the saved settings, or the defaults.
func (r *Routing) Get(ctx context.Context) (routing.Settings, error) {
	s := routing.Defaults()
	if _, err := r.store.GetSetting(ctx, routingKey, &s); err != nil {
		return s, err
	}
	if s.Rules == nil {
		s.Rules = []routing.Rule{}
	}
	if s.Presets.WarpServices == nil {
		s.Presets.WarpServices = []string{}
	}
	return s, nil
}

// Save stores settings from the UI. The WARP device and the bridge are
// managed here, never taken from the request.
func (r *Routing) Save(ctx context.Context, adminID int64, in routing.Settings) (routing.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, err := r.Get(ctx)
	if err != nil {
		return cur, err
	}
	users, err := r.store.ListUsers(ctx)
	if err != nil {
		return cur, err
	}
	exists := func(id int64) bool {
		return slices.ContainsFunc(users, func(u store.User) bool { return u.ID == id })
	}
	in.WARP.Account, in.Bridge = cur.WARP.Account, cur.Bridge
	next, err := routing.Normalize(in, exists)
	if err != nil {
		return cur, err
	}
	if next.WARP.Mode != routing.WarpOff && next.WARP.Account == nil {
		return cur, validate.Fail("warp.mode", "register WARP first")
	}
	if err := r.store.SetSetting(ctx, routingKey, next); err != nil {
		return cur, err
	}
	r.audit(ctx, adminID, "routing.update", "routing", "", map[string]any{
		"rules": len(next.Rules), "warp": next.WARP.Mode, "dns": next.DNS.Upstream, "geo": next.Geo.Source,
	})
	r.changed()
	return next, nil
}

// RegisterWarp creates a WARP device (replacing an existing one) and the
// loopback bridge other cores reach it through.
func (r *Routing) RegisterWarp(ctx context.Context, adminID int64) (routing.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.Get(ctx)
	if err != nil {
		return s, err
	}
	acc, err := warp.Register(ctx)
	if err != nil {
		return s, err
	}
	if old := s.WARP.Account; old != nil {
		warp.Delete(ctx, old) // the old device is useless now; best effort
	}
	s.WARP.Account = acc
	if s.WARP.Mode == routing.WarpOff {
		s.WARP.Mode = routing.WarpRules
	}
	if s.Bridge.Port == 0 {
		port, err := freeLoopbackPort()
		if err != nil {
			return s, err
		}
		s.Bridge = routing.Bridge{Port: port, User: "warp", Password: randomToken(16)}
	}
	if err := r.store.SetSetting(ctx, routingKey, s); err != nil {
		return s, err
	}
	r.audit(ctx, adminID, "warp.register", "warp", acc.ID, nil)
	r.changed()
	return s, nil
}

// SetWarpLicense applies a WARP+ key.
func (r *Routing) SetWarpLicense(ctx context.Context, adminID int64, license string) (routing.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.Get(ctx)
	if err != nil {
		return s, err
	}
	if s.WARP.Account == nil {
		return s, ErrNoWarp
	}
	if len(license) < 8 || len(license) > 64 {
		return s, validate.Fail("license", "a WARP+ key looks like xxxxxxxx-xxxxxxxx-xxxxxxxx")
	}
	plus, err := warp.SetLicense(ctx, s.WARP.Account, license)
	if err != nil {
		return s, err
	}
	s.WARP.Account.License, s.WARP.Account.WarpPlus = license, plus
	if err := r.store.SetSetting(ctx, routingKey, s); err != nil {
		return s, err
	}
	r.audit(ctx, adminID, "warp.license", "warp", s.WARP.Account.ID, nil)
	return s, nil
}

// DeleteWarp forgets the device and turns WARP off.
func (r *Routing) DeleteWarp(ctx context.Context, adminID int64) (routing.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.Get(ctx)
	if err != nil {
		return s, err
	}
	if s.WARP.Account != nil {
		warp.Delete(ctx, s.WARP.Account)
	}
	s.WARP = routing.WarpSettings{Mode: routing.WarpOff}
	if err := r.store.SetSetting(ctx, routingKey, s); err != nil {
		return s, err
	}
	r.audit(ctx, adminID, "warp.delete", "warp", "", nil)
	r.changed()
	return s, nil
}

// CheckWarp fetches Cloudflare's trace through WARP, and directly for
// comparison.
func (r *Routing) CheckWarp(ctx context.Context) (through, direct warp.Trace, err error) {
	s, err := r.Get(ctx)
	if err != nil {
		return
	}
	direct, _ = warp.Check(ctx, nil)
	if !s.WarpUsable() || s.Bridge.Port == 0 {
		return through, direct, ErrNoWarp
	}
	through, err = warp.Check(ctx, &s.Bridge)
	return through, direct, err
}

func (r *Routing) GeoStatus(ctx context.Context) GeoStatus {
	var st GeoStatus
	r.store.GetSetting(ctx, geoStatusKey, &st)
	return st
}

// UpdateGeo downloads the databases from the configured source now.
func (r *Routing) UpdateGeo(ctx context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, err := r.Get(ctx)
	if err != nil {
		return false, err
	}
	ipURL, siteURL := s.Geo.GeoIPURL, s.Geo.GeoSiteURL
	if src, ok := routing.GeoSources[s.Geo.Source]; ok {
		ipURL, siteURL = src[0], src[1]
	}
	st := r.GeoStatus(ctx)
	st.LastCheck = r.now().UTC()
	changed, err := geo.Update(ctx, r.geoDir, ipURL, siteURL)
	if err != nil {
		st.LastError = err.Error()
		r.store.SetSetting(ctx, geoStatusKey, st)
		r.events.Add(ctx, EventGeoFailed, "warning", 0, "", map[string]any{"error": err.Error()})
		return false, err
	}
	st.LastError = ""
	if changed || st.LastSource != s.Geo.Source {
		st.LastUpdate, st.LastSource = st.LastCheck, s.Geo.Source
		st.UpdateCount++
	}
	r.store.SetSetting(ctx, geoStatusKey, st)
	if changed {
		r.cacheKey = ""
		r.events.Add(ctx, EventGeoUpdated, "info", 0, "", map[string]any{"source": s.Geo.Source})
		if err := r.restart(); err != nil {
			r.log.Warn("restart cores after geo update", "err", err)
		}
		r.changed() // sing-box gets the new lists inlined
	}
	return changed, nil
}

// RunGeoUpdates keeps the databases fresh: shortly after start, then daily.
func (r *Routing) RunGeoUpdates(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s, err := r.Get(ctx)
		st := r.GeoStatus(ctx)
		if err == nil && s.Geo.AutoUpdate && r.now().Sub(st.LastCheck) >= geoEvery-time.Minute {
			if _, err := r.UpdateGeo(ctx); err != nil {
				r.log.Warn("geo update failed", "err", err)
			}
		}
		t.Reset(time.Hour)
	}
}

// GeoCodes lists the categories of the installed databases, read once per
// database version (runetfreedom's geosite.dat alone is ~86 MB).
func (r *Routing) GeoCodes() (sites, ips []string) {
	files := r.fileKey()
	r.listMu.Lock()
	defer r.listMu.Unlock()
	if files != r.listKey {
		r.listSites, _ = geo.Codes(filepath.Join(r.geoDir, "geosite.dat"))
		r.listIPs, _ = geo.Codes(filepath.Join(r.geoDir, "geoip.dat"))
		r.listKey = files
		debug.FreeOSMemory()
	}
	return r.listSites, r.listIPs
}

// hysteriaGeo writes (once per database version and set of categories) a
// directory with geo files trimmed to what the rules use, and returns it.
// The name changes with the content, so Hysteria restarts by itself.
func (r *Routing) hysteriaGeo(key string, sites, ips []string) string {
	if len(sites)+len(ips) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	name := "hysteria-" + hex.EncodeToString(sum[:6])
	dir := filepath.Join(r.geoDir, name)
	if _, err := os.Stat(filepath.Join(dir, "geoip.dat")); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			r.log.Warn("hysteria geo subset", "err", err)
			return r.geoDir
		}
		err1 := geo.Subset(filepath.Join(r.geoDir, "geosite.dat"), filepath.Join(dir, "geosite.dat"), sites)
		err2 := geo.Subset(filepath.Join(r.geoDir, "geoip.dat"), filepath.Join(dir, "geoip.dat"), ips)
		if err := errors.Join(err1, err2); err != nil {
			r.log.Warn("hysteria geo subset", "err", err)
			os.RemoveAll(dir)
			return r.geoDir
		}
		debug.FreeOSMemory()
	}
	// Older subsets go once they cannot be needed for a rollback any more.
	if old, _ := filepath.Glob(filepath.Join(r.geoDir, "hysteria-*")); len(old) > 0 {
		for _, o := range old {
			if st, err := os.Stat(o); err == nil && filepath.Base(o) != name && time.Since(st.ModTime()) > time.Hour {
				os.RemoveAll(o)
			}
		}
	}
	return dir
}

// Compile prepares the routing input for the compilers. The geo categories
// sing-box needs are extracted once per database version.
func (r *Routing) Compile(ctx context.Context) (compile.Routing, error) {
	s, err := r.Get(ctx)
	if err != nil {
		return compile.Routing{}, err
	}
	files := r.fileKey()
	r.mu.Lock()
	if files != r.codesKey {
		siteCodes, ipCodes := r.GeoCodes()
		set := make(map[string]bool, len(siteCodes)+len(ipCodes))
		for _, c := range siteCodes {
			set["geosite:"+c] = true
		}
		for _, c := range ipCodes {
			set["geoip:"+c] = true
		}
		r.codesKey, r.codes = files, set
	}
	codes := r.codes
	r.mu.Unlock()
	has := func(entry string) bool {
		name, _, _ := strings.Cut(entry, "@")
		return codes[name]
	}
	rules, _ := s.Expand(compile.Email, has)
	sites, ips := routing.GeoNames(rules)
	key := files + "|" + joinKey(sites, ips)
	r.mu.Lock()
	if key != r.cacheKey {
		siteMap, err1 := geo.Sites(filepath.Join(r.geoDir, "geosite.dat"), sites)
		ipMap, err2 := geo.IPs(filepath.Join(r.geoDir, "geoip.dat"), ips)
		if err := errors.Join(err1, err2); err != nil && len(sites)+len(ips) > 0 {
			r.log.Warn("read geo databases", "err", err)
		}
		r.cacheKey, r.cacheSite, r.cacheIP = key, siteMap, ipMap
		// The databases are read whole (tens of MB) to keep a few categories;
		// hand that memory back now rather than at the next GC cycle.
		debug.FreeOSMemory()
	}
	siteMap, ipMap := r.cacheSite, r.cacheIP
	r.mu.Unlock()
	return compile.NewRouting(s, r.hysteriaGeo(key, sites, ips), has, siteMap, ipMap), nil
}

func joinKey(sites, ips []string) string {
	k := ""
	for _, s := range sites {
		k += "s:" + s + ","
	}
	for _, s := range ips {
		k += "i:" + s + ","
	}
	return k
}

func (r *Routing) fileKey() string {
	k := ""
	for _, i := range geo.Installed(r.geoDir) {
		k += i.Name + strconv.FormatInt(i.Modified.UnixNano(), 10) + strconv.FormatInt(i.Size, 10)
	}
	return k
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (r *Routing) audit(ctx context.Context, adminID int64, action, targetType, targetID string, diff any) {
	r.store.AddAudit(ctx, store.AuditEntry{AdminID: adminID, Action: action, TargetType: targetType, TargetID: targetID, Diff: diff}, r.now())
}

// Country is a country the geoip database knows, for the country picker.
type Country struct {
	Code    string `json:"code"`
	HasSite bool   `json:"hasSite"` // a geosite category-<code> exists too
}

// Countries lists two-letter geoip codes and whether the ru-blocked lists
// are installed.
func (r *Routing) Countries() (list []Country, ruBlocked bool) {
	sites, ips := r.GeoCodes()
	list = []Country{}
	for _, c := range ips {
		if len(c) == 2 && c[0] >= 'a' && c[0] <= 'z' && c[1] >= 'a' && c[1] <= 'z' {
			list = append(list, Country{Code: c, HasSite: slices.Contains(sites, "category-"+c)})
		}
	}
	return list, slices.Contains(sites, routing.RuBlocked) || slices.Contains(ips, routing.RuBlocked)
}
