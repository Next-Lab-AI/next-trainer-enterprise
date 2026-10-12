package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/jobs"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type jobOut struct {
	JobID         string          `json:"job_id"`
	UserID        string          `json:"user_id"`
	PoolID        string          `json:"pool_id"`
	NodeID        string          `json:"node_id"`
	Engine        string          `json:"engine"`
	EngineVersion string          `json:"engine_version"`
	ConfigTOML    string          `json:"config_toml"`
	Assets        json.RawMessage `json:"assets"`
	GPUs          json.RawMessage `json:"gpus"`
	Status        string          `json:"status"`
	ExitCode      *int64          `json:"exit_code"`
	Request       json.RawMessage `json:"request"`
	CreatedAt     int64           `json:"created_at"`
	StartedAt     *int64          `json:"started_at"`
	EndedAt       *int64          `json:"ended_at"`
}

func jobDTO(j *store.Job) jobOut {
	return jobOut{
		JobID: j.JobID, UserID: j.UserID, PoolID: j.PoolID, NodeID: j.NodeID,
		Engine: j.Engine, EngineVersion: j.EngineVersion, ConfigTOML: j.ConfigTOML,
		Assets: rawOr(j.Assets, "[]"), GPUs: rawOr(j.GPUs, "[]"), Request: rawOr(j.Request, "null"),
		Status: j.Status, ExitCode: j.ExitCode,
		CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, EndedAt: j.EndedAt,
	}
}

func (s *Server) handleListPools(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	ps, err := s.pools.VisiblePools(r.Context(), u)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ps == nil {
		ps = []*store.Pool{}
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) handleSubmitJob(w http.ResponseWriter, r *http.Request) {
	var req jobs.SubmitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request"))
		return
	}
	u := auth.UserFrom(r.Context())
	job, err := s.jobs.Submit(r.Context(), u, req)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, jobDTO(job))
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	f := store.JobFilter{
		PoolID: r.URL.Query().Get("pool_id"),
		Limit:  200,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	list, err := s.jobs.List(r.Context(), u, f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]jobOut, 0, len(list))
	for _, j := range list {
		out = append(out, jobDTO(j))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getJobChecked(w http.ResponseWriter, r *http.Request) *store.Job {
	u := auth.UserFrom(r.Context())
	job, err := s.st.Jobs().GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return nil
	}
	if u.Role != store.RoleAdmin && job.UserID != u.ID {
		ok, _ := s.pools.CanSubmit(r.Context(), u, job.PoolID)
		if !ok {
			writeErr(w, http.StatusForbidden, jobs.ErrForbidden)
			return nil
		}
	}
	return job
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job := s.getJobChecked(w, r)
	if job == nil {
		return
	}
	writeJSON(w, http.StatusOK, jobDTO(job))
}

func (s *Server) handleStopJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Preempt bool `json:"preempt"`
	}
	decodeJSON(r, &req)
	u := auth.UserFrom(r.Context())
	if err := s.jobs.Stop(r.Context(), u, r.PathValue("id"), req.Preempt); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

func (s *Server) handleKillJob(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	if err := s.jobs.Kill(r.Context(), u, r.PathValue("id")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job := s.getJobChecked(w, r)
	if job == nil {
		return
	}
	var after int64
	if v := r.URL.Query().Get("after_seq"); v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	evs, err := s.st.Jobs().ListEvents(r.Context(), job.JobID, after, 1000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if evs == nil {
		evs = []*store.Event{}
	}
	writeJSON(w, http.StatusOK, evs)
}
