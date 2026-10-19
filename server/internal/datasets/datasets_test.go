package datasets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store"
	"github.com/Next-Lab-AI/next-trainer-enterprise/server/internal/store/sqlite"
)

func newService(t *testing.T) (*Service, store.Store, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lb, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(db, lb), db, lb.root
}

func TestUploadDownloadRoundtrip(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	content := "hello dataset"
	sum := sha256.Sum256([]byte(content))

	d, err := svc.Upload(ctx, "admin", "imgs", "v1", store.DatasetKindDataset, "", "imgs.tar.gz", "", "", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if d.Backend != store.BackendLocal || d.SizeBytes != int64(len(content)) ||
		d.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("meta: %+v", d)
	}

	rc, got, err := svc.Download(ctx, d.DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != content || got.DatasetID != d.DatasetID {
		t.Fatalf("download: %q", data)
	}

	if _, _, err := svc.Download(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestUploadBadKind(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	if _, err := svc.Upload(ctx, "admin", "x", "", "artifact", "", "f", "", "", strings.NewReader("x")); !errors.Is(err, ErrBadKind) {
		t.Fatalf("artifact via upload: %v", err)
	}
}

func TestSoftDelete(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newService(t)
	d, _ := svc.Upload(ctx, "admin", "imgs", "", store.DatasetKindDataset, "", "f.bin", "", "", strings.NewReader("x"))
	if err := svc.Delete(ctx, d.DatasetID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Download(ctx, d.DatasetID); !errors.Is(err, ErrDeleted) {
		t.Fatalf("deleted download: %v", err)
	}
	got, _ := st.Datasets().Get(ctx, d.DatasetID)
	if got.DeletedAt == nil {
		t.Fatal("not soft deleted")
	}
}

func TestReceiveArtifact(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newService(t)
	u := &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	st.Users().Create(ctx, u)
	p := &store.Pool{PoolID: store.NewID(), Name: "p1", CreatedAt: 1}
	st.Pools().Create(ctx, p)
	j := &store.Job{JobID: store.NewID(), UserID: u.ID, PoolID: p.PoolID,
		Engine: "sd-scripts", ConfigTOML: "x=1", Status: store.JobStatusRunning, CreatedAt: 1}
	st.Jobs().CreateJob(ctx, j)

	d, err := svc.ReceiveArtifact(ctx, j.JobID, "output/model.safetensors", strings.NewReader("weights"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != store.DatasetKindArtifact || d.JobID != j.JobID {
		t.Fatalf("artifact: %+v", d)
	}
	pools, _ := st.Pools().PoolsOfDataset(ctx, d.DatasetID)
	if len(pools) != 1 || pools[0] != p.PoolID {
		t.Fatalf("artifact pool grant: %v", pools)
	}
	rc, _, err := svc.Download(ctx, d.DatasetID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "weights" {
		t.Fatalf("content: %q", data)
	}

	if _, err := svc.ReceiveArtifact(ctx, "ghost", "x", strings.NewReader("x")); err == nil {
		t.Fatal("artifact for unknown job accepted")
	}
	if _, err := svc.ReceiveArtifact(ctx, j.JobID, "../escape", strings.NewReader("x")); err == nil {
		t.Fatal("path escape accepted")
	}
}

func TestCanAgentDownload(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newService(t)
	u := &store.User{ID: store.NewID(), Username: "u1", PasswordHash: "h", Role: store.RoleUser, CreatedAt: 1, UpdatedAt: 1}
	st.Users().Create(ctx, u)
	p := &store.Pool{PoolID: store.NewID(), Name: "p1", CreatedAt: 1}
	st.Pools().Create(ctx, p)
	j := &store.Job{JobID: store.NewID(), UserID: u.ID, PoolID: p.PoolID,
		Engine: "sd-scripts", ConfigTOML: "x=1", Status: store.JobStatusRunning, CreatedAt: 1}
	st.Jobs().CreateJob(ctx, j)
	d, _ := svc.Upload(ctx, u.ID, "imgs", "", store.DatasetKindDataset, "", "f", "", "", strings.NewReader("x"))

	ok, err := svc.CanAgentDownload(ctx, d.DatasetID, j.JobID)
	if err != nil || ok {
		t.Fatalf("ungranted: %v %v", ok, err)
	}
	st.Pools().GrantDataset(ctx, d.DatasetID, p.PoolID)
	ok, err = svc.CanAgentDownload(ctx, d.DatasetID, j.JobID)
	if err != nil || !ok {
		t.Fatalf("granted: %v %v", ok, err)
	}
}
