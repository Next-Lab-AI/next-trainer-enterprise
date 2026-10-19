package fetch

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixture is produced by @zip.js/zip.js (web upload path) with
// encryptionStrength 3 (AES-256); see docs/research-2026-10-17.
const zipJSPassword = "OpjWMbV3x7Q2K0s1mZ4yF8aBcDeFgHiJkLmNoPqRsTu="

func TestUnzipEncryptedZipJSInterop(t *testing.T) {
	dest := t.TempDir()
	if err := unzipEncrypted("testdata/enc-aes256.zip", dest, zipJSPassword); err != nil {
		t.Fatalf("unzip: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(got) != "hello encrypted world\n" {
		t.Fatalf("unexpected content: %q", got)
	}
	got, err = os.ReadFile(filepath.Join(dest, "sub", "dir", "note.md"))
	if err != nil {
		t.Fatalf("read nested: %v", err)
	}
	if string(got) != "# nested file\n" {
		t.Fatalf("unexpected nested content: %q", got)
	}
}

func TestUnzipEncryptedWrongPassword(t *testing.T) {
	if err := unzipEncrypted("testdata/enc-aes256.zip", t.TempDir(), "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestCleanZipName(t *testing.T) {
	for _, bad := range []string{"../evil", "..", "/abs", "a/../../b", ""} {
		if _, err := cleanZipName(bad); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}
	for _, good := range []string{"a/b.txt", "sub\\win.txt", "x"} {
		if _, err := cleanZipName(good); err != nil {
			t.Fatalf("unexpected rejection for %q: %v", good, err)
		}
	}
}
