package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
)

type uploadInitIn struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	EncAlgo  string `json:"enc_algo"`
	EncKey   string `json:"enc_key"`
	Size     int64  `json:"size"`
}

type uploadSessionOut struct {
	ID       string `json:"upload_id"`
	Received int64  `json:"received"`
	Size     int64  `json:"size"`
}

func sessionOut(s *datasets.UploadSession) uploadSessionOut {
	return uploadSessionOut{ID: s.ID, Received: s.Received, Size: s.Size}
}

func (s *Server) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var in uploadInitIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad json"))
		return
	}
	if in.Name == "" {
		in.Name = in.Filename
	}
	if in.Name == "" || in.Filename == "" {
		writeErr(w, http.StatusBadRequest, errors.New("name and filename required"))
		return
	}
	u := auth.UserFrom(r.Context())
	sess, err := s.uploads.Init(u.ID, in.Name, in.Version, in.Filename, in.EncAlgo, in.EncKey, in.Size)
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, sessionOut(sess))
}

func (s *Server) handleUploadGet(w http.ResponseWriter, r *http.Request) {
	sess, err := s.uploads.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, sessionOut(sess))
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		writeErr(w, http.StatusBadRequest, errors.New("bad offset"))
		return
	}
	sess, err := s.uploads.WriteChunk(r.PathValue("id"), offset, r.Body)
	if err != nil {
		if errors.Is(err, datasets.ErrUploadOffset) && sess != nil {
			w.Header().Set("X-Received", strconv.FormatInt(sess.Received, 10))
		}
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, sessionOut(sess))
}

func (s *Server) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	d, err := s.uploads.Complete(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, datasetDTO(d))
}
