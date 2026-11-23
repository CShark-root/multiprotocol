package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	masterKeyFile        = "master.key"
	encryptedPrefix      = "mpenc1:"
	encryptionCheckKey   = "encryption_check"
	encryptionCheckValue = "MultiProtocol encrypted store v1"
)

type secretCipher struct {
	key []byte
}

func loadSecretCipher(dbPath string) (*secretCipher, error) {
	path := filepath.Join(filepath.Dir(dbPath), masterKeyFile)
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			key, err = os.ReadFile(path)
		} else if err == nil {
			_, err = f.Write(key)
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return nil, fmt.Errorf("create encryption key: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("read encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s: expected 32 bytes, got %d", path, len(key))
	}
	return &secretCipher{key: key}, nil
}

func (c *secretCipher) encrypt(context, plain string) (string, error) {
	if plain == "" || strings.HasPrefix(plain, encryptedPrefix) {
		return plain, nil
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), []byte(context))
	return encryptedPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

func (c *secretCipher) decrypt(context, stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, encryptedPrefix) {
		return stored, nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted %s: %w", context, err)
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted %s is too short", context)
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(context))
	if err != nil {
		return "", fmt.Errorf("decrypt %s: %w", context, err)
	}
	return string(plain), nil
}

func (c *secretCipher) tokenHash(token string) string {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte("user subscription token\x00" + token))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Store) encryptExisting(ctx context.Context) (bool, error) {
	changed := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var check string
		err := tx.QueryRowContext(ctx, `SELECT value_json FROM settings WHERE key = ?`, encryptionCheckKey).Scan(&check)
		if errors.Is(err, sql.ErrNoRows) {
			check, err = s.cipher.encrypt("settings", encryptionCheckValue)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value_json) VALUES (?, ?)`, encryptionCheckKey, check); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			check, err = s.cipher.decrypt("settings", check)
			if err != nil || check != encryptionCheckValue {
				return errors.New("encryption key does not match this database")
			}
		}
		for _, table := range []struct {
			name, id, value, context string
		}{
			{"settings", "key", "value_json", "settings"},
			{"inbounds", "id", "settings_json", "inbound settings"},
			{"audit_log", "id", "diff_json", "audit diff"},
		} {
			ok, err := encryptRows(ctx, tx, s.cipher, table.name, table.id, table.value, table.context)
			if err != nil {
				return err
			}
			changed = changed || ok
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, sub_token, sub_token_hash, uuid, COALESCE(amnezia_key, '') FROM users`)
		if err != nil {
			return err
		}
		type userSecrets struct {
			id      int64
			token   string
			hash    sql.NullString
			uuid    string
			amnezia string
		}
		var users []userSecrets
		for rows.Next() {
			var u userSecrets
			if err := rows.Scan(&u.id, &u.token, &u.hash, &u.uuid, &u.amnezia); err != nil {
				rows.Close()
				return err
			}
			users = append(users, u)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, u := range users {
			if u.hash.Valid && strings.HasPrefix(u.token, encryptedPrefix) && strings.HasPrefix(u.uuid, encryptedPrefix) &&
				(u.amnezia == "" || strings.HasPrefix(u.amnezia, encryptedPrefix)) {
				continue
			}
			plainToken, err := s.cipher.decrypt("user subscription token", u.token)
			if err != nil {
				return err
			}
			token, err := s.cipher.encrypt("user subscription token", plainToken)
			if err != nil {
				return err
			}
			uuid, err := s.cipher.encrypt("user UUID", u.uuid)
			if err != nil {
				return err
			}
			amnezia, err := s.cipher.encrypt("user Amnezia key", u.amnezia)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE users SET sub_token = ?, sub_token_hash = ?, uuid = ?, amnezia_key = ? WHERE id = ?`,
				token, s.cipher.tokenHash(plainToken), uuid, amnezia, u.id); err != nil {
				return err
			}
			changed = true
		}
		rows, err = tx.QueryContext(ctx, `SELECT id, COALESCE(totp_secret, '') FROM admins`)
		if err != nil {
			return err
		}
		type adminSecret struct {
			id     int64
			secret string
		}
		var admins []adminSecret
		for rows.Next() {
			var a adminSecret
			if err := rows.Scan(&a.id, &a.secret); err != nil {
				rows.Close()
				return err
			}
			admins = append(admins, a)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, a := range admins {
			if a.secret == "" || strings.HasPrefix(a.secret, encryptedPrefix) {
				continue
			}
			secret, err := s.cipher.encrypt("admin TOTP secret", a.secret)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE admins SET totp_secret = ? WHERE id = ?`, secret, a.id); err != nil {
				return err
			}
			changed = true
		}
		return nil
	})
	return changed, err
}

func encryptRows(ctx context.Context, tx *sql.Tx, c *secretCipher, table, idColumn, valueColumn, context string) (bool, error) {
	query := fmt.Sprintf("SELECT %s, %s FROM %s", idColumn, valueColumn, table)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	type row struct {
		id    any
		value string
	}
	var values []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.value); err != nil {
			rows.Close()
			return false, err
		}
		values = append(values, r)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	changed := false
	for _, r := range values {
		if r.value == "" || strings.HasPrefix(r.value, encryptedPrefix) {
			continue
		}
		encrypted, err := c.encrypt(context, r.value)
		if err != nil {
			return false, err
		}
		update := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", table, valueColumn, idColumn)
		if _, err := tx.ExecContext(ctx, update, encrypted, r.id); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
