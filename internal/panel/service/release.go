package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"panel/internal/shared/version"
)

// UpdaterScript — установщик обновлений. Панель (без root) только кладёт
// update-request в каталог данных; panel-update.path замечает файл, и
// panel-update.service запускает проверенный установщик от root.
const UpdaterScript = "/usr/local/lib/panel/panel-update.sh"

type Release struct {
	Tag       string    `json:"tag"`
	URL       string    `json:"url"`
	Published time.Time `json:"published"`
}

var latestCache struct {
	sync.Mutex
	at   time.Time
	info *Release
	err  string
}

// LatestRelease спрашивает GitHub не чаще раза в час (force — сейчас).
func LatestRelease(ctx context.Context, force bool) (*Release, string) {
	c := &latestCache
	c.Lock()
	defer c.Unlock()
	if !force && time.Since(c.at) < time.Hour {
		return c.info, c.err
	}
	c.at = time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+version.Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		c.info, c.err = nil, err.Error()
		return c.info, c.err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.info, c.err = nil, "GitHub: HTTP "+resp.Status
		if resp.StatusCode == http.StatusNotFound {
			c.err = "no releases yet"
		}
		return c.info, c.err
	}
	var r struct {
		Tag       string    `json:"tag_name"`
		URL       string    `json:"html_url"`
		Published time.Time `json:"published_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		c.info, c.err = nil, err.Error()
		return c.info, c.err
	}
	c.info, c.err = &Release{Tag: r.Tag, URL: r.URL, Published: r.Published}, ""
	return c.info, c.err
}

// UpdaterInstalled — есть ли на сервере установщик обновлений.
func UpdaterInstalled() bool {
	_, err := os.Stat(UpdaterScript)
	return runtime.GOOS == "linux" && err == nil
}

// UpdateRequestPath — файл-запрос обновления.
func UpdateRequestPath(dataDir string) string { return filepath.Join(dataDir, "update-request") }

// RequestUpdate просит установщик поставить последний релиз (cores — и ядра).
func RequestUpdate(dataDir string, cores bool) error {
	if !UpdaterInstalled() {
		return errors.New("the updater is not installed here; run install.sh once")
	}
	body := "requested-by=panel\n"
	if cores {
		body += "cores=1\n"
	}
	path := UpdateRequestPath(dataDir)
	if err := os.WriteFile(path+".new", []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Rename(path+".new", path); err != nil {
		return errors.Join(err, os.Remove(path+".new"))
	}
	return nil
}
