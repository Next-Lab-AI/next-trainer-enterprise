package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/auth"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/datasets"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/jobs"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/pools"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/registry"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/ws"
)

type fixture struct {
	srv *httptest.Server
	st  store.Store
	hub *ws.Hub
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	authSvc := auth.NewService(db, time.Hour)
	reg := registry.New(db, authSvc)
	poolsSvc := pools.New(db)
	hub := ws.NewHub(reg, db, ws.Config{HeartbeatIntervalS: 15})
	logDir := t.TempDir()
	jobsSvc := jobs.New(db, poolsSvc, hub, jobs.Config{LogDir: logDir, HTTPBase: "http://test"})
	hub.SetHandler(jobsSvc)
	lb, err := datasets.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dsSvc := datasets.New(db, lb)
	s := New(db, authSvc, poolsSvc, jobsSvc, dsSvc, hub)
	s.LogDir = logDir
	srv := httptest.NewServer(s.Router(nil))
	t.Cleanup(srv.Close)
	return &fixture{srv: srv, st: db, hub: hub}
}

func (fx *fixture) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, fx.srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	rec.Body = new(bytes.Buffer)
	io.Copy(rec.Body, resp.Body)
	return rec
}

func login(t *testing.T, fx *fixture, user, pass string) string {
	t.Helper()
	rec := fx.do(t, "POST", "/api/auth/login", "", map[string]string{"username": user, "password": pass})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: %d %s", user, rec.Code, rec.Body)
	}
	var out struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Token == "" {
		t.Fatal("no token")
	}
	return out.Token
}

func bootstrap(t *testing.T, fx *fixture) string {
	t.Helper()
	rec := fx.do(t, "POST", "/api/bootstrap", "", map[string]string{"username": "admin", "password": "adminpw"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap: %d %s", rec.Code, rec.Body)
	}
	return login(t, fx, "admin", "adminpw")
}

func TestBootstrapLoginMe(t *testing.T) {
	fx := newFixture(t)

	rec := fx.do(t, "GET", "/api/auth/me", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth me: %d", rec.Code)
	}
	token := bootstrap(t, fx)

	rec = fx.do(t, "POST", "/api/bootstrap", "", map[string]string{"username": "x", "password": "y"})
	if rec.Code != http.StatusGone {
		t.Fatalf("second bootstrap: %d", rec.Code)
	}

	rec = fx.do(t, "GET", "/api/auth/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d", rec.Code)
	}
	var me userOut
	json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Username != "admin" || me.Role != store.RoleAdmin {
		t.Fatalf("me: %+v", me)
	}

	rec = fx.do(t, "POST", "/api/auth/logout", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := fx.do(t, "GET", "/api/auth/me", token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}
}

func TestAdminRBAC(t *testing.T) {
	fx := newFixture(t)
	adminTok := bootstrap(t, fx)

	rec := fx.do(t, "POST", "/api/users", adminTok, map[string]string{"username": "u1", "password": "pw1", "role": "user"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body)
	}
	userTok := login(t, fx, "u1", "pw1")

	if rec := fx.do(t, "GET", "/api/users", userTok, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("user list users: %d", rec.Code)
	}
	if rec := fx.do(t, "GET", "/api/users", adminTok, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin list users: %d", rec.Code)
	}
	if rec := fx.do(t, "POST", "/api/tokens", userTok, map[string]string{"label": "x"}); rec.Code != http.StatusForbidden {
		t.Fatalf("user create token: %d", rec.Code)
	}
	rec = fx.do(t, "POST", "/api/tokens", adminTok, map[string]string{"label": "ci"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin create token: %d", rec.Code)
	}
	var tokOut struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tokOut)
	if tokOut.Token == "" {
		t.Fatal("no plaintext token")
	}
	rec = fx.do(t, "POST", "/api/tokens/"+tokOut.ID+"/revoke", adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d", rec.Code)
	}
}

func TestPoolAndJobFlow(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	adminTok := bootstrap(t, fx)

	rec := fx.do(t, "POST", "/api/pools", adminTok, map[string]string{"name": "p1"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pool: %d", rec.Code)
	}
	var pool store.Pool
	json.Unmarshal(rec.Body.Bytes(), &pool)

	rec = fx.do(t, "POST", "/api/users", adminTok, map[string]string{"username": "u1", "password": "pw1"})
	var u userOut
	json.Unmarshal(rec.Body.Bytes(), &u)
	userTok := login(t, fx, "u1", "pw1")

	rec = fx.do(t, "POST", "/api/jobs", userTok, map[string]any{"pool_id": pool.PoolID, "engine": "sd-scripts"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("ungranted submit: %d %s", rec.Code, rec.Body)
	}

	rec = fx.do(t, "POST", "/api/pools/"+pool.PoolID+"/users", adminTok, map[string]any{"user_id": u.ID, "grant": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("grant: %d", rec.Code)
	}

	rec = fx.do(t, "POST", "/api/jobs", userTok, map[string]any{"pool_id": pool.PoolID, "engine": "sd-scripts"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body)
	}
	var job store.Job
	json.Unmarshal(rec.Body.Bytes(), &job)
	if job.Status != store.JobStatusQueued {
		t.Fatalf("job: %+v", job)
	}

	rec = fx.do(t, "GET", "/api/jobs", userTok, nil)
	var list []store.Job
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}

	rec = fx.do(t, "GET", "/api/jobs/"+job.JobID, userTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job: %d", rec.Code)
	}
	_ = ctx
}

func TestDatasetUploadDownload(t *testing.T) {
	fx := newFixture(t)
	adminTok := bootstrap(t, fx)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("name", "imgs")
	mw.WriteField("kind", "dataset")
	fw, _ := mw.CreateFormFile("file", "imgs.tar.gz")
	fw.Write([]byte("fake-dataset-content"))
	mw.Close()

	req, _ := http.NewRequest("POST", fx.srv.URL+"/api/datasets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	var d store.Dataset
	json.NewDecoder(resp.Body).Decode(&d)
	if d.SHA256 == "" || d.SizeBytes != int64(len("fake-dataset-content")) {
		t.Fatalf("dataset: %+v", d)
	}

	rec := fx.do(t, "GET", "/api/datasets", adminTok, nil)
	var list []store.Dataset
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}

	req, _ = http.NewRequest("GET", fx.srv.URL+"/datasets/"+d.DatasetID, nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("agent download no token: %d", resp2.StatusCode)
	}

	rec = fx.do(t, "DELETE", "/api/datasets/"+d.DatasetID, adminTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestRegistrationSetting(t *testing.T) {
	fx := newFixture(t)
	adminTok := bootstrap(t, fx)
	rec := fx.do(t, "GET", "/api/settings/registration", adminTok, nil)
	var out struct {
		Open bool `json:"open"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Open {
		t.Fatal("default open")
	}
	rec = fx.do(t, "PUT", "/api/settings/registration", adminTok, map[string]bool{"open": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("set: %d", rec.Code)
	}
	rec = fx.do(t, "GET", "/api/settings/registration", adminTok, nil)
	json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Open {
		t.Fatal("not open after set")
	}
}
