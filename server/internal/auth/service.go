package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrUserDisabled       = errors.New("auth: user disabled")
	ErrBootstrapDone      = errors.New("auth: bootstrap already completed")
	ErrTokenExpired       = errors.New("auth: token expired")
	ErrTokenRevoked       = errors.New("auth: token revoked")
	ErrForbidden          = errors.New("auth: forbidden")
)

const SettingRegistrationOpen = "registration_open"

const SessionCookie = "nt_session"

type session struct {
	userID    string
	expiresAt time.Time
}

type Service struct {
	st       store.Store
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]*session
}

func NewService(st store.Store, sessionTTL time.Duration) *Service {
	return &Service{
		st:       st,
		ttl:      sessionTTL,
		now:      time.Now,
		sessions: make(map[string]*session),
	}
}

func (s *Service) BootstrapNeeded(ctx context.Context) (bool, error) {
	n, err := s.st.Users().Count(ctx)
	return n == 0, err
}

func (s *Service) CreateFirstAdmin(ctx context.Context, username, password string) (*store.User, error) {
	needed, err := s.BootstrapNeeded(ctx)
	if err != nil {
		return nil, err
	}
	if !needed {
		return nil, ErrBootstrapDone
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	u := &store.User{
		ID: store.NewID(), Username: username, PasswordHash: hash,
		Role: store.RoleAdmin, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.st.Users().Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (string, *store.User, error) {
	u, err := s.st.Users().GetByUsername(ctx, username)
	if err != nil {
		if err == store.ErrNotFound {
			VerifyPassword(password, timingDummyHash())
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, err
	}
	if !VerifyPassword(password, u.PasswordHash) {
		return "", nil, ErrInvalidCredentials
	}
	if u.Disabled {
		return "", nil, ErrUserDisabled
	}
	token := NewToken()
	s.mu.Lock()
	s.sessions[HashToken(token)] = &session{userID: u.ID, expiresAt: s.now().Add(s.ttl)}
	s.mu.Unlock()
	return token, u, nil
}

func (s *Service) Logout(ctx context.Context, token string) {
	s.mu.Lock()
	delete(s.sessions, HashToken(token))
	s.mu.Unlock()
}

func (s *Service) Authenticate(ctx context.Context, token string) (*store.User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	hash := HashToken(token)
	s.mu.Lock()
	sess, ok := s.sessions[hash]
	if ok && s.now().After(sess.expiresAt) {
		delete(s.sessions, hash)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, ErrInvalidCredentials
	}
	u, err := s.st.Users().Get(ctx, sess.userID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if u.Disabled {
		return nil, ErrUserDisabled
	}
	return u, nil
}

func (s *Service) ForceLogoutUser(ctx context.Context, userID string) {
	s.mu.Lock()
	for h, sess := range s.sessions {
		if sess.userID == userID {
			delete(s.sessions, h)
		}
	}
	s.mu.Unlock()
}

func (s *Service) ChangePassword(ctx context.Context, userID, oldPassword, newPassword string) error {
	u, err := s.st.Users().Get(ctx, userID)
	if err != nil {
		return err
	}
	if !VerifyPassword(oldPassword, u.PasswordHash) {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.UpdatedAt = s.now().Unix()
	return s.st.Users().Update(ctx, u)
}

func (s *Service) IssueAccessToken(ctx context.Context, label, createdBy string, expiresAt *int64) (string, *store.AccessToken, error) {
	plain := NewToken()
	t := &store.AccessToken{
		ID: store.NewID(), TokenHash: HashToken(plain), Label: label,
		CreatedBy: createdBy, CreatedAt: s.now().Unix(), ExpiresAt: expiresAt,
	}
	if err := s.st.AccessTokens().Create(ctx, t); err != nil {
		return "", nil, err
	}
	return plain, t, nil
}

func (s *Service) ValidateAccessToken(ctx context.Context, plain string) (*store.AccessToken, error) {
	t, err := s.st.AccessTokens().GetByHash(ctx, HashToken(plain))
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if t.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}
	if t.ExpiresAt != nil && s.now().Unix() > *t.ExpiresAt {
		return nil, ErrTokenExpired
	}
	return t, nil
}

func (s *Service) RevokeAccessToken(ctx context.Context, id string) error {
	return s.st.AccessTokens().Revoke(ctx, id, s.now().Unix())
}

func (s *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	v, err := s.st.Settings().Get(ctx, SettingRegistrationOpen)
	if err == store.ErrNotFound {
		return false, nil
	}
	return v == "1", err
}

func (s *Service) SetRegistrationOpen(ctx context.Context, open bool) error {
	v := "0"
	if open {
		v = "1"
	}
	return s.st.Settings().Set(ctx, SettingRegistrationOpen, v)
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func timingDummyHash() string {
	dummyHashOnce.Do(func() {
		dummyHash, _ = HashPassword("dummy")
	})
	return dummyHash
}
