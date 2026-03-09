package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runger/clai/internal/config"
)

func TestJSONRPC_Ping(t *testing.T) {
	t.Parallel()

	socketPath, cleanup := startJSONRPCTestServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial json-rpc socket: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}` + "\n")); err != nil {
		t.Fatalf("write ping request: %v", err)
	}

	resp := readJSONLine(t, conn, reader)
	if got := resp["jsonrpc"]; got != "2.0" {
		t.Fatalf("jsonrpc = %v, want 2.0", got)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result in ping response: %#v", resp)
	}
	if result["pong"] != true {
		t.Fatalf("pong = %v, want true", result["pong"])
	}
}

func TestJSONRPC_CommandLifecycle_AndSuggestionNotification(t *testing.T) {
	t.Parallel()

	socketPath, cleanup := startJSONRPCTestServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial json-rpc socket: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	writeJSONLine(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "command.start",
		"params": map[string]any{
			"session_id": "sess-1",
			"command_id": "cmd-1",
			"timestamp":  time.Now().UnixMilli(),
		},
	})
	startResp := readJSONLine(t, conn, reader)
	assertOKAck(t, startResp)

	writeJSONLine(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "output.chunk",
		"params": map[string]any{
			"command_id":  "cmd-1",
			"data_base64": "aGVsbG8=", // "hello"
			"is_stderr":   false,
		},
	})
	outputResp := readJSONLine(t, conn, reader)
	assertOKAck(t, outputResp)

	writeJSONLine(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "command.end",
		"params": map[string]any{
			"command_id": "cmd-1",
			"exit_code":  1,
			"timestamp":  time.Now().UnixMilli(),
		},
	})
	endResp := readJSONLine(t, conn, reader)
	assertOKAck(t, endResp)

	notification := readJSONLine(t, conn, reader)
	if notification["method"] != "suggestion.available" {
		t.Fatalf("expected suggestion.available notification, got %#v", notification)
	}
	params, ok := notification["params"].(map[string]any)
	if !ok {
		t.Fatalf("notification missing params: %#v", notification)
	}
	if params["command_id"] != "cmd-1" {
		t.Fatalf("notification command_id = %v, want cmd-1", params["command_id"])
	}
}

func startJSONRPCTestServer(t *testing.T) (socketPath string, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("/tmp", "clai-jsonrpc-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	paths := &config.Paths{BaseDir: tmpDir}

	store := newMockStore()
	server, err := NewServer(&ServerConfig{
		Store:       store,
		Paths:       paths,
		IdleTimeout: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start(ctx)
	}()

	socketPath = paths.JSONRPCSocketFile()
	waitForPath(t, socketPath)

	cleanup = func() {
		cancel()
		server.Shutdown()
		_ = os.RemoveAll(tmpDir)
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("server returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for server shutdown")
		}
	}

	return socketPath, cleanup
}

func waitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for path: %s", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func writeJSONLine(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json request: %v", err)
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("write json request: %v", err)
	}
}

func readJSONLine(t *testing.T, conn net.Conn, reader *bufio.Reader) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read json line: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		t.Fatalf("unmarshal json line %q: %v", string(line), err)
	}
	return obj
}

func assertOKAck(t *testing.T, response map[string]any) {
	t.Helper()
	if response["error"] != nil {
		t.Fatalf("unexpected error response: %#v", response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object: %#v", response)
	}
	if result["ok"] != true {
		t.Fatalf("result.ok = %v, want true", result["ok"])
	}
}

func TestJSONRPC_MethodNotFound(t *testing.T) {
	t.Parallel()

	socketPath, cleanup := startJSONRPCTestServer(t)
	defer cleanup()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial json-rpc socket: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)

	writeJSONLine(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "not.a.method",
		"params":  map[string]any{},
	})

	resp := readJSONLine(t, conn, reader)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got %#v", resp)
	}
	if int(errObj["code"].(float64)) != jsonRPCMethodMissing {
		t.Fatalf("error code = %v, want %d", errObj["code"], jsonRPCMethodMissing)
	}
}

func TestJSONRPCTestServer_UsesIsolatedSockets(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	paths := &config.Paths{BaseDir: tmpDir}
	expected := filepath.Join(tmpDir, "daemon.sock")
	if got := paths.JSONRPCSocketFile(); got != expected {
		t.Fatalf("json-rpc socket path = %s, want %s", got, expected)
	}
}
