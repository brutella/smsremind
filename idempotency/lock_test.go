package idempotency

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireLockAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "smsremind.lock")

	lock, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock error = %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected lock file removed, stat err = %v", err)
	}
}

func TestAcquireLockHeld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "smsremind.lock")

	first, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock first error = %v", err)
	}
	defer first.Release()

	_, err = AcquireLock(path, time.Minute)
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}
}

func TestAcquireLockRemovesStaleLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "smsremind.lock")
	old := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	if err := os.WriteFile(path, []byte("1234 "+old+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	lock, err := AcquireLock(path, time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock stale error = %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release error = %v", err)
	}
}
