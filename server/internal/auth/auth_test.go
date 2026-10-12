package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
)

func newService(t *testing.T) (*Service, store.Store) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, time.Hour), db
}

func TestPasswordRoundtrip(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("s3cret", h) {
		t.Fatal("verify failed")
	}
	if VerifyPassword("wrong", h) {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("x", "garbage") {
		t.Fatal("garbage hash accepted")
	}
}

func TestBootstrapAndLogin(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)

	if needed, _ := s.BootstrapNeeded(ctx); !needed {
		t.Fatal("expected bootstrap needed")
	}
	u, err := s.CreateFirstAdmin(ctx, "admin", "pw123456")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != store.RoleAdmin {
		t.Fatalf("role: %s", u.Role)
	}
	if _, err := s.CreateFirstAdmin(ctx, "second", "pw"); !errors.Is(err, ErrBootstrapDone) {
		t.Fatalf("second bootstrap: %v", err)
	}

	if _, _, err := s.Login(ctx, "admin", "bad"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad pw: %v", err)
	}
	if _, _, err := s.Login(ctx, "ghost", "pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad user: %v", err)
	}
	token, u2, err := s.Login(ctx, "admin", "pw123456")
	if err != nil || u2.ID != u.ID {
		t.Fatalf("login: %v", err)
	}
	got, err := s.Authenticate(ctx, token)
	if err != nil || got.Username != "admin" {
		t.Fatalf("auth: %v %+v", err, got)
	}
	s.Logout(ctx, token)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("after logout: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewService(db, time.Minute)
	now := time.Now()
	s.now = func() time.Time { return now }

	s.CreateFirstAdmin(ctx, "admin", "pw")
	token, _, err := s.Login(ctx, "admin", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expired: %v", err)
	}
}

func TestDisabledUser(t *testing.T) {
	ctx := context.Background()
	s, st := newService(t)
	u, _ := s.CreateFirstAdmin(ctx, "admin", "pw")
	token, _, _ := s.Login(ctx, "admin", "pw")

	u.Disabled = true
	st.Users().Update(ctx, u)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled session: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin", "pw"); !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("disabled login: %v", err)
	}
}

func TestAccessTokens(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	u, _ := s.CreateFirstAdmin(ctx, "admin", "pw")

	plain, tok, err := s.IssueAccessToken(ctx, "ci-node", u.ID, nil)
	if err != nil || plain == "" {
		t.Fatal(err)
	}
	got, err := s.ValidateAccessToken(ctx, plain)
	if err != nil || got.ID != tok.ID {
		t.Fatalf("validate: %v", err)
	}
	if _, err := s.ValidateAccessToken(ctx, "bogus"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bogus: %v", err)
	}
	if err := s.RevokeAccessToken(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateAccessToken(ctx, plain); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("revoked: %v", err)
	}

	past := time.Now().Add(-time.Hour).Unix()
	plain2, _, err := s.IssueAccessToken(ctx, "expired", u.ID, &past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateAccessToken(ctx, plain2); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired: %v", err)
	}
}

func TestRegistrationWindow(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	if open, _ := s.RegistrationOpen(ctx); open {
		t.Fatal("default should be closed")
	}
	if err := s.SetRegistrationOpen(ctx, true); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.RegistrationOpen(ctx); !open {
		t.Fatal("expected open")
	}
}

func TestMiddleware(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	s.CreateFirstAdmin(ctx, "admin", "pw")
	token, _, _ := s.Login(ctx, "admin", "pw")

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil || u.Username != "admin" {
			t.Error("no user in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	s.RequireAuth(ok).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.RequireAuth(ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer: %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	rec = httptest.NewRecorder()
	s.RequireAdmin(ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie admin: %d", rec.Code)
	}

	st, _ := sqlite.Open(":memory:")
	defer st.Close()
	s2 := NewService(st, time.Hour)
	s2.CreateFirstAdmin(ctx, "admin", "pw")
	st.Users().Create(ctx, &store.User{ID: store.NewID(), Username: "u1",
		PasswordHash: mustHash("pw"), Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1})
	utok, _, _ := s2.Login(ctx, "u1", "pw")
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+utok)
	rec = httptest.NewRecorder()
	s2.RequireAdmin(ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user as admin: %d", rec.Code)
	}
}

func mustHash(pw string) string {
	h, err := HashPassword(pw)
	if err != nil {
		panic(err)
	}
	return h
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()
	s, _ := newService(t)
	u, _ := s.CreateFirstAdmin(ctx, "admin", "oldpw")
	if err := s.ChangePassword(ctx, u.ID, "wrong", "newpw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong old: %v", err)
	}
	if err := s.ChangePassword(ctx, u.ID, "oldpw", "newpw"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Login(ctx, "admin", "oldpw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old pw still works: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin", "newpw"); err != nil {
		t.Fatalf("new pw: %v", err)
	}
}
