// Package config holds the panel's on-disk settings (panel.json in the data
// directory). The file is created on first start with a random port and a
// random secret path, so a fresh install is never reachable at a guessable URL.
package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

const FileName = "panel.json"

// DecoyMode is how the panel answers requests outside its secret path.
type DecoyMode string

const (
	// DecoyNotFound replies with an empty 404, like a bare web server.
	DecoyNotFound DecoyMode = "404"
	// DecoyDrop closes the connection without writing a response.
	DecoyDrop DecoyMode = "drop"
)

type Config struct {
	Listen   string    `json:"listen"`
	BasePath string    `json:"basePath"`
	SubPath  string    `json:"subPath"`           // secret prefix of subscription links, different from basePath
	TLSCert  string    `json:"tlsCert,omitempty"` // empty: self-signed pair in <data>/tls
	TLSKey   string    `json:"tlsKey,omitempty"`
	Decoy    DecoyMode `json:"decoy"`
	// PublicHost is the address clients connect to (IP or domain), used in
	// share links. Detected on first start; edit it if the server sits
	// behind NAT.
	PublicHost string `json:"publicHost"`
	// LANSubPort serves subscriptions over plain HTTP, and only while
	// PublicHost is a private LAN address (a test VM): phone apps reject the
	// self-signed certificate, and inside a home network there is no way to
	// get a real one. Never used on a public address.
	LANSubPort int `json:"lanSubPort,omitempty"`
}

// LANOnly reports whether clients reach the server by a private address,
// i.e. it is a test machine inside a home network.
func (c Config) LANOnly() bool {
	ip := net.ParseIP(c.PublicHost)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

var (
	basePathRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	hostnameRe = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

// ValidHost reports whether h is an IP address or a domain name.
func ValidHost(h string) bool { return net.ParseIP(h) != nil || hostnameRe.MatchString(h) }

func ValidBasePath(p string) bool { return basePathRe.MatchString(p) }

func (c Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if !ValidBasePath(c.BasePath) {
		return errors.New("basePath: 8-64 characters of A-Z, a-z, 0-9, '_' or '-'")
	}
	if !ValidBasePath(c.SubPath) || c.SubPath == c.BasePath {
		return errors.New("subPath: 8-64 characters of A-Z, a-z, 0-9, '_' or '-', different from basePath")
	}
	if !ValidHost(c.PublicHost) {
		return errors.New("publicHost: an IP address or a domain name")
	}
	switch c.Decoy {
	case DecoyNotFound, DecoyDrop:
	default:
		return fmt.Errorf("decoy: unknown mode %q", c.Decoy)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tlsCert and tlsKey must be set together")
	}
	return nil
}

// Load reads <dataDir>/panel.json, filling in and saving fields added by
// newer versions.
func Load(dataDir string) (Config, error) {
	path := filepath.Join(dataDir, FileName)
	var cfg Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.fillDefaults() {
		if err := Save(dataDir, cfg); err != nil {
			return cfg, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) fillDefaults() (changed bool) {
	if c.SubPath == "" {
		c.SubPath, changed = RandomBasePath(), true
	}
	if c.PublicHost == "" {
		c.PublicHost, changed = DetectPublicHost(), true
	}
	if c.LANOnly() && c.LANSubPort == 0 {
		if port, err := randomFreePort(20000, 60000); err == nil {
			c.LANSubPort, changed = port, true
		}
	}
	return changed
}

// DetectPublicHost picks the first public IPv4 of this machine, falling back
// to any non-loopback address (a VM behind NAT gets its LAN address).
func DetectPublicHost() string {
	addrs, _ := net.InterfaceAddrs()
	var fallback string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.IsLinkLocalUnicast() || ipnet.IP.To4() == nil {
			continue
		}
		if !ipnet.IP.IsPrivate() {
			return ipnet.IP.String()
		}
		if fallback == "" {
			fallback = ipnet.IP.String()
		}
	}
	if fallback == "" {
		fallback = "127.0.0.1"
	}
	return fallback
}

// LoadOrCreate reads <dataDir>/panel.json, creating it with fresh random
// values if it does not exist yet.
func LoadOrCreate(dataDir string) (cfg Config, created bool, err error) {
	cfg, err = Load(dataDir)
	if !errors.Is(err, fs.ErrNotExist) {
		return cfg, false, err
	}
	port, err := randomFreePort(20000, 60000)
	if err != nil {
		return cfg, false, err
	}
	cfg = Config{
		Listen:   net.JoinHostPort("0.0.0.0", strconv.Itoa(port)),
		BasePath: RandomBasePath(),
		Decoy:    DecoyNotFound,
	}
	cfg.fillDefaults()
	if err := Save(dataDir, cfg); err != nil {
		return cfg, false, err
	}
	return cfg, true, nil
}

// Save writes the config atomically with owner-only permissions.
func Save(dataDir string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

const basePathAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// RandomBasePath returns 20 characters from a 62-symbol alphabet (~119 bits),
// far beyond what any scanner can enumerate.
func RandomBasePath() string {
	b := make([]byte, 20)
	for i := range b {
		b[i] = basePathAlphabet[randIntn(len(basePathAlphabet))]
	}
	return string(b)
}

func randomFreePort(lo, hi int) (int, error) {
	for range 50 {
		port := lo + randIntn(hi-lo)
		l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		l.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free TCP port found in %d-%d", lo, hi)
}

func randIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return int(v.Int64())
}
