package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/panel/validate"
)

// Выбор версий ядер, как в 3x-ui: список последних стабильных релизов с
// GitHub, рекомендуемая версия и закрепление выбранной.

// CoreVersion — одна версия ядра в списке.
type CoreVersion struct {
	Version     string    `json:"version"` // без префикса: "26.10.31", "2.13.0"
	Published   time.Time `json:"published"`
	Recommended bool      `json:"recommended,omitempty"`
	Note        string    `json:"note,omitempty"` // ключ подсказки для интерфейса
}

// CoreVersions — ядро с его версиями для вкладки «Обслуживание».
type CoreVersions struct {
	Core     string        `json:"core"` // имя у агента: xray, hysteria, singbox
	Current  string        `json:"current"`
	Pinned   string        `json:"pinned,omitempty"`
	Latest   string        `json:"latest"`
	Versions []CoreVersion `json:"versions"`
	Error    string        `json:"error,omitempty"`
}

// recommended — версии, которые панель советует вместо последней (пусто —
// советуем последнюю стабильную). Меняется вместе с релизами панели, когда
// у свежей версии ядра находится известная проблема.
var recommended = map[string]string{}

// versionRe — версия N.N.N (без хвостов вроде -beta): такие понимает root-скрипт.
var versionRe = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)

// scriptName — имя ядра для root-скрипта (каталог в /usr/local/lib/panel).
func scriptName(core string) string {
	if core == "singbox" {
		return "sing-box"
	}
	return core
}

// coreNote — подсказка к версии Xray. 26.9.8+ проверены (Xray- и Mihomo-клиенты
// работают, sing-box по Reality нет); на 26.3.27 с настройками панели на VM
// не подключился никто — более старые помечаем как непроверенные.
func coreNote(core, v string) string {
	if core != "xray" {
		return ""
	}
	if versionLess(v, "26.9.8") {
		return "realityOld"
	}
	return "realityMlkem"
}

var releasesCache struct {
	sync.Mutex
	at   map[string]time.Time
	list map[string][]CoreVersion
}

// releases — последние стабильные релизы репозитория (кэш на час).
func releases(ctx context.Context, repo, prefix string) ([]CoreVersion, error) {
	c := &releasesCache
	c.Lock()
	defer c.Unlock()
	if c.at == nil {
		c.at, c.list = map[string]time.Time{}, map[string][]CoreVersion{}
	}
	if time.Since(c.at[repo]) < time.Hour && c.list[repo] != nil {
		return c.list[repo], nil
	}
	// У sing-box на странице десятки альф с сотней файлов каждая: ответ
	// весит больше 10 МБ. Читаем его потоком и листаем, пока не наберём
	// стабильные версии.
	var out []CoreVersion
	for page := 1; page <= 3 && len(out) < 10; page++ {
		more, err := releasesPage(ctx, repo, prefix, page, &out)
		if err != nil {
			if len(out) > 0 {
				break // что успели прочитать — уже годится
			}
			return nil, err
		}
		if !more {
			break
		}
	}
	c.at[repo], c.list[repo] = time.Now(), out
	return out, nil
}

// releasesPage дописывает в out стабильные версии с одной страницы релизов;
// more — есть ли смысл просить следующую.
func releasesPage(ctx context.Context, repo, prefix string, page int, out *[]CoreVersion) (more bool, err error) {
	url := "https://api.github.com/repos/" + repo + "/releases?per_page=30&page=" + strconv.Itoa(page)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, errors.New(repo + ": HTTP " + resp.Status)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<20))
	if _, err := dec.Token(); err != nil { // открывающая скобка массива
		return false, err
	}
	n := 0
	for dec.More() {
		var r struct {
			Tag        string    `json:"tag_name"`
			Prerelease bool      `json:"prerelease"`
			Draft      bool      `json:"draft"`
			Published  time.Time `json:"published_at"`
		}
		if err := dec.Decode(&r); err != nil {
			return false, err
		}
		n++
		v, ok := strings.CutPrefix(r.Tag, prefix)
		if r.Prerelease || r.Draft || !ok || !versionRe.MatchString(v) {
			continue
		}
		*out = append(*out, CoreVersion{Version: v, Published: r.Published})
		if len(*out) == 10 {
			return false, nil
		}
	}
	return n == 30, nil
}

// Versions — ядра этого сервера с доступными версиями.
func (c *CoreUpdater) Versions(ctx context.Context) []CoreVersions {
	pins := c.Get(ctx).Pins
	var out []CoreVersions
	for _, core := range c.health(ctx).Cores {
		r, ok := coreRepos[core.Name]
		if !ok || !core.Installed {
			continue
		}
		cv := CoreVersions{Core: core.Name, Current: strings.TrimPrefix(core.Version, "v"), Pinned: pins[core.Name], Versions: []CoreVersion{}}
		list, err := releases(ctx, r.repo, r.prefix)
		if err != nil {
			cv.Error = err.Error()
		}
		rec := recommended[core.Name]
		if rec == "" && len(list) > 0 {
			rec = list[0].Version
		}
		if len(list) > 0 {
			cv.Latest = list[0].Version
		}
		for _, v := range list {
			v.Recommended = v.Version == rec
			v.Note = coreNote(core.Name, v.Version)
			cv.Versions = append(cv.Versions, v)
		}
		out = append(out, cv)
	}
	return out
}

// ErrUpdateBusy — уже идёт обновление панели или ядер.
var ErrUpdateBusy = errors.New("an update is already running")

// Install ставит выбранную версию ядра. Версия не последняя — ядро
// закрепляется, и ночное автообновление его не трогает; "latest" снимает
// закрепление.
func (c *CoreUpdater) Install(ctx context.Context, core, version string) error {
	if !c.Supported() {
		return validate.Fail("core", "the updater is not installed here")
	}
	if _, ok := coreRepos[core]; !ok {
		return validate.Fail("core", "unknown core")
	}
	if _, err := os.Stat(filepath.Join(c.dataDir, "update-request")); err == nil {
		return ErrUpdateBusy
	}
	var list []CoreVersions
	if version != "latest" {
		list = c.Versions(ctx)
		i := slices.IndexFunc(list, func(v CoreVersions) bool { return v.Core == core })
		if i < 0 || !slices.ContainsFunc(list[i].Versions, func(v CoreVersion) bool { return v.Version == version }) {
			return validate.Fail("version", "pick a version from the list")
		}
		if list[i].Latest == version {
			version = "latest" // последняя — значит без закрепления
		}
	}
	s := c.Get(ctx)
	if s.Pins == nil {
		s.Pins = map[string]string{}
	}
	if version == "latest" {
		delete(s.Pins, core)
	} else {
		s.Pins[core] = version
	}
	if err := c.store.SetSetting(ctx, coreAutoKey, s); err != nil {
		return err
	}
	return c.request([]string{scriptName(core) + "@" + version})
}
