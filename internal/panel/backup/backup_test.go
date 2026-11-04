package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/panel/store"
)

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(src, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	st.CreateUser(ctx, store.UserFields{Name: "phone", Enabled: true}, "tok", "uuid", time.Now())
	os.WriteFile(filepath.Join(src, "panel.json"), []byte(`{"listen":":1"}`), 0o600)
	var buf bytes.Buffer
	if err := Write(ctx, st, src, &buf); err != nil {
		t.Fatal(err)
	}
	st.Close()

	dst := t.TempDir()
	os.WriteFile(filepath.Join(dst, "panel.json"), []byte("old"), 0o600)
	got, err := Restore(bytes.NewReader(buf.Bytes()), dst)
	if err != nil || len(got) != 2 {
		t.Fatalf("restored %v, %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "panel.json.before-restore")); string(b) != "old" {
		t.Error("the previous file must be kept")
	}
	st2, err := store.Open(ctx, filepath.Join(dst, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if users, _ := st2.ListUsers(ctx); len(users) != 1 || users[0].Name != "phone" {
		t.Fatalf("users after restore: %+v", users)
	}
}

func TestRestoreIgnoresForeignPaths(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"../../evil", "/etc/passwd", "panel.db"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: 1, Typeflag: tar.TypeReg})
		tw.Write([]byte("x"))
	}
	tw.Close()
	gz.Close()
	dst := t.TempDir()
	got, _ := Restore(&buf, dst)
	if len(got) != 1 || got[0] != "panel.db" {
		t.Fatalf("restored %v", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dst)), "evil")); err == nil {
		t.Fatal("wrote outside the data dir")
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	plain := []byte("tar.gz bytes")
	enc, err := Encrypt(plain, "a long passphrase")
	if err != nil || !IsEncrypted(enc) || bytes.Contains(enc, plain) {
		t.Fatalf("encrypt: %v", err)
	}
	if got, err := Decrypt(enc, "a long passphrase"); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	if _, err := Decrypt(enc, "wrong password!"); err != ErrBadPassword {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := Encrypt(plain, "short"); err == nil {
		t.Fatal("accepted a short password")
	}
}
