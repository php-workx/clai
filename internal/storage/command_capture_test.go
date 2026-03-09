package storage

import (
	"context"
	"testing"
	"time"
)

func TestSQLiteStore_CommandCaptureLifecycle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	startTS := time.Now().Add(-2 * time.Second).UnixMilli()
	endTS := time.Now().UnixMilli()

	if err := store.UpsertCommandEventStart(ctx, "sess-1", "cmd-1", startTS); err != nil {
		t.Fatalf("UpsertCommandEventStart() error = %v", err)
	}

	result, err := store.DB().ExecContext(ctx, `
		UPDATE command_events SET captured_bytes = ? WHERE command_id = ?
	`, 1234, "cmd-1")
	if err != nil {
		t.Fatalf("seed captured_bytes error = %v", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		t.Fatalf("expected one seeded row, got %d", rows)
	}

	if err := store.FinalizeCommandEvent(ctx, "cmd-1", 1, endTS, true, 1234); err != nil {
		t.Fatalf("FinalizeCommandEvent() error = %v", err)
	}

	var (
		exitCode      int
		storedEndTS   int64
		isSensitive   int
		capturedBytes int64
	)
	err = store.DB().QueryRowContext(ctx, `
		SELECT exit_code, end_ts, is_sensitive, captured_bytes
		FROM command_events
		WHERE command_id = ?
	`, "cmd-1").Scan(&exitCode, &storedEndTS, &isSensitive, &capturedBytes)
	if err != nil {
		t.Fatalf("query command_events error = %v", err)
	}

	if exitCode != 1 {
		t.Errorf("exit_code = %d, want 1", exitCode)
	}
	if storedEndTS != endTS {
		t.Errorf("end_ts = %d, want %d", storedEndTS, endTS)
	}
	if isSensitive != 1 {
		t.Errorf("is_sensitive = %d, want 1", isSensitive)
	}
	if capturedBytes != 1234 {
		t.Errorf("captured_bytes = %d, want 1234", capturedBytes)
	}
}

func TestSQLiteStore_AppendCommandOutputChunk_AndPrune(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UnixMilli()

	if err := store.AppendCommandOutputChunk(ctx, "cmd-2", []byte("hello "), false, now, now+5000); err != nil {
		t.Fatalf("append stdout chunk 1 error = %v", err)
	}
	if err := store.AppendCommandOutputChunk(ctx, "cmd-2", []byte("world"), false, now, now+5000); err != nil {
		t.Fatalf("append stdout chunk 2 error = %v", err)
	}
	if err := store.AppendCommandOutputChunk(ctx, "cmd-2", []byte("oops"), true, now, now+5000); err != nil {
		t.Fatalf("append stderr chunk error = %v", err)
	}

	var stdoutBlob, stderrBlob []byte
	err := store.DB().QueryRowContext(ctx, `
		SELECT stdout_blob, stderr_blob FROM command_output WHERE command_id = ?
	`, "cmd-2").Scan(&stdoutBlob, &stderrBlob)
	if err != nil {
		t.Fatalf("query command_output error = %v", err)
	}

	if string(stdoutBlob) != "hello world" {
		t.Errorf("stdout_blob = %q, want %q", string(stdoutBlob), "hello world")
	}
	if string(stderrBlob) != "oops" {
		t.Errorf("stderr_blob = %q, want %q", string(stderrBlob), "oops")
	}

	// Create an already-expired row and ensure pruning removes it.
	_, err = store.DB().ExecContext(ctx, `
		INSERT OR REPLACE INTO command_output (
			command_id, stdout_blob, stderr_blob, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?)
	`, "cmd-expired", []byte("x"), []byte{}, now-10000, now-1000)
	if err != nil {
		t.Fatalf("seed expired row error = %v", err)
	}

	pruned, err := store.PruneExpiredCommandOutput(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredCommandOutput() error = %v", err)
	}
	if pruned < 1 {
		t.Errorf("expected pruned >= 1, got %d", pruned)
	}
}

func TestSQLiteStore_FinalizeCommandEvent_NotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	err := store.FinalizeCommandEvent(context.Background(), "missing-cmd", 1, time.Now().UnixMilli(), false, 0)
	if err == nil {
		t.Fatal("expected error for missing command event")
	}
	if err != ErrCommandNotFound {
		t.Fatalf("expected ErrCommandNotFound, got %v", err)
	}
}
