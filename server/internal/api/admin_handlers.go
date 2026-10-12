package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	toks, err := s.st.AccessTokens().List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	type dto struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		CreatedBy string `json:"created_by"`
		CreatedAt int64  `json:"created_at"`
		ExpiresAt *int64 `json:"expires_at"`
		RevokedAt *int64 `json:"revoked_at"`
	}
	out := make([]dto, 0, len(toks))
	for _, t := range toks {
		out = append(out, dto{t.ID, t.Label, t.CreatedBy, t.CreatedAt, t.ExpiresAt, t.RevokedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label     string `json:"label"`
		ExpiresAt *int64 `json:"expires_at"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	u := auth.UserFrom(r.Context())
	plain, tok, err := s.auth.IssueAccessToken(r.Context(), req.Label, u.ID, req.ExpiresAt)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": plain, "id": tok.ID})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.RevokeAccessToken(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGetRegistration(w http.ResponseWriter, r *http.Request) {
	open, err := s.auth.RegistrationOpen(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open": open})
}

func (s *Server) handleSetRegistration(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Open bool `json:"open"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	if err := s.auth.SetRegistrationOpen(r.Context(), req.Open); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"open": req.Open})
}

func (s *Server) handleCreatePool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name required"))
		return
	}
	p := &store.Pool{PoolID: store.NewID(), Name: req.Name, CreatedAt: time.Now().Unix()}
	if err := s.st.Pools().Create(r.Context(), p); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleDeletePool(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Pools().Delete(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAssignNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"node_id"`
		Assign bool   `json:"assign"`
	}
	if err := decodeJSON(r, &req); err != nil || req.NodeID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("node_id required"))
		return
	}
	poolID := r.PathValue("id")
	var err error
	if req.Assign {
		err = s.st.Pools().AssignNode(r.Context(), req.NodeID, poolID)
	} else {
		err = s.st.Pools().UnassignNode(r.Context(), req.NodeID, poolID)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGrantUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Grant  bool   `json:"grant"`
	}
	if err := decodeJSON(r, &req); err != nil || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("user_id required"))
		return
	}
	poolID := r.PathValue("id")
	var err error
	if req.Grant {
		err = s.st.Pools().GrantUser(r.Context(), req.UserID, poolID)
	} else {
		err = s.st.Pools().RevokeUser(r.Context(), req.UserID, poolID)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleGrantDataset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DatasetID string `json:"dataset_id"`
		Grant     bool   `json:"grant"`
	}
	if err := decodeJSON(r, &req); err != nil || req.DatasetID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("dataset_id required"))
		return
	}
	poolID := r.PathValue("id")
	var err error
	if req.Grant {
		err = s.st.Pools().GrantDataset(r.Context(), req.DatasetID, poolID)
	} else {
		err = s.st.Pools().RevokeDataset(r.Context(), req.DatasetID, poolID)
	}
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description string            `json:"description"`
		Labels      map[string]string `json:"labels"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	labels := []byte("{}")
	if req.Labels != nil {
		labels, _ = json.Marshal(req.Labels)
	}
	if err := s.st.Nodes().UpdateMeta(r.Context(), r.PathValue("id"), req.Description, labels); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRevokeNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.Nodes().SetStatus(r.Context(), id, store.NodeStatusRevoked); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleEnvGC(w http.ResponseWriter, r *http.Request) {
	var req protocol.EnvGC
	decodeJSON(r, &req)
	u := auth.UserFrom(r.Context())
	id, err := s.hub.SendCommand(r.Context(), r.PathValue("id"), protocol.TypeEnvGC, req, u.ID)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"command_id": id})
}

func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	var req protocol.Provision
	if err := decodeJSON(r, &req); err != nil || req.Engine == "" || req.Package.URI == "" {
		writeErr(w, http.StatusBadRequest, errors.New("engine and package.uri required"))
		return
	}
	u := auth.UserFrom(r.Context())
	id, err := s.hub.SendCommand(r.Context(), r.PathValue("id"), protocol.TypeProvision, req, u.ID)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"command_id": id})
}
