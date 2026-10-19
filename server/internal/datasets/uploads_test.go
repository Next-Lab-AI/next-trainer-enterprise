package datasets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUploadManagerChunkedRoundtrip(t *testing.T) {
	svc, _, _ := newService(t)
	m := NewUploadManager(t.TempDir(), svc)
	ctx := context.Background()

	content := "0123456789abcdef0123456789abcdef"
	sess, err := m.Init("u1", "imgs", "v1", "imgs.zip", "zip-aes256", "pw", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteChunk(sess.ID, 5, strings.NewReader(content[:10])); !errors.Is(err, ErrUploadOffset) {
		t.Fatalf("expected offset mismatch, got %v", err)
	}
	if _, err := m.WriteChunk(sess.ID, 0, strings.NewReader(content[:10])); err != nil {
		t.Fatal(err)
	}
	sess, err = m.WriteChunk(sess.ID, 10, strings.NewReader(content[10:]))
	if err != nil {
		t.Fatal(err)
	}
	if sess.Received != int64(len(content)) {
		t.Fatalf("received %d", sess.Received)
	}
	d, err := m.Complete(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "imgs" || d.EncAlgo != "zip-aes256" || d.EncKey != "pw" {
		t.Fatalf("dataset: %+v", d)
	}
	if d.SizeBytes != int64(len(content)) {
		t.Fatalf("size %d", d.SizeBytes)
	}
	if _, err := m.Get(sess.ID); !errors.Is(err, ErrUploadNotFound) {
		t.Fatalf("session should be gone after complete, got %v", err)
	}
}

func TestUploadManagerCompleteIncomplete(t *testing.T) {
	svc, _, _ := newService(t)
	m := NewUploadManager(t.TempDir(), svc)
	sess, err := m.Init("u1", "x", "", "x.zip", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.WriteChunk(sess.ID, 0, strings.NewReader("short")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Complete(context.Background(), sess.ID); !errors.Is(err, ErrUploadSize) {
		t.Fatalf("expected size error, got %v", err)
	}
}
