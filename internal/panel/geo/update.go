package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Files are the two databases, by the names Xray and Hysteria expect.
var Files = []string{"geoip.dat", "geosite.dat"}

// runetfreedom's geosite.dat is ~86 MB.
const maxSize = 256 << 20

var client = &http.Client{Timeout: 3 * time.Minute}

// Update downloads both databases into dir. Each file is checked against
// the publisher's .sha256sum when one exists and must parse; the old file
// is kept as <name>.prev. Either both files are replaced or neither is.
// It reports whether anything changed.
func Update(ctx context.Context, dir, geoipURL, geositeURL string) (changed bool, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	urls := map[string]string{"geoip.dat": geoipURL, "geosite.dat": geositeURL}
	staged := map[string]string{}
	defer func() {
		for _, p := range staged {
			os.Remove(p)
		}
	}()
	for _, name := range Files {
		tmp := filepath.Join(dir, name+".new")
		sum, err := download(ctx, urls[name], tmp)
		if err != nil {
			return false, fmt.Errorf("%s: %w", name, err)
		}
		staged[name] = tmp
		if want, err := publishedSum(ctx, urls[name]+".sha256sum"); err != nil {
			return false, fmt.Errorf("%s checksum: %w", name, err)
		} else if want != "" && want != sum {
			return false, fmt.Errorf("%s: checksum mismatch", name)
		}
		if codes, err := Codes(tmp); err != nil || len(codes) == 0 {
			return false, fmt.Errorf("%s: not a geo database", name)
		}
		if old, err := fileSum(filepath.Join(dir, name)); err == nil && old != sum {
			changed = true
		} else if err != nil {
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	for _, name := range Files {
		live := filepath.Join(dir, name)
		if _, err := os.Stat(live); err == nil {
			if err := os.Rename(live, live+".prev"); err != nil {
				return false, err
			}
		}
		if err := os.Rename(staged[name], live); err != nil {
			return false, err
		}
		delete(staged, name)
	}
	return true, nil
}

func download(ctx context.Context, url, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if n > maxSize {
		return "", errors.New("file too large")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// publishedSum fetches "<hex>  name"; a missing file (custom sources) is
// not an error.
func publishedSum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 || len(f[0]) != 64 {
		return "", errors.New("unreadable checksum file")
	}
	return strings.ToLower(f[0]), nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Seed copies the databases shipped with Xray into dir when dir has none,
// so rules work before the first download.
func Seed(dir, from string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range Files {
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst+".new", b, 0o644); err != nil {
			return err
		}
		if err := os.Rename(dst+".new", dst); err != nil {
			return err
		}
	}
	return nil
}

// Info describes the installed databases.
type Info struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func Installed(dir string) []Info {
	var out []Info
	for _, name := range Files {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil {
			out = append(out, Info{Name: name, Size: st.Size(), Modified: st.ModTime().UTC()})
		}
	}
	return out
}
