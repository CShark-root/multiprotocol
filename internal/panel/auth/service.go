// Package auth handles admin accounts and browser sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"panel/internal/panel/store"
	"panel/internal/panel/validate"
)

const SessionTTL = 12 * time.Hour

var (
	ErrAlreadySetup       = errors.New("an admin account already exists")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthenticated    = errors.New("not authenticated")
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

type Service struct {
	store *store.Store
	now   func() time.Time

	dummyOnce sync.Once
	dummyHash string
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, now: time.Now}
}

func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.store.CountAdmins(ctx)
	return n == 0, err
}

// Setup creates the first admin. It fails with ErrAlreadySetup afterwards.
func (s *Service) Setup(ctx context.Context, username, password string) (store.Admin, error) {
	if !usernameRe.MatchString(username) {
		return store.Admin{}, validate.Fail("username", "3-32 characters: letters, digits, '_', '.', '-'")
	}
	if n := utf8.RuneCountInString(password); n < 10 || n > 128 {
		return store.Admin{}, validate.Fail("password", "10-128 characters")
	}
	admin, err := s.store.CreateFirstAdmin(ctx, username, HashPassword(password), s.now())
	if errors.Is(err, store.ErrConflict) {
		return store.Admin{}, ErrAlreadySetup
	}
	return admin, err
}

// Login verifies credentials and opens a session. Unknown usernames cost the
// same argon2 work as wrong passwords, so timing does not reveal which
// usernames exist.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (string, store.Admin, error) {
	admin, err := s.store.AdminByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		VerifyPassword(password, s.dummy())
		return "", store.Admin{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", store.Admin{}, err
	}
	ok, err := VerifyPassword(password, admin.PasswordHash)
	if err != nil {
		return "", store.Admin{}, err
	}
	if !ok {
		return "", store.Admin{}, ErrInvalidCredentials
	}
	token, err := s.CreateSession(ctx, admin.ID, ip, userAgent)
	return token, admin, err
}

// CreateSession returns a new opaque token; only its hash is persisted.
func (s *Service) CreateSession(ctx context.Context, adminID int64, ip, userAgent string) (string, error) {
	token := rand.Text() + rand.Text() // 2 x 128 bits
	now := s.now()
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	err := s.store.CreateSession(ctx, store.Session{
		TokenHash: hashToken(token),
		AdminID:   adminID,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
		IP:        ip,
		UserAgent: userAgent,
	})
	return token, err
}

func (s *Service) Authenticate(ctx context.Context, token string) (store.Admin, error) {
	if token == "" || len(token) > 128 {
		return store.Admin{}, ErrUnauthenticated
	}
	sess, err := s.store.SessionByHash(ctx, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.Admin{}, ErrUnauthenticated
	}
	if err != nil {
		return store.Admin{}, err
	}
	if !s.now().Before(sess.ExpiresAt) {
		return store.Admin{}, ErrUnauthenticated
	}
	admin, err := s.store.AdminByID(ctx, sess.AdminID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Admin{}, ErrUnauthenticated
	}
	return admin, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, hashToken(token))
}

func (s *Service) PruneSessions(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, s.now())
}

func (s *Service) dummy() string {
	s.dummyOnce.Do(func() { s.dummyHash = HashPassword(rand.Text()) })
	return s.dummyHash
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
