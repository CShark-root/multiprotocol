package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/panel/domain"
	"panel/internal/panel/store"
	"panel/internal/panel/subscription"
	"panel/internal/panel/tlsutil"
)

func TestDomainCertForInbounds(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	certPEM, keyPEM, _, _ := tlsutil.SelfSignedFor("myvpn.duckdns.org")
	cert, err := domain.ParseCert([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	var current *domain.Cert
	svc := NewInbounds(st, nil, 1)
	svc.portFree = func(string, int) error { return nil }
	svc.UseDomainCerts(func(sni string) *domain.Cert {
		if current != nil && sni == current.Domain {
			return current
		}
		return nil
	}, func() string { return "" })

	// Created before the certificate exists: self-signed, pinned.
	ib, err := svc.Create(ctx, 0, InboundInput{Remark: "hy", Protocol: "hysteria2", Port: 8443, ServerName: "myvpn.duckdns.org", Masquerade: "https://www.bing.com", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if ib.Settings.Hysteria.DomainCert || ib.Settings.Hysteria.PinSHA256 == "" {
		t.Fatalf("before the certificate: %+v", ib.Settings.Hysteria.DomainCert)
	}
	other, _ := svc.Create(ctx, 0, InboundInput{Remark: "any", Protocol: "anytls", Port: 9443, ServerName: "www.bing.com", Enabled: true})

	// The certificate arrives: the matching inbound switches, the other stays.
	current = cert
	if err := svc.RefreshDomainCerts(ctx); err != nil {
		t.Fatal(err)
	}
	ib, _ = st.GetInbound(ctx, ib.ID)
	h := ib.Settings.Hysteria
	if !h.DomainCert || h.CertPEM != certPEM || h.PinSHA256 != "" {
		t.Fatal("hysteria inbound did not switch to the domain certificate")
	}
	u := store.User{ID: 1, UUID: "11111111-1111-4111-8111-111111111111"}
	if link := subscription.Hysteria2Link(ib, u, "myvpn.duckdns.org"); strings.Contains(link, "insecure") || strings.Contains(link, "pinSHA256") {
		t.Fatalf("a trusted certificate needs no insecure/pin: %s", link)
	}
	if o, _ := st.GetInbound(ctx, other.ID); o.Settings.TLS.DomainCert {
		t.Fatal("an inbound with another SNI must keep its own certificate")
	}

	// The domain is removed: back to a self-signed certificate.
	current = nil
	svc.RefreshDomainCerts(ctx)
	ib, _ = st.GetInbound(ctx, ib.ID)
	if ib.Settings.Hysteria.DomainCert || ib.Settings.Hysteria.CertPEM == certPEM || ib.Settings.Hysteria.PinSHA256 == "" {
		t.Fatal("did not fall back to a self-signed certificate")
	}
}

func TestDomainSettingsValidation(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(ctx, filepath.Join(t.TempDir(), "p.db"))
	defer st.Close()
	d := NewDomains(st, NewEvents(st), t.TempDir(), nil)
	for _, in := range []DomainSettings{
		{Name: "bad name", Token: "11111111-1111-1111-1111-111111111111", IPMode: "auto"},
		{Name: "myvpn", Token: "not-a-token", IPMode: "auto"},
		{Name: "myvpn", Token: "11111111-1111-1111-1111-111111111111", IPMode: "custom", IP: "nope"},
	} {
		if _, err := d.Save(ctx, 0, in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	if n, ok := domain.DuckName(" MyVPN.duckdns.org "); !ok || n != "myvpn" {
		t.Errorf("DuckName: %q %v", n, ok)
	}
}
