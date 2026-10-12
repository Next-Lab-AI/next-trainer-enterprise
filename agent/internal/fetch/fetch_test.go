package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Next-Lab-AI/next-trainer-enterprise/protocol"
)

func sha256hex(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestFetchFileScheme(t *testing.T) {
	root := t.TempDir()
	content := []byte("model weights here")
	src := filepath.Join(t.TempDir(), "ds.bin")
	os.WriteFile(src, content, 0o644)

	f := New(root, "", func() string { return "" }, S3ConfigFromEnv)
	p, sum, err := f.Fetch(context.Background(), protocol.Asset{URI: "file://" + src, SHA256: sha256hex(content)})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != string(content) || sum != sha256hex(content) {
		t.Fatalf("content mismatch")
	}

	p2, _, err := f.Fetch(context.Background(), protocol.Asset{URI: "file://" + src, SHA256: sha256hex(content)})
	if err != nil || p2 != p {
		t.Fatalf("cache hit failed: %v %v", p2, err)
	}

	os.WriteFile(src, []byte("tampered"), 0o644)
	if _, _, err := f.Fetch(context.Background(), protocol.Asset{URI: "file://" + src + ".other", SHA256: sha256hex(content)}); err == nil {
		t.Fatal("sha256 mismatch not detected")
	}
}

func TestFetchSchemeWhitelist(t *testing.T) {
	f := New(t.TempDir(), "", func() string { return "" }, S3ConfigFromEnv)
	if _, _, err := f.Fetch(context.Background(), protocol.Asset{URI: "ftp://x/y"}); err == nil {
		t.Fatal("ftp scheme accepted")
	}
}

func TestFetchHTTPSOnlyServerBase(t *testing.T) {
	content := []byte("dataset")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write(content)
	}))
	defer srv.Close()

	f := New(t.TempDir(), srv.URL, func() string { return "tok" }, S3ConfigFromEnv)
	_, sum, err := f.Fetch(context.Background(), protocol.Asset{URI: srv.URL + "/datasets/1"})
	if err != nil || sum != sha256hex(content) {
		t.Fatalf("https fetch: %v %v", sum, err)
	}

	if _, _, err := f.Fetch(context.Background(), protocol.Asset{URI: "https://evil.example.com/x"}); err == nil {
		t.Fatal("non-server https accepted")
	}
}

func TestUploadArtifact(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	root := t.TempDir()
	local := filepath.Join(root, "ckpt.safetensors")
	os.WriteFile(local, []byte("weights"), 0o644)

	f := New(root, srv.URL, func() string { return "tok" }, S3ConfigFromEnv)
	if err := f.UploadArtifact(context.Background(), "j1", local, "output/ckpt.safetensors"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gotPath != "/artifacts/j1/output/ckpt.safetensors" || gotAuth != "Bearer tok" || gotBody != "weights" {
		t.Fatalf("upload mismatch: %q %q %q", gotPath, gotAuth, gotBody)
	}
}

func TestGC(t *testing.T) {
	root := t.TempDir()
	f := New(root, "", func() string { return "" }, S3ConfigFromEnv)
	ctx := context.Background()

	mk := func(uri string, content string) string {
		src := filepath.Join(t.TempDir(), "f")
		os.WriteFile(src, []byte(content), 0o644)
		p, _, err := f.Fetch(ctx, protocol.Asset{URI: "file://" + src})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := mk("file:///a", "aaa")
	referenced := mk("file:///b", "bbb")

	past := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(old, past, past)
	os.Chtimes(referenced, past, past)

	jobDir := filepath.Join(root, "jobs", "j1")
	if _, err := LinkIntoJob(referenced, jobDir, "data.bin"); err != nil {
		t.Fatalf("LinkIntoJob: %v", err)
	}

	removed, err := f.GC(ctx, 30)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed = %v", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old unreferenced cache survived")
	}
	if _, err := os.Stat(referenced); err != nil {
		t.Fatal("referenced cache removed")
	}
}

func TestWsToHTTPBase(t *testing.T) {
	if got := WsToHTTPBase("wss://ctrl.example.com/ws"); got != "https://ctrl.example.com/ws" {
		t.Fatalf("got %q", got)
	}
}
