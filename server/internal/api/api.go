package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/jobs"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/pools"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/ws"
)

type Server struct {
	st       store.Store
	auth     *auth.Service
	pools    *pools.Service
	jobs     *jobs.Service
	datasets *datasets.Service
	uploads  *datasets.UploadManager
	hub      *ws.Hub
	LogDir   string
}

func New(st store.Store, authSvc *auth.Service, poolsSvc *pools.Service,
	jobsSvc *jobs.Service, datasetsSvc *datasets.Service, hub *ws.Hub) *Server {
	return &Server{st: st, auth: authSvc, pools: poolsSvc, jobs: jobsSvc, datasets: datasetsSvc, hub: hub}
}

func (s *Server) SetUploads(m *datasets.UploadManager) { s.uploads = m }

func (s *Server) Router(static http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	authed := http.NewServeMux()
	authed.HandleFunc("GET /api/auth/me", s.handleMe)
	authed.HandleFunc("POST /api/auth/password", s.handleChangePassword)
	authed.HandleFunc("GET /api/pools", s.handleListPools)
	authed.HandleFunc("GET /api/jobs", s.handleListJobs)
	authed.HandleFunc("POST /api/jobs", s.handleSubmitJob)
	authed.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	authed.HandleFunc("POST /api/jobs/{id}/stop", s.handleStopJob)
	authed.HandleFunc("POST /api/jobs/{id}/kill", s.handleKillJob)
	authed.HandleFunc("GET /api/jobs/{id}/events", s.handleJobEvents)
	authed.HandleFunc("GET /api/jobs/{id}/logs", s.handleJobLogs)
	authed.HandleFunc("GET /api/jobs/{id}/logs/stream", s.handleJobLogsStream)
	authed.HandleFunc("GET /api/jobs/{id}/artifacts", s.handleJobArtifacts)
	authed.HandleFunc("GET /api/datasets/{id}/download", s.handleDatasetDownload)
	authed.HandleFunc("GET /api/nodes", s.handleListNodes)
	authed.HandleFunc("GET /api/nodes/{id}", s.handleGetNode)
	authed.HandleFunc("GET /api/datasets", s.handleListDatasets)

	admin := http.NewServeMux()
	admin.HandleFunc("GET /api/users", s.handleListUsers)
	admin.HandleFunc("POST /api/users", s.handleCreateUser)
	admin.HandleFunc("PATCH /api/users/{id}", s.handleUpdateUser)
	admin.HandleFunc("GET /api/tokens", s.handleListTokens)
	admin.HandleFunc("POST /api/tokens", s.handleCreateToken)
	admin.HandleFunc("POST /api/tokens/{id}/revoke", s.handleRevokeToken)
	admin.HandleFunc("GET /api/settings/registration", s.handleGetRegistration)
	admin.HandleFunc("PUT /api/settings/registration", s.handleSetRegistration)
	admin.HandleFunc("POST /api/pools", s.handleCreatePool)
	admin.HandleFunc("DELETE /api/pools/{id}", s.handleDeletePool)
	admin.HandleFunc("POST /api/pools/{id}/nodes", s.handleAssignNode)
	admin.HandleFunc("POST /api/pools/{id}/users", s.handleGrantUser)
	admin.HandleFunc("POST /api/pools/{id}/datasets", s.handleGrantDataset)
	admin.HandleFunc("PATCH /api/nodes/{id}", s.handleUpdateNode)
	admin.HandleFunc("POST /api/nodes/{id}/revoke", s.handleRevokeNode)
	admin.HandleFunc("POST /api/nodes/{id}/env_gc", s.handleEnvGC)
	admin.HandleFunc("POST /api/nodes/{id}/provision", s.handleProvision)
	admin.HandleFunc("POST /api/datasets", s.handleUploadDataset)
	admin.HandleFunc("DELETE /api/datasets/{id}", s.handleDeleteDataset)
	admin.HandleFunc("POST /api/datasets/uploads", s.handleUploadInit)
	admin.HandleFunc("GET /api/datasets/uploads/{id}", s.handleUploadGet)
	admin.HandleFunc("PUT /api/datasets/uploads/{id}/chunk", s.handleUploadChunk)
	admin.HandleFunc("POST /api/datasets/uploads/{id}/complete", s.handleUploadComplete)

	mux.Handle("/api/", s.auth.RequireAuth(authed))
	mux.Handle("/api/users", s.auth.RequireAdmin(admin))
	mux.Handle("/api/users/", s.auth.RequireAdmin(admin))
	mux.Handle("/api/tokens", s.auth.RequireAdmin(admin))
	mux.Handle("/api/tokens/", s.auth.RequireAdmin(admin))
	mux.Handle("/api/settings/", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/pools", s.auth.RequireAdmin(admin))
	mux.Handle("DELETE /api/pools/{id}", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/pools/{id}/nodes", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/pools/{id}/users", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/pools/{id}/datasets", s.auth.RequireAdmin(admin))
	mux.Handle("PATCH /api/nodes/{id}", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/nodes/{id}/revoke", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/nodes/{id}/env_gc", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/nodes/{id}/provision", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/datasets", s.auth.RequireAdmin(admin))
	mux.Handle("DELETE /api/datasets/{id}", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/datasets/uploads", s.auth.RequireAdmin(admin))
	mux.Handle("GET /api/datasets/uploads/{id}", s.auth.RequireAdmin(admin))
	mux.Handle("PUT /api/datasets/uploads/{id}/chunk", s.auth.RequireAdmin(admin))
	mux.Handle("POST /api/datasets/uploads/{id}/complete", s.auth.RequireAdmin(admin))

	mux.HandleFunc("GET /datasets/{id}", s.handleAgentDatasetDownload)
	mux.HandleFunc("PUT /artifacts/{job_id}/{path...}", s.handleAgentArtifactUpload)

	mux.Handle("GET /agent/ws", s.hub)

	if static != nil {
		mux.Handle("/", static)
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func statusOf(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, datasets.ErrUploadNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict), errors.Is(err, datasets.ErrUploadOffset):
		return http.StatusConflict
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, pools.ErrForbidden),
		errors.Is(err, jobs.ErrForbidden), errors.Is(err, datasets.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, jobs.ErrBadRequest), errors.Is(err, datasets.ErrBadKind),
		errors.Is(err, datasets.ErrUploadSize):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
