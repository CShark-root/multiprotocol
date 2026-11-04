// Package backup packs what is needed to bring the panel back on another
// server: the database (users, inbounds with their keys, routing, WARP,
// domain), panel.json (port, secret paths) and the domain certificate.
// Cores, geo databases and the self-signed panel certificate are not
// included: the installer and the panel recreate them.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/panel/store"
)

// Files besides the database, relative to the data dir.
var extra = []string{"panel.json", "tls/acme/cert.pem", "tls/acme/key.pem", "tls/acme/account.key"}

const dbName = "panel.db"

// Write streams a .tar.gz backup of dataDir to w.
func Write(ctx context.Context, st *store.Store, dataDir string, w io.Writer) error {
	tmp, err := os.MkdirTemp(dataDir, "backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	snap := filepath.Join(tmp, dbName)
	if err := st.Snapshot(ctx, snap); err != nil {
		return fmt.Errorf("database snapshot: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(name, path string) error {
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	}
	if err := add(dbName, snap); err != nil {
		return err
	}
	for _, name := range extra {
		if err := add(name, filepath.Join(dataDir, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Name is the suggested file name.
func Name(now time.Time) string {
	return "multiprotocol-backup-" + now.Format("2006-01-02-1504") + ".tar.gz"
}

// Restore unpacks a backup into dataDir. The panel must be stopped. The
// current files are kept next to them with a ".before-restore" suffix.
func Restore(r io.Reader, dataDir string) ([]string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("not a panel backup: %w", err)
	}
	tr := tar.NewReader(gz)
	allowed := map[string]bool{dbName: true}
	for _, e := range extra {
		allowed[e] = true
	}
	var restored []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return restored, err
		}
		// Only known names: a crafted archive cannot write anywhere else.
		if !allowed[h.Name] || h.Typeflag != tar.TypeReg || h.Size > 1<<30 {
			continue
		}
		dst := filepath.Join(dataDir, filepath.FromSlash(h.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return restored, err
		}
		if _, err := os.Stat(dst); err == nil {
			os.Rename(dst, dst+".before-restore")
		}
		if h.Name == dbName {
			// SQLite side files belong to the old database: keep them with it.
			for _, suffix := range []string{"-wal", "-shm"} {
				os.Rename(dst+suffix, dst+suffix+".before-restore")
			}
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return restored, err
		}
		_, err = io.Copy(f, io.LimitReader(tr, h.Size))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return restored, err
		}
		restored = append(restored, h.Name)
	}
	if !contains(restored, dbName) {
		return restored, errors.New("the archive has no database")
	}
	return restored, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
