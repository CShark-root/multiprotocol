package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"

	"golang.org/x/crypto/scrypt"
)

// Encrypted backups (for copies kept outside the server, e.g. in Telegram):
// "MPBK1" | salt(16) | nonce(12) | AES-256-GCM(tar.gz), the key derived
// from the password with scrypt.
var magic = []byte("MPBK1")

func key(password string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
}

func Encrypt(plain []byte, password string) ([]byte, error) {
	if len(password) < 8 {
		return nil, errors.New("the backup password must be at least 8 characters")
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	rand.Read(salt)
	rand.Read(nonce)
	k, err := key(password, salt)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(k)
	gcm, _ := cipher.NewGCM(block)
	out := append(append(append(append([]byte{}, magic...), salt...), nonce...), gcm.Seal(nil, nonce, plain, magic)...)
	return out, nil
}

// IsEncrypted reports whether data is an encrypted backup.
func IsEncrypted(data []byte) bool { return bytes.HasPrefix(data, magic) }

var ErrBadPassword = errors.New("wrong backup password, or the file is damaged")

func Decrypt(data []byte, password string) ([]byte, error) {
	if !IsEncrypted(data) || len(data) < len(magic)+16+12+16 {
		return nil, errors.New("not an encrypted panel backup")
	}
	salt := data[len(magic) : len(magic)+16]
	nonce := data[len(magic)+16 : len(magic)+28]
	k, err := key(password, salt)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(k)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, data[len(magic)+28:], magic)
	if err != nil {
		return nil, ErrBadPassword
	}
	return plain, nil
}
