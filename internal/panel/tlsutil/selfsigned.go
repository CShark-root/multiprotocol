// Package tlsutil provides the panel's fallback self-signed certificate.
// Let's Encrypt IP/domain certificates come later; see PLAN.md 8.1.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureSelfSigned loads certPath/keyPath, first generating an ECDSA P-256
// self-signed pair if either file is missing.
func EnsureSelfSigned(certPath, keyPath string) (tls.Certificate, error) {
	if fileExists(certPath) && fileExists(keyPath) {
		return tls.LoadX509KeyPair(certPath, keyPath)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	ips := localIPs()
	tmpl := x509.Certificate{
		SerialNumber: serial,
		// The server's own IP as CN, like any generic self-signed cert: a name
		// such as "panel" would be a fingerprint, and an empty subject breaks
		// OpenSSL-based clients ("couldn't get X509-issuer name").
		Subject:               pkix.Name{CommonName: commonName(ips)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// SelfSignedFor creates a self-signed ECDSA certificate for a domain name,
// for cores such as Hysteria that need TLS but have no real certificate yet.
// pin is the lowercase hex SHA-256 of the certificate, for client pinning.
func SelfSignedFor(name string) (certPEM, keyPEM, pin string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return "", "", "", err
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{name},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return "", "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", "", err
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		hex.EncodeToString(sum[:]), nil
}

// Fingerprint is the SHA-256 of the leaf certificate, formatted the way
// browsers show it, so the admin can verify a self-signed cert on first visit.
func Fingerprint(cert tls.Certificate) string {
	if len(cert.Certificate) == 0 {
		return ""
	}
	sum := sha256.Sum256(cert.Certificate[0])
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// LocalIPs lists the addresses of all interfaces, used both as certificate
// SANs and to print candidate panel URLs.
func LocalIPs() []net.IP { return localIPs() }

func localIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	var ips []net.IP
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLinkLocalUnicast() {
			ips = append(ips, ipnet.IP)
		}
	}
	return ips
}

func commonName(ips []net.IP) string {
	for _, ip := range ips {
		if !ip.IsLoopback() && ip.To4() != nil {
			return ip.String()
		}
	}
	return "localhost"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
