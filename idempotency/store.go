package idempotency

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
	data map[string]time.Time
}

// Open loads (or creates) a JSON-backed idempotency store.
func Open(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: make(map[string]time.Time),
	}

	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Exists returns true if the key already exists.
func (s *Store) Exists(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.data[key]
	return ok
}

// Mark records the key with the current timestamp.
// Calling Mark multiple times with the same key is safe.
func (s *Store) Mark(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = time.Now().UTC()
	return s.saveLocked()
}

// Delete removes a key (optional helper).
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	return s.saveLocked()
}

// Keys returns a copy of all stored keys.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

// Close is a no-op but allows future extensions.
func (s *Store) Close() error {
	return nil
}

// ---------- internal ----------

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // empty store
		}
		return err
	}

	var raw map[string]time.Time
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	s.data = raw
	return nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	tmp := s.path + ".tmp"

	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, s.path)
}
