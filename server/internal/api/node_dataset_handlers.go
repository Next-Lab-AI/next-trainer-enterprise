package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

type nodeOut struct {
	*store.Node
	Online   bool            `json:"online"`
	Pools    []string        `json:"pools"`
	BaseEnvs json.RawMessage `json:"base_envs,omitempty"`
}

func (s *Server) decorateNode(r *http.Request, n *store.Node) nodeOut {
	pools, _ := s.st.Pools().PoolsOfNode(r.Context(), n.NodeID)
	if pools == nil {
		pools = []string{}
	}
	envs, _ := json.Marshal(s.hub.BaseEnvs(n.NodeID))
	return nodeOut{Node: n, Online: s.hub.Online(n.NodeID), Pools: pools, BaseEnvs: envs}
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

func (s *Server) handleListDatasets(w http.ResponseWriter, r *http.Request) {
	ds, err := s.st.Datasets().List(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ds == nil {
		ds = []*store.Dataset{}
	}
	writeJSON(w, http.StatusOK, ds)
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
	writeJSON(w, http.StatusCreated, d)
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
