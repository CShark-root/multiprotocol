// Package domain gets a real TLS certificate for the panel's domain: it
// keeps the DuckDNS record pointing at the server and obtains Let's Encrypt
// certificates over the DNS-01 challenge, which needs no open port, so it
// works behind NAT (a test VM) as well as on a VPS.
package domain

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
)

const (
	LetsEncrypt        = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
	TermsURL           = "https://letsencrypt.org/repository/"
)

var client = &http.Client{Timeout: 30 * time.Second}

var (
	duckNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	duckTokenRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// DuckName accepts "myvpn" or "myvpn.duckdns.org" and returns "myvpn".
func DuckName(s string) (string, bool) {
	s = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "."), ".duckdns.org")
	return s, duckNameRe.MatchString(s)
}

func ValidDuckToken(s string) bool { return duckTokenRe.MatchString(strings.ToLower(s)) }

// duck calls the DuckDNS update API, which answers "OK" or "KO". The token
// travels in the query string because that is the only form the API has.
func duck(ctx context.Context, q url.Values) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.duckdns.org/update?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("duckdns: " + redact(err.Error(), q.Get("token")))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if !strings.HasPrefix(strings.TrimSpace(string(b)), "OK") {
		return errors.New("duckdns refused the update: check the name and the token")
	}
	return nil
}

func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "***")
}

// DuckSetIP points name.duckdns.org at ip; an empty ip lets DuckDNS use the
// address the request comes from (the server's public IP).
func DuckSetIP(ctx context.Context, name, token, ip string) error {
	return duck(ctx, url.Values{"domains": {name}, "token": {token}, "ip": {ip}})
}

func duckTXT(ctx context.Context, name, token, txt string) error {
	q := url.Values{"domains": {name}, "token": {token}, "txt": {txt}}
	if txt == "" {
		q.Set("clear", "true")
	}
	return duck(ctx, q)
}

// Cert is an issued certificate chain with its key, PEM-encoded.
type Cert struct {
	CertPEM  []byte
	KeyPEM   []byte
	Domain   string
	NotAfter time.Time
	Issuer   string
}

// ParseCert reads a PEM pair and checks that it holds together.
func ParseCert(certPEM, keyPEM []byte) (*Cert, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("no certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	c := &Cert{CertPEM: certPEM, KeyPEM: keyPEM, NotAfter: leaf.NotAfter, Issuer: leaf.Issuer.CommonName}
	if len(leaf.DNSNames) > 0 {
		c.Domain = leaf.DNSNames[0]
	}
	if leaf.Issuer.Organization != nil && len(leaf.Issuer.Organization) > 0 {
		c.Issuer = strings.TrimSpace(leaf.Issuer.Organization[0] + " " + leaf.Issuer.CommonName)
	}
	return c, nil
}

// Issue obtains a certificate for name.duckdns.org from Let's Encrypt
// (staging for tests) via DNS-01. The ACME account key lives in dir.
// Calling it means agreeing to the Let's Encrypt subscriber agreement; the
// UI says so next to the button.
func Issue(ctx context.Context, dir string, staging bool, name, token string, progress func(string)) (*Cert, error) {
	domain := name + ".duckdns.org"
	directory, keyFile := LetsEncrypt, "account.key"
	if staging {
		directory, keyFile = LetsEncryptStaging, "account-staging.key"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	accountKey, err := loadOrCreateKey(filepath.Join(dir, keyFile))
	if err != nil {
		return nil, err
	}
	cl := &acme.Client{Key: accountKey, DirectoryURL: directory, UserAgent: "MultiProtocol-panel"}
	progress("account")
	if _, err := cl.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("let's encrypt account: %w", err)
	}
	order, err := cl.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	defer duckTXT(context.Background(), name, token, "")
	for _, zurl := range order.AuthzURLs {
		z, err := cl.GetAuthorization(ctx, zurl)
		if err != nil {
			return nil, err
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range z.Challenges {
			if c.Type == "dns-01" {
				chal = c
			}
		}
		if chal == nil {
			return nil, errors.New("let's encrypt offered no dns-01 challenge")
		}
		value, err := cl.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return nil, err
		}
		progress("dns")
		if err := duckTXT(ctx, name, token, value); err != nil {
			return nil, err
		}
		progress("propagation")
		if err := waitTXT(ctx, "_acme-challenge."+domain, value); err != nil {
			return nil, err
		}
		progress("validation")
		if _, err := cl.Accept(ctx, chal); err != nil {
			return nil, fmt.Errorf("accept challenge: %w", err)
		}
		if _, err := cl.WaitAuthorization(ctx, z.URI); err != nil {
			return nil, fmt.Errorf("let's encrypt could not verify the domain: %w", err)
		}
	}
	if order, err = cl.WaitOrder(ctx, order.URI); err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	progress("certificate")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
	}, key)
	if err != nil {
		return nil, err
	}
	der, _, err := cl.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	var certPEM []byte
	for _, b := range der {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})...)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return ParseCert(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

// waitTXT polls public resolvers until both see the record (DuckDNS
// usually publishes within seconds), then leaves a little slack for the
// resolvers Let's Encrypt itself uses.
func waitTXT(ctx context.Context, name, value string) error {
	deadline := time.Now().Add(3 * time.Minute)
	resolvers := []string{"https://cloudflare-dns.com/dns-query", "https://dns.google/resolve"}
	for time.Now().Before(deadline) {
		seen := 0
		for _, r := range resolvers {
			if ok, _ := hasTXT(ctx, r, name, value); ok {
				seen++
			}
		}
		if seen == len(resolvers) {
			select {
			case <-time.After(10 * time.Second):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.New("the DNS record did not show up within 3 minutes")
}

func hasTXT(ctx context.Context, resolver, name, value string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolver+"?type=TXT&name="+url.QueryEscape(name), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var r struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&r); err != nil {
		return false, err
	}
	for _, a := range r.Answer {
		if strings.Trim(a.Data, `"`) == value {
			return true, nil
		}
	}
	return false, nil
}

func loadOrCreateKey(path string) (crypto.Signer, error) {
	if b, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, errors.New("bad ACME account key")
		}
		return x509.ParseECPrivateKey(block.Bytes)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
