package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type nodeOut struct {
	NodeID          string          `json:"node_id"`
	Hostname        string          `json:"hostname"`
	Description     string          `json:"description"`
	AgentVersion    string          `json:"agent_version"`
	ProtocolVersion string          `json:"protocol_version"`
	Labels          json.RawMessage `json:"labels"`
	GPUs            json.RawMessage `json:"gpus"`
	Status          string          `json:"status"`
	FirstSeen       int64           `json:"first_seen"`
	LastSeen        int64           `json:"last_seen"`
	Online          bool                  `json:"online"`
	Pools           []string              `json:"pools"`
	BaseEnvs        json.RawMessage       `json:"base_envs,omitempty"`
	Engines         []protocol.EngineInfo `json:"engines"`
}

func rawOr(b []byte, def string) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(def)
	}
	return json.RawMessage(b)
}

func (s *Server) decorateNode(r *http.Request, n *store.Node) nodeOut {
	pools, _ := s.st.Pools().PoolsOfNode(r.Context(), n.NodeID)
	if pools == nil {
		pools = []string{}
	}
	envs, _ := json.Marshal(s.hub.BaseEnvs(n.NodeID))
	engines := s.hub.Engines(n.NodeID)
	if engines == nil {
		engines = []protocol.EngineInfo{}
	}
	return nodeOut{
		NodeID:          n.NodeID,
		Hostname:        n.Hostname,
		Description:     n.Description,
		AgentVersion:    n.AgentVersion,
		ProtocolVersion: n.ProtocolVersion,
		Labels:          rawOr(n.Labels, "null"),
		GPUs:            rawOr(n.GPUs, "[]"),
		Status:          n.Status,
		FirstSeen:       n.FirstSeen,
		LastSeen:        n.LastSeen,
		Online:          s.hub.Online(n.NodeID),
		Pools:           pools,
		BaseEnvs:        envs,
		Engines:         engines,
	}
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	ns, err := s.st.Nodes().List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]nodeOut, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.decorateNode(r, n))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.Nodes().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, s.decorateNode(r, n))
}

type datasetOut struct {
	DatasetID string `json:"dataset_id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Backend   string `json:"backend"`
	URI       string `json:"uri"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	JobID     string `json:"job_id"`
	DeletedAt *int64 `json:"deleted_at"`
}

func datasetDTO(d *store.Dataset) datasetOut {
	return datasetOut{
		DatasetID: d.DatasetID, Name: d.Name, Version: d.Version, Kind: d.Kind,
		Backend: d.Backend, URI: d.URI, SHA256: d.SHA256, SizeBytes: d.SizeBytes,
		CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, JobID: d.JobID, DeletedAt: d.DeletedAt,
	}
}

func (s *Server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	ds, err := s.st.Datasets().List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]datasetOut, 0, len(ds))
	for _, d := range ds {
		out = append(out, datasetDTO(d))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUploadDataset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("multipart form required"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("file field required"))
		return
	}
	defer file.Close()
	kind := r.FormValue("kind")
	if kind == "" {
		kind = store.DatasetKindDataset
	}
	u := auth.UserFrom(r.Context())
	d, err := s.datasets.Upload(r.Context(), u.ID,
		firstNonEmpty(r.FormValue("name"), header.Filename),
		r.FormValue("version"), kind, r.FormValue("backend"), header.Filename, file)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, datasetDTO(d))
}

func (s *Server) handleDeleteDataset(w http.ResponseWriter, r *http.Request) {
	if err := s.datasets.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) agentNode(w http.ResponseWriter, r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if len(h) <= 7 || h[:7] != "Bearer " {
		writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return "", false
	}
	nodeID, ok := s.hub.AuthenticateSession(h[7:])
	if !ok {
		writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return "", false
	}
	return nodeID, true
}

func (s *Server) handleAgentDatasetDownload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.agentNode(w, r); !ok {
		return
	}
	datasetID := r.PathValue("id")
	jobID := r.URL.Query().Get("job_id")
	if jobID != "" {
		ok, err := s.datasets.CanAgentDownload(r.Context(), datasetID, jobID)
		if err != nil || !ok {
			writeErr(w, http.StatusForbidden, datasets.ErrForbidden)
			return
		}
	}
	rc, d, err := s.datasets.Download(r.Context(), datasetID)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-SHA256", d.SHA256)
	io.Copy(w, rc)
}

func (s *Server) handleAgentArtifactUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.agentNode(w, r); !ok {
		return
	}
	defer r.Body.Close()
	d, err := s.datasets.ReceiveArtifact(r.Context(), r.PathValue("job_id"), r.PathValue("path"), r.Body)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"dataset_id": d.DatasetID})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
