package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Identity struct {
	NodeID       string `json:"node_id"`
	Credential   string `json:"credential"`
	SessionToken string `json:"session_token"`
	LastSeq      int64  `json:"last_seq"`
}

var ErrNoIdentity = errors.New("config: no identity file")

func IdentityPath(root string) string {
	return filepath.Join(root, "identity.json")
}

func LoadIdentity(root string) (Identity, error) {
	data, err := os.ReadFile(IdentityPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return Identity{}, ErrNoIdentity
	}
	if err != nil {
		return Identity{}, fmt.Errorf("config: read identity: %w", err)
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return Identity{}, fmt.Errorf("config: parse identity: %w", err)
	}
	return id, nil
}

func SaveIdentity(root string, id Identity) error {
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal identity: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("config: mkdir root: %w", err)
	}
	tmp, err := os.CreateTemp(root, ".identity-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp identity: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write identity: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("config: chmod identity: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: fsync identity: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close identity: %w", err)
	}
	if err := os.Rename(tmpName, IdentityPath(root)); err != nil {
		return fmt.Errorf("config: rename identity: %w", err)
	}
	return nil
}
