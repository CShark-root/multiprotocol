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
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrInvalidTOTP        = errors.New("invalid two-factor code")
	ErrWrongPassword      = errors.New("the current password is wrong")
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

type Service struct {
	store *store.Store
	now   func() time.Time

	dummyOnce sync.Once
	dummyHash string

	// Last TOTP step accepted per admin: a code works only once.
	totpMu   sync.Mutex
	lastStep map[int64]int64
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, now: time.Now, lastStep: map[int64]int64{}}
}

func validPassword(p string) error {
	if n := utf8.RuneCountInString(p); n < 10 || n > 128 {
		return validate.Fail("password", "10-128 characters")
	}
	return nil
}

// checkTOTP verifies a code and refuses to accept the same step twice.
func (s *Service) checkTOTP(adminID int64, secret, code string) bool {
	step, ok := verifyTOTP(secret, code, s.now())
	if !ok {
		return false
	}
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	if step <= s.lastStep[adminID] {
		return false
	}
	s.lastStep[adminID] = step
	return true
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
	if err := validPassword(password); err != nil {
		return store.Admin{}, err
	}
	admin, err := s.store.CreateFirstAdmin(ctx, username, HashPassword(password), s.now())
	if errors.Is(err, store.ErrConflict) {
		return store.Admin{}, ErrAlreadySetup
	}
	return admin, err
}

// Login verifies credentials (and the TOTP code when two-factor
// authentication is on) and opens a session. Unknown usernames cost the same
// argon2 work as wrong passwords, so timing does not reveal which usernames
// exist. ErrTOTPRequired is returned only after the password matched.
func (s *Service) Login(ctx context.Context, username, password, code, ip, userAgent string) (string, store.Admin, error) {
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
	if admin.TOTPSecret != "" {
		if code == "" {
			return "", store.Admin{}, ErrTOTPRequired
		}
		if !s.checkTOTP(admin.ID, admin.TOTPSecret, code) {
			return "", store.Admin{}, ErrInvalidTOTP
		}
	}
	token, err := s.CreateSession(ctx, admin.ID, ip, userAgent)
	return token, admin, err
}

// ChangePassword checks the current password, sets the new one and signs
// out every other session.
func (s *Service) ChangePassword(ctx context.Context, admin store.Admin, current, next, keepToken string) error {
	if ok, err := VerifyPassword(current, admin.PasswordHash); err != nil || !ok {
		return ErrWrongPassword
	}
	if err := validPassword(next); err != nil {
		return err
	}
	if err := s.store.SetAdminPassword(ctx, admin.ID, HashPassword(next)); err != nil {
		return err
	}
	_, err := s.store.DeleteOtherSessions(ctx, admin.ID, hashToken(keepToken))
	return err
}

// ConfirmPassword is for dangerous actions that need a fresh proof.
func (s *Service) ConfirmPassword(admin store.Admin, password string) error {
	if ok, err := VerifyPassword(password, admin.PasswordHash); err != nil || !ok {
		return ErrWrongPassword
	}
	return nil
}

// EnableTOTP stores a secret the admin's app produced a valid code for.
func (s *Service) EnableTOTP(ctx context.Context, admin store.Admin, secret, code string) error {
	if _, err := b32.DecodeString(secret); err != nil || len(secret) < 16 {
		return validate.Fail("secret", "invalid secret")
	}
	if !s.checkTOTP(admin.ID, secret, code) {
		return ErrInvalidTOTP
	}
	return s.store.SetAdminTOTP(ctx, admin.ID, secret)
}

// DisableTOTP needs both the password and a current code.
func (s *Service) DisableTOTP(ctx context.Context, admin store.Admin, password, code string) error {
	if err := s.ConfirmPassword(admin, password); err != nil {
		return err
	}
	if admin.TOTPSecret == "" {
		return nil
	}
	if !s.checkTOTP(admin.ID, admin.TOTPSecret, code) {
		return ErrInvalidTOTP
	}
	return s.store.SetAdminTOTP(ctx, admin.ID, "")
}

// SessionInfo is a session as shown to its owner. ID is the stored hash of
// the token, never the token itself.
type SessionInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Current   bool      `json:"current"`
}

func (s *Service) Sessions(ctx context.Context, adminID int64, currentToken string) ([]SessionInfo, error) {
	list, err := s.store.ListSessions(ctx, adminID, s.now())
	if err != nil {
		return nil, err
	}
	cur := hashToken(currentToken)
	out := make([]SessionInfo, 0, len(list))
	for _, x := range list {
		out = append(out, SessionInfo{ID: x.TokenHash, CreatedAt: x.CreatedAt, ExpiresAt: x.ExpiresAt, IP: x.IP, UserAgent: x.UserAgent, Current: x.TokenHash == cur})
	}
	return out, nil
}

func (s *Service) RevokeSession(ctx context.Context, adminID int64, id string) error {
	return s.store.DeleteSessionOf(ctx, adminID, id)
}

func (s *Service) RevokeOtherSessions(ctx context.Context, adminID int64, currentToken string) (int64, error) {
	return s.store.DeleteOtherSessions(ctx, adminID, hashToken(currentToken))
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

// ResetPassword sets a new password from the server's command line (the
// installer, or an admin who forgot it) and ends every session. With
// disableTOTP it also switches two-factor authentication off, for a lost
// phone.
func (s *Service) ResetPassword(ctx context.Context, username, password string, disableTOTP bool) error {
	if err := validPassword(password); err != nil {
		return err
	}
	admin, err := s.store.AdminByUsername(ctx, username)
	if err != nil {
		return err
	}
	if err := s.store.SetAdminPassword(ctx, admin.ID, HashPassword(password)); err != nil {
		return err
	}
	if disableTOTP {
		if err := s.store.SetAdminTOTP(ctx, admin.ID, ""); err != nil {
			return err
		}
	}
	_, err = s.store.DeleteOtherSessions(ctx, admin.ID, "")
	return err
}

// Admins lists admin usernames.
func (s *Service) Admins(ctx context.Context) ([]string, error) { return s.store.AdminUsernames(ctx) }
