package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"panel/internal/agent"
	"panel/internal/panel/store"
)

// Автообновление ядер. Панель (без root) раз в сутки в заданный час
// сравнивает версии ядер с последними стабильными релизами на GitHub и,
// если есть новее, кладёт update-request с "cores-only=1". Дальше работает
// root-скрипт update-cores.sh: копия старого бинаря, установка, проверка
// конфига, перезапуск панели и откат, если ядро не поднялось. Итог он пишет
// в cores-update.json, который панель читает при старте.

const (
	EventCoresUpdated  = "cores.updated"
	EventCoresRollback = "cores.rollback"

	coreAutoKey = "cores.auto"
)

// CoreAutoSettings — настройки автообновления ядер.
type CoreAutoSettings struct {
	Enabled   bool      `json:"enabled"`
	Hour      int       `json:"hour"` // час по времени сервера, 0–23
	LastCheck time.Time `json:"lastCheck"`
	LastError string    `json:"lastError,omitempty"`
}

// coreRepos: имя ядра у агента → репозиторий и префикс тега.
var coreRepos = map[string]struct{ repo, prefix string }{
	"xray":     {"XTLS/Xray-core", "v"},
	"hysteria": {"apernet/hysteria", "app/v"},
	"singbox":  {"SagerNet/sing-box", "v"},
}

type CoreUpdater struct {
	store   *store.Store
	events  *Events
	health  func(context.Context) agent.NodeHealth
	dataDir string
	script  string // существует только на сервере, установленном install.sh
	log     *slog.Logger
	now     func() time.Time
	latest  func(ctx context.Context, repo string) (string, error)
}

func NewCoreUpdater(st *store.Store, events *Events, health func(context.Context) agent.NodeHealth, dataDir string, log *slog.Logger) *CoreUpdater {
	return &CoreUpdater{store: st, events: events, health: health, dataDir: dataDir,
		script: "/usr/local/lib/panel/update-cores.sh", log: log, now: time.Now, latest: latestTag}
}

func (c *CoreUpdater) Get(ctx context.Context) CoreAutoSettings {
	s := CoreAutoSettings{Enabled: true, Hour: 4}
	c.store.GetSetting(ctx, coreAutoKey, &s)
	return s
}

func (c *CoreUpdater) Save(ctx context.Context, enabled bool, hour int) (CoreAutoSettings, error) {
	if hour < 0 || hour > 23 {
		return CoreAutoSettings{}, errors.New("hour must be 0-23")
	}
	s := c.Get(ctx)
	s.Enabled, s.Hour = enabled, hour
	return s, c.store.SetSetting(ctx, coreAutoKey, s)
}

// Supported: скрипт обновления ядер установлен (сервер, а не разработка).
func (c *CoreUpdater) Supported() bool {
	_, err := os.Stat(c.script)
	return err == nil
}

// Run сначала сообщает итог прошлого обновления, затем раз в 10 минут
// смотрит, не пора ли проверить версии.
func (c *CoreUpdater) Run(ctx context.Context) {
	c.report(ctx)
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s := c.Get(ctx)
		now := c.now()
		if !s.Enabled || !c.Supported() || now.Hour() != s.Hour || now.Sub(s.LastCheck) < 20*time.Hour {
			continue
		}
		newer, err := c.Check(ctx)
		s.LastCheck, s.LastError = now, ""
		if err != nil {
			s.LastError = err.Error()
			c.log.Warn("core update check", "err", err)
		}
		c.store.SetSetting(ctx, coreAutoKey, s)
		if len(newer) > 0 {
			c.log.Info("core updates found, requesting install", "cores", newer)
			if err := c.request(); err != nil {
				c.log.Warn("request core update", "err", err)
			}
		}
	}
}

// Check возвращает ядра, для которых на GitHub есть версия новее.
func (c *CoreUpdater) Check(ctx context.Context) ([]string, error) {
	var newer []string
	var errs []error
	for _, core := range c.health(ctx).Cores {
		r, ok := coreRepos[core.Name]
		if !ok || !core.Installed || core.Version == "" {
			continue
		}
		tag, err := c.latest(ctx, r.repo)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !strings.HasPrefix(tag, r.prefix) {
			continue
		}
		if versionLess(strings.TrimPrefix(core.Version, "v"), strings.TrimPrefix(tag, r.prefix)) {
			newer = append(newer, core.Name)
		}
	}
	return newer, errors.Join(errs...)
}

func (c *CoreUpdater) request() error {
	path := filepath.Join(c.dataDir, "update-request")
	if _, err := os.Stat(path); err == nil {
		return nil // уже ждёт своей очереди
	}
	if err := os.WriteFile(path+".new", []byte("requested-by=auto\ncores-only=1\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

type coreChange struct {
	Core string `json:"core"`
	From string `json:"from"`
	To   string `json:"to"`
}

// report превращает cores-update.json в события (и уведомления в Telegram).
func (c *CoreUpdater) report(ctx context.Context) {
	path := filepath.Join(c.dataDir, "cores-update.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)
	var r struct {
		Updated    []coreChange `json:"updated"`
		RolledBack []coreChange `json:"rolledBack"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		c.log.Warn("cores-update.json", "err", err)
		return
	}
	for _, u := range r.Updated {
		c.events.Add(ctx, EventCoresUpdated, "info", 0, "", map[string]any{"core": u.Core, "from": u.From, "to": u.To})
	}
	for _, u := range r.RolledBack {
		c.events.Add(ctx, EventCoresRollback, "warning", 0, "", map[string]any{"core": u.Core, "from": u.From, "to": u.To})
	}
}

// latestTag — тег последнего стабильного релиза (GitHub не отдаёт пре-релизы в /latest).
func latestTag(ctx context.Context, repo string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New(repo + ": HTTP " + resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

// versionLess сравнивает "1.14.4" и "1.15.0" по числам. Непонятные версии
// считаются не меньше, чтобы ничего не ставить по ошибке.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	if len(pa) != 3 || len(pb) != 3 {
		return false
	}
	for i := range 3 {
		x, err1 := strconv.Atoi(pa[i])
		y, err2 := strconv.Atoi(pb[i])
		if err1 != nil || err2 != nil {
			return false
		}
		if x != y {
			return x < y
		}
	}
	return false
}
