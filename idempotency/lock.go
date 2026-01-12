package idempotency

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Lock struct {
	path string
}

// AcquireLock creates an exclusive lock file.
// If the lock already exists and is not stale, it returns an error.
// If the lock is stale, it is removed and re-acquired.
func AcquireLock(path string, maxAge time.Duration) (*Lock, error) {
	now := time.Now().UTC()

	// Try fast path: exclusive create
	if tryCreateLock(path, now) {
		return &Lock{path: path}, nil
	}

	// Lock exists → check staleness
	info, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	pid, ts, err := parseLockInfo(string(info))
	if err != nil {
		return nil, fmt.Errorf("lock exists but is invalid: %w", err)
	}

	if now.Sub(ts) < maxAge {
		return nil, fmt.Errorf("lock already held (pid=%d, age=%s)", pid, now.Sub(ts))
	}

	// Stale lock → remove and retry once
	_ = os.Remove(path)

	if tryCreateLock(path, now) {
		return &Lock{path: path}, nil
	}

	return nil, errors.New("failed to acquire lock after removing stale lock")
}

// Release removes the lock file.
func (l *Lock) Release() error {
	return os.Remove(l.path)
}

// ---------- helpers ----------

func tryCreateLock(path string, now time.Time) bool {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()

	// Write: PID + timestamp (UTC)
	_, _ = fmt.Fprintf(f, "%d %s\n", os.Getpid(), now.Format(time.RFC3339))
	return true
}

func parseLockInfo(s string) (pid int, ts time.Time, err error) {
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return 0, time.Time{}, errors.New("invalid lock format")
	}

	pid, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, time.Time{}, err
	}

	ts, err = time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return 0, time.Time{}, err
	}

	return pid, ts, nil
}
