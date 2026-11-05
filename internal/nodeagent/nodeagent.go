// Package nodeagent — агент на другом нашем сервере. Он запускает ядра так
// же, как главная панель у себя, а конфиги берёт у неё: сам приходит по
// HTTPS на её секретный путь с токеном узла. Входящих служебных портов на
// узле нет.
package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/nodeproto"
	"panel/internal/shared/version"
)

// Config — agent.json в каталоге данных узла.
type Config struct {
	URL     string `json:"url"`     // https://host:port/<secret>
	Token   string `json:"token"`   // токен узла
	Pin     string `json:"pin"`     // отпечаток самоподписанного сертификата панели
	XrayAPI string `json:"xrayApi"` // выбирается при первом запуске
}

const configName = "agent.json"

func LoadConfig(dataDir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(dataDir, configName))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func SaveConfig(dataDir string, c Config) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	p := filepath.Join(dataDir, configName)
	if err := os.WriteFile(p+".new", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".new", p)
}

// Join разбирает код из команды установки и сохраняет agent.json.
func Join(dataDir, code string) error {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(code))
	if err != nil {
		return errors.New("код подключения повреждён")
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return errors.New("код подключения повреждён")
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(c.Token) < 40 {
		return errors.New("в коде подключения нет адреса панели или токена")
	}
	if old, err := LoadConfig(dataDir); err == nil {
		c.XrayAPI = old.XrayAPI
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	return SaveConfig(dataDir, c)
}

// normPin: "AA:BB:…" → "aabb…".
func normPin(p string) string { return strings.ToLower(strings.ReplaceAll(p, ":", "")) }

// httpClient проверяет сертификат панели обычным способом (домен с
// Let's Encrypt), а если не вышло — сверяет отпечаток с закреплённым.
func httpClient(c Config) (*http.Client, error) {
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	pin := normPin(c.Pin)
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         host,
		InsecureSkipVerify: true, // проверка ниже, в VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("панель не прислала сертификат")
			}
			leaf := cs.PeerCertificates[0]
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter}); err == nil {
				return nil
			}
			sum := sha256.Sum256(leaf.Raw)
			if pin != "" && hex.EncodeToString(sum[:]) == pin {
				return nil
			}
			return errors.New("сертификат панели не совпадает с закреплённым")
		},
	}
	return &http.Client{
		Timeout:   time.Duration(nodeproto.MaxWait+20) * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsCfg, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second},
	}, nil
}

type Agent struct {
	dataDir string
	cfg     Config
	agent   *agent.Agent
	client  *http.Client
	log     *slog.Logger

	applied  string
	applyErr string
	pending  []agent.TrafficDelta
	files    map[string]string // что панель велела держать (путь → sha256)
}

// New готовит агента. cores — ядра узла, собранные как у панели.
func New(dataDir string, cfg Config, cores *agent.Agent, log *slog.Logger) (*Agent, error) {
	cl, err := httpClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Agent{dataDir: dataDir, cfg: cfg, agent: cores, client: cl, log: log, files: map[string]string{}}, nil
}

// PickXrayAPI выбирает свободный порт для API Xray и запоминает его.
func PickXrayAPI(dataDir string, cfg *Config) error {
	if cfg.XrayAPI != "" {
		return nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	cfg.XrayAPI = l.Addr().String()
	l.Close()
	return SaveConfig(dataDir, *cfg)
}

// Run ходит к панели, пока не отменят ctx.
func (a *Agent) Run(ctx context.Context) {
	fresh := true
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		if d, err := a.agent.Traffic(ctx); err == nil || len(d) > 0 {
			a.pending = append(a.pending, d...)
		}
		online, _ := a.agent.Online(ctx)
		req := nodeproto.SyncRequest{
			AgentVersion: version.Version, Health: a.agent.Health(ctx), Traffic: a.pending, Online: online,
			XrayAPI: a.cfg.XrayAPI, Applied: a.applied, ApplyError: a.applyErr, Files: a.localSums(), Fresh: fresh,
		}
		resp, err := a.sync(ctx, req)
		if err != nil {
			a.log.Warn("sync with panel", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff, fresh = 5*time.Second, false
		a.pending = nil
		a.handle(ctx, resp)
	}
}

func (a *Agent) sync(ctx context.Context, req nodeproto.SyncRequest) (nodeproto.SyncResponse, error) {
	var resp nodeproto.SyncResponse
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.URL+nodeproto.Path+"/sync", bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	r.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	r.Header.Set("Content-Type", "application/json")
	res, err := a.client.Do(r)
	if err != nil {
		return resp, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return resp, errors.New("панель не узнала узел: токен заменён или узел удалён")
	}
	if res.StatusCode != http.StatusOK {
		return resp, fmt.Errorf("панель ответила %s", res.Status)
	}
	return resp, json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(&resp)
}

func (a *Agent) handle(ctx context.Context, resp nodeproto.SyncResponse) {
	// Сначала файлы: конфиг Xray и Hysteria может ссылаться на новые гео-базы.
	filesChanged := false
	if resp.Files != nil {
		a.files = resp.Files
		for rel, sum := range resp.Files {
			if fileSHA(a.abs(rel)) == sum {
				continue
			}
			if err := a.download(ctx, rel, sum); err != nil {
				a.log.Warn("download from panel", "file", rel, "err", err)
				continue
			}
			filesChanged = true
		}
	}
	if resp.Configs != nil {
		err := a.agent.Apply(ctx, agent.NodeState{Configs: resp.Configs})
		a.applied, a.applyErr = resp.Hash, ""
		if err != nil {
			a.applyErr = err.Error()
			a.log.Warn("apply config", "err", err)
		} else {
			a.log.Info("config applied", "hash", resp.Hash)
		}
	} else if filesChanged {
		a.agent.RestartCores() // те же конфиги, новые гео-базы
	}
	for _, c := range resp.Commands {
		if c == nodeproto.CmdRestartCores {
			a.log.Info("restart cores by panel request")
			a.agent.RestartCores()
		}
	}
}

// safeRel пускает только файлы гео-баз внутри каталога данных.
func safeRel(rel string) bool {
	c := path.Clean(rel)
	return c == rel && strings.HasPrefix(c, "geo/") && !strings.Contains(c, "..") && !path.IsAbs(c)
}

func (a *Agent) abs(rel string) string { return filepath.Join(a.dataDir, filepath.FromSlash(rel)) }

func (a *Agent) localSums() map[string]string {
	out := map[string]string{}
	for rel := range a.files {
		if safeRel(rel) {
			if s := fileSHA(a.abs(rel)); s != "" {
				out[rel] = s
			}
		}
	}
	return out
}

func (a *Agent) download(ctx context.Context, rel, want string) error {
	if !safeRel(rel) {
		return errors.New("недопустимый путь")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, a.cfg.URL+nodeproto.Path+"/file?path="+url.QueryEscape(rel), nil)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	cl := *a.client
	cl.Timeout = 10 * time.Minute
	res, err := cl.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("панель ответила %s", res.Status)
	}
	dst := a.abs(rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("контрольная сумма не совпала")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func fileSHA(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
