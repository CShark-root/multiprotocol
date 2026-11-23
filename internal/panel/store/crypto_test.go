package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlaintextMigrationAndEncryptedReads(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.db")
	st, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	admin, err := st.CreateFirstAdmin(ctx, "admin", "hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, UserFields{Name: "user", Enabled: true, AmneziaKey: "vpn://amnezia-secret"},
		"subscription-secret", "user-uuid-secret", now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateInbound(ctx, InboundFields{Remark: "main", Settings: InboundSettings{
		Reality: &RealitySettings{PrivateKey: "reality-private-secret"},
	}}, now); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "telegram", map[string]string{"token": "telegram-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAudit(ctx, AuditEntry{Diff: map[string]string{"key": "audit-secret"}}, now); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	updates := []string{
		`UPDATE users SET sub_token = 'subscription-secret', sub_token_hash = NULL, uuid = 'user-uuid-secret', amnezia_key = 'vpn://amnezia-secret'`,
		`UPDATE admins SET totp_secret = 'totp-secret'`,
		`UPDATE inbounds SET settings_json = '{"reality":{"privateKey":"reality-private-secret"}}'`,
		`UPDATE settings SET value_json = '{"token":"telegram-secret"}' WHERE key = 'telegram'`,
		`UPDATE audit_log SET diff_json = '{"key":"audit-secret"}'`,
	}
	for _, query := range updates {
		if _, err := legacy.Exec(query); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	assertDatabaseContains(t, dir, "subscription-secret")

	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	for _, secret := range []string{
		"subscription-secret", "user-uuid-secret", "vpn://amnezia-secret", "totp-secret",
		"reality-private-secret", "telegram-secret", "audit-secret",
	} {
		assertDatabaseOmits(t, dir, secret)
	}
	info, err := os.Stat(filepath.Join(dir, masterKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("master key mode = %o", info.Mode().Perm())
	}

	u, err := st.GetUserBySubToken(ctx, "subscription-secret")
	if err != nil {
		t.Fatal(err)
	}
	if u.UUID != "user-uuid-secret" || u.AmneziaKey != "vpn://amnezia-secret" {
		t.Fatalf("user secrets were not restored: %+v", u)
	}
	in, err := st.GetInbound(ctx, 1)
	if err != nil || in.Settings.Reality.PrivateKey != "reality-private-secret" {
		t.Fatalf("inbound: %+v, %v", in, err)
	}
	var setting map[string]string
	if ok, err := st.GetSetting(ctx, "telegram", &setting); err != nil || !ok || setting["token"] != "telegram-secret" {
		t.Fatalf("setting: %v, %v, %v", ok, setting, err)
	}
	admin, err = st.AdminByID(ctx, admin.ID)
	if err != nil || admin.TOTPSecret != "totp-secret" {
		t.Fatalf("admin TOTP: %+v, %v", admin, err)
	}
	audit, err := st.ListAudit(ctx, 0, 10)
	if err != nil || len(audit) != 1 || !strings.Contains(string(audit[0].Diff), "audit-secret") {
		t.Fatalf("audit: %+v, %v", audit, err)
	}

	var encryptedToken string
	if err := st.db.QueryRowContext(ctx, `SELECT sub_token FROM users`).Scan(&encryptedToken); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encryptedToken, encryptedPrefix) {
		t.Fatalf("stored token is not encrypted: %q", encryptedToken)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var afterRestart string
	if err := st.db.QueryRowContext(ctx, `SELECT sub_token FROM users`).Scan(&afterRestart); err != nil {
		t.Fatal(err)
	}
	if afterRestart != encryptedToken {
		t.Fatal("an already encrypted database was rewritten on restart")
	}
}

func TestEncryptedDatabaseNeedsItsKey(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	st, err := Open(ctx, filepath.Join(src, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, UserFields{Name: "user", Enabled: true}, "token", "uuid", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := os.ReadFile(filepath.Join(src, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "panel.db"), db, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err = Open(ctx, filepath.Join(dst, "panel.db"))
	if err == nil {
		st.Close()
		t.Fatal("database opened with a different key")
	}
}

func assertDatabaseContains(t *testing.T, dir, value string) {
	t.Helper()
	if !databaseContains(t, dir, value) {
		t.Fatalf("test setup did not store plaintext %q", value)
	}
}

func assertDatabaseOmits(t *testing.T, dir, value string) {
	t.Helper()
	if databaseContains(t, dir, value) {
		t.Fatalf("database still contains plaintext %q", value)
	}
}

func databaseContains(t *testing.T, dir, value string) bool {
	t.Helper()
	for _, name := range []string{"panel.db", "panel.db-wal"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if errorsIsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(value)) {
			return true
		}
	}
	return false
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
