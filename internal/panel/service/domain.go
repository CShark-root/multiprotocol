package service

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"panel/internal/panel/domain"
	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

const (
	domainKey       = "domain"
	domainStatusKey = "domain_status"
	renewBefore     = 30 * 24 * time.Hour

	EventCertIssued = "cert.issued"
	EventCertFailed = "cert.failed"
)

// DomainSettings is the DuckDNS name and how its A record is kept.
type DomainSettings struct {
	Name    string `json:"name"`    // "myvpn" for myvpn.duckdns.org
	Token   string `json:"token"`   // never sent back to the UI
	IPMode  string `json:"ipMode"`  // "auto" (the address DuckDNS sees) or "custom"
	IP      string `json:"ip"`      // for "custom", e.g. a LAN address on a test VM
	Staging bool   `json:"staging"` // Let's Encrypt staging: untrusted test certificates
}

func (d DomainSettings) Domain() string {
	if d.Name == "" {
		return ""
	}
	return d.Name + ".duckdns.org"
}

// DomainStatus is what happened last.
type DomainStatus struct {
	IPUpdatedAt time.Time `json:"ipUpdatedAt"`
	IPError     string    `json:"ipError"`
	LastAttempt time.Time `json:"lastAttempt"`
	LastError   string    `json:"lastError"`
	Progress    string    `json:"progress"` // while issuing: account, dns, propagation, validation, certificate
}

// Domains owns the domain settings and the certificate. The panel's TLS
// listener asks it for the certificate on every handshake, so a renewal
// takes effect without a restart.
type Domains struct {
	store  *store.Store
	events *Events
	dir    string // <data>/tls/acme
	// onCert is called after a new certificate is stored (to refresh the
	// inbounds that use it).
	onCert func(ctx context.Context, c *domain.Cert)
	log    *slog.Logger
	now    func() time.Time

	mu       sync.Mutex // one issuance at a time
	cert     atomic.Pointer[domain.Cert]
	tlsCert  atomic.Pointer[tls.Certificate]
	progress atomic.Value // string
}

func NewDomains(st *store.Store, events *Events, dataDir string, log *slog.Logger) *Domains {
	d := &Domains{store: st, events: events, dir: filepath.Join(dataDir, "tls", "acme"), log: log, now: time.Now}
	d.progress.Store("")
	d.loadFromDisk()
	return d
}

// OnCert registers the hook run after each new certificate.
func (d *Domains) OnCert(fn func(ctx context.Context, c *domain.Cert)) { d.onCert = fn }

func (d *Domains) loadFromDisk() {
	certPEM, err1 := os.ReadFile(filepath.Join(d.dir, "cert.pem"))
	keyPEM, err2 := os.ReadFile(filepath.Join(d.dir, "key.pem"))
	if err1 != nil || err2 != nil {
		return
	}
	if c, err := domain.ParseCert(certPEM, keyPEM); err == nil {
		d.setCert(c)
	}
}

func (d *Domains) setCert(c *domain.Cert) {
	pair, err := tls.X509KeyPair(c.CertPEM, c.KeyPEM)
	if err != nil {
		d.log.Warn("domain certificate unusable", "err", err)
		return
	}
	d.cert.Store(c)
	d.tlsCert.Store(&pair)
}

// Cert returns the current certificate, or nil.
func (d *Domains) Cert() *domain.Cert { return d.cert.Load() }

// CertFor returns the certificate when it covers sni, for inbounds whose
// server name is the panel's domain.
func (d *Domains) CertFor(sni string) *domain.Cert {
	c := d.cert.Load()
	if c == nil || !strings.EqualFold(c.Domain, sni) || d.now().After(c.NotAfter) {
		return nil
	}
	return c
}

// GetCertificate picks the domain certificate when the client asked for the
// domain, and the fallback (self-signed) one otherwise, e.g. for an IP.
func (d *Domains) GetCertificate(fallback tls.Certificate) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if c := d.cert.Load(); c != nil && h.ServerName != "" && strings.EqualFold(h.ServerName, c.Domain) {
			return d.tlsCert.Load(), nil
		}
		return &fallback, nil
	}
}

func (d *Domains) Get(ctx context.Context) (DomainSettings, error) {
	s := DomainSettings{IPMode: "auto"}
	_, err := d.store.GetSetting(ctx, domainKey, &s)
	return s, err
}

func (d *Domains) Status(ctx context.Context) DomainStatus {
	var st DomainStatus
	d.store.GetSetting(ctx, domainStatusKey, &st)
	st.Progress, _ = d.progress.Load().(string)
	return st
}

