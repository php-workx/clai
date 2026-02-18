//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_Success(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "picker.lock")

	fd, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}
	defer releaseLock(fd)

	// Lock file should exist.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lock file was not created")
	}
}

func TestAcquireLock_SecondInstanceFails(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "picker.lock")

	// Acquire first lock.
	fd1, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("first acquireLock failed: %v", err)
	}

	// Second lock should fail with EWOULDBLOCK.
	fd2, err := acquireLock(lockPath)
	if err == nil {
		releaseLock(fd2)
		releaseLock(fd1)
		t.Fatal("expected second acquireLock to fail, but it succeeded")
	}

	// Release the first lock.
	releaseLock(fd1)

	// Now the second attempt should succeed.
	fd3, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("third acquireLock (after release) failed: %v", err)
	}
	releaseLock(fd3)
}

func TestReleaseLock_InvalidFd(t *testing.T) {
	// Releasing with -1 should not panic.
	releaseLock(-1)
}
