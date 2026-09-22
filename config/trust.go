package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// TrustStore records which project directories the user trusted, keyed by
// absolute path and fingerprinted by path plus git remote origin URL. A
// fingerprint change (moved project, changed remote) revokes trust until
// the user trusts again.
type TrustStore struct {
	path string

	mu      sync.Mutex
	entries map[string]trustEntry
}

type trustEntry struct {
	Fingerprint string `json:"fingerprint"`
	TrustedAt   string `json:"trustedAt"`
}

// OpenTrustStore loads (or creates) the store at file.
func OpenTrustStore(file string) (*TrustStore, error) {
	s := &TrustStore{path: file, entries: map[string]trustEntry{}}
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read trust store: %w", err)
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, fmt.Errorf("config: parse trust store: %w", err)
	}
	return s, nil
}

// Status reports whether dir is trusted under its current fingerprint.
func (s *TrustStore) Status(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[abs]
	return ok && e.Fingerprint == fingerprint(abs)
}

// Trust records the current fingerprint of dir.
func (s *TrustStore) Trust(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[abs] = trustEntry{Fingerprint: fingerprint(abs), TrustedAt: time.Now().UTC().Format(time.RFC3339)}
	return s.saveLocked()
}

// Untrust removes dir's record.
func (s *TrustStore) Untrust(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, abs)
	return s.saveLocked()
}

func (s *TrustStore) saveLocked() error {
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("config: trust store dir: %w", err)
	}
	if err := os.WriteFile(s.path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("config: write trust store: %w", err)
	}
	return nil
}

// fingerprint hashes the project path and its git remote origin URL.
func fingerprint(dir string) string {
	remote := ""
	if out, err := exec.Command("git", "-C", dir, "config", "--get", "remote.origin.url").Output(); err == nil {
		remote = string(out)
	}
	sum := sha256.Sum256([]byte(dir + "\x00" + remote))
	return hex.EncodeToString(sum[:])
}