// Save stores the settings. An empty token keeps the saved one; changing the
// name or the IP updates the DNS record right away.
func (d *Domains) Save(ctx context.Context, adminID int64, in DomainSettings) (DomainSettings, error) {
	cur, err := d.Get(ctx)
	if err != nil {
		return cur, err
	}
	name, ok := domain.DuckName(in.Name)
	if !ok {
		return cur, validate.Fail("name", "letters, digits and dashes, as on duckdns.org")
	}
	in.Name = name
	in.Token = strings.ToLower(strings.TrimSpace(in.Token))
	if in.Token == "" {
		in.Token = cur.Token
	}
	if !domain.ValidDuckToken(in.Token) {
		return cur, validate.Fail("token", "the token from duckdns.org looks like xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx")
	}
	switch in.IPMode {
	case "auto":
		in.IP = ""
	case "custom":
		if ip := net.ParseIP(strings.TrimSpace(in.IP)); ip == nil || ip.To4() == nil {
			return cur, validate.Fail("ip", "an IPv4 address")
		}
		in.IP = strings.TrimSpace(in.IP)
	default:
		return cur, validate.Fail("ipMode", "auto or custom")
	}
	if err := d.store.SetSetting(ctx, domainKey, in); err != nil {
		return cur, err
	}
	writeAudit(ctx, d.store, d.now(), adminID, "domain.update", "domain", 0, map[string]any{"name": in.Domain(), "ipMode": in.IPMode, "staging": in.Staging})
	d.updateIP(ctx, in)
	return in, nil
}

// Delete forgets the domain and its certificate.
func (d *Domains) Delete(ctx context.Context, adminID int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.store.SetSetting(ctx, domainKey, DomainSettings{IPMode: "auto"}); err != nil {
		return err
	}
	d.store.SetSetting(ctx, domainStatusKey, DomainStatus{})
	os.Remove(filepath.Join(d.dir, "cert.pem"))
	os.Remove(filepath.Join(d.dir, "key.pem"))
	d.cert.Store(nil)
	d.tlsCert.Store(nil)
	writeAudit(ctx, d.store, d.now(), adminID, "domain.delete", "domain", 0, nil)
	if d.onCert != nil {
		d.onCert(ctx, nil)
	}
	return nil
}

func (d *Domains) updateIP(ctx context.Context, s DomainSettings) error {
	st := d.Status(ctx)
	err := domain.DuckSetIP(ctx, s.Name, s.Token, s.IP)
	if err != nil {
		st.IPError = err.Error()
	} else {
		st.IPError, st.IPUpdatedAt = "", d.now().UTC()
	}
	st.Progress = ""
	d.store.SetSetting(ctx, domainStatusKey, st)
	return err
}

var ErrNoDomain = errors.New("set up the domain first")

// Issue gets a new certificate now. It takes a minute or two (DNS has to
// propagate), so callers run it with a generous context.
func (d *Domains) Issue(ctx context.Context, adminID int64) (*domain.Cert, error) {
	if !d.mu.TryLock() {
		return nil, errors.New("a certificate is already being issued")
	}
	defer d.mu.Unlock()
	s, err := d.Get(ctx)
	if err != nil {
		return nil, err
	}
	if s.Name == "" || s.Token == "" {
		return nil, ErrNoDomain
	}
	defer d.progress.Store("")
	st := d.Status(ctx)
	st.LastAttempt = d.now().UTC()
	c, err := domain.Issue(ctx, d.dir, s.Staging, s.Name, s.Token, func(p string) { d.progress.Store(p) })
	if err != nil {
		st.LastError, st.Progress = err.Error(), ""
		d.store.SetSetting(ctx, domainStatusKey, st)
		d.events.Add(ctx, EventCertFailed, "warning", 0, "", map[string]any{"domain": s.Domain(), "error": err.Error()})
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(d.dir, "key.pem"), c.KeyPEM, 0o600); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(d.dir, "cert.pem"), c.CertPEM, 0o600); err != nil {
		return nil, err
	}
	d.setCert(c)
	st.LastError, st.Progress = "", ""
	d.store.SetSetting(ctx, domainStatusKey, st)
	if adminID != 0 {
		writeAudit(ctx, d.store, d.now(), adminID, "cert.issue", "domain", 0, map[string]any{"domain": c.Domain, "staging": s.Staging})
	}
	d.events.Add(ctx, EventCertIssued, "info", 0, "", map[string]any{"domain": c.Domain, "until": c.NotAfter.Format(time.DateOnly)})
	if d.onCert != nil {
		d.onCert(ctx, c)
	}
	return c, nil
}

// Run keeps the DNS record current and renews the certificate 30 days
// before it expires; checked shortly after start and then every 12 hours.
func (d *Domains) Run(ctx context.Context) {
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		t.Reset(12 * time.Hour)
		s, err := d.Get(ctx)
		if err != nil || s.Name == "" || s.Token == "" {
			continue
		}
		if err := d.updateIP(ctx, s); err != nil {
			d.log.Warn("duckdns update", "err", err)
		}
		c := d.cert.Load()
		if c != nil && c.Domain == s.Domain() && c.NotAfter.Sub(d.now()) > renewBefore {
			continue
		}
		if c == nil && d.Status(ctx).LastAttempt.IsZero() {
			continue // never issued by the admin: do not start on our own
		}
		ictx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		if _, err := d.Issue(ictx, 0); err != nil {
			d.log.Warn("certificate renewal", "err", err)
		}
		cancel()
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
