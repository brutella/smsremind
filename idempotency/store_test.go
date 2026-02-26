package idempotency

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreMarkExistsDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sent.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}

	if s.Exists("k1") {
		t.Fatalf("did not expect key")
	}

	if err := s.Mark("k1"); err != nil {
		t.Fatalf("Mark error = %v", err)
	}
	if !s.Exists("k1") {
		t.Fatalf("expected key to exist")
	}

	if err := s.Delete("k1"); err != nil {
		t.Fatalf("Delete error = %v", err)
	}
	if s.Exists("k1") {
		t.Fatalf("did not expect key after delete")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected state file, got %v", err)
	}
}
