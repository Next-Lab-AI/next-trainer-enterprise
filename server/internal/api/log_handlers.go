package api

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
)

func (s *Server) jobLogPath(jobID string) string {
	return filepath.Join(s.LogDir, "jobs", jobID+".jsonl")
}

func (s *Server) handleJobLogs(w http.ResponseWriter, r *http.Request) {
	job := s.getJobChecked(w, r)
	if job == nil {
		return
	}
	f, err := os.Open(s.jobLogPath(job.JobID))
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/x-ndjson")
	io.Copy(w, f)
}

func (s *Server) handleJobLogsStream(w http.ResponseWriter, r *http.Request) {
	job := s.getJobChecked(w, r)
	if job == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	path := s.jobLogPath(job.JobID)
	var offset int64
	var pending string
	ctx := r.Context()

	terminal := func() bool {
		j, err := s.st.Jobs().GetJob(ctx, job.JobID)
		if err != nil {
			return true
		}
		return j.Status == store.JobStatusFinished || j.Status == store.JobStatusFailed ||
			j.Status == store.JobStatusTerminated
	}

	for {
		f, err := os.Open(path)
		if err == nil {
			f.Seek(offset, io.SeekStart)
			reader := bufio.NewReader(f)
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					offset += int64(len(line))
					if line[len(line)-1] == '\n' {
						fmt.Fprintf(w, "data: %s\n\n", line[:len(line)-1])
						pending = ""
					} else {
						offset -= int64(len(line))
						pending = line
					}
				}
				if err != nil {
					break
				}
			}
			f.Close()
			flusher.Flush()
		}
		_ = pending
		if terminal() {
			fmt.Fprintf(w, "event: done\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}
