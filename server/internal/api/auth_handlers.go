package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type bootstrapReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	needed, err := s.auth.BootstrapNeeded(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !needed {
		writeErr(w, http.StatusGone, auth.ErrBootstrapDone)
		return
	}
	var req bootstrapReq
	if err := decodeJSON(r, &req); err != nil || req.Username == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, errors.New("username and password required"))
		return
	}
	u, err := s.auth.CreateFirstAdmin(r.Context(), req.Username, req.Password)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, userDTO(u))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req bootstrapReq
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	token, u, err := s.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(12 * time.Hour),
	})
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": userDTO(u)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerOrCookie(r)
	if token != "" {
		s.auth.Logout(r.Context(), token)
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func bearerOrCookie(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userDTO(auth.UserFrom(r.Context())))
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old_password"`
		New string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.New == "" {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	u := auth.UserFrom(r.Context())
	if err := s.auth.ChangePassword(r.Context(), u.ID, req.Old, req.New); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type userOut struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Disabled  bool   `json:"disabled"`
	CreatedAt int64  `json:"created_at"`
}

func userDTO(u *store.User) userOut {
	return userOut{ID: u.ID, Username: u.Username, Role: u.Role, Disabled: u.Disabled, CreatedAt: u.CreatedAt}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.Users().List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]userOut, 0, len(users))
	for _, u := range users {
		out = append(out, userDTO(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Username == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, errors.New("username and password required"))
		return
	}
	if req.Role == "" {
		req.Role = store.RoleUser
	}
	if req.Role != store.RoleAdmin && req.Role != store.RoleUser {
		writeErr(w, http.StatusBadRequest, errors.New("bad role"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().Unix()
	u := &store.User{ID: store.NewID(), Username: req.Username, PasswordHash: hash,
		Role: req.Role, CreatedAt: now, UpdatedAt: now}
	if err := s.st.Users().Create(r.Context(), u); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, userDTO(u))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, err := s.st.Users().Get(r.Context(), id)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	var req struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	if req.Role != nil {
		if *req.Role != store.RoleAdmin && *req.Role != store.RoleUser {
			writeErr(w, http.StatusBadRequest, errors.New("bad role"))
			return
		}
		u.Role = *req.Role
	}
	if req.Disabled != nil {
		u.Disabled = *req.Disabled
	}
	if req.Password != nil {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		u.PasswordHash = hash
	}
	u.UpdatedAt = time.Now().Unix()
	if err := s.st.Users().Update(r.Context(), u); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	if u.Disabled {
		s.auth.ForceLogoutUser(r.Context(), u.ID)
	}
	writeJSON(w, http.StatusOK, userDTO(u))
}
