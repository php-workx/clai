package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

type historyGlobals struct {
	cwd     string
	session string
	status  string
	format  string
	limit   int
	global  bool
}

type suggestGlobals struct {
	limit int
	json  bool
}

func withHistoryGlobals(t *testing.T, g historyGlobals) {
	t.Helper()
	old := historyGlobals{
		limit:   historyLimit,
		cwd:     historyCWD,
		session: historySession,
		global:  historyGlobal,
		status:  historyStatus,
		format:  historyFormat,
	}
	historyLimit = g.limit
	historyCWD = g.cwd
	historySession = g.session
	historyGlobal = g.global
	historyStatus = g.status
	historyFormat = g.format

	t.Cleanup(func() {
		historyLimit = old.limit
		historyCWD = old.cwd
		historySession = old.session
		historyGlobal = old.global
		historyStatus = old.status
		historyFormat = old.format
	})
}

func withSuggestGlobals(t *testing.T, g suggestGlobals) {
	t.Helper()
	old := suggestGlobals{limit: suggestLimit, json: suggestJSON}
	suggestLimit = g.limit
	suggestJSON = g.json
	t.Cleanup(func() {
		suggestLimit = old.limit
		suggestJSON = old.json
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = old
	out := <-outC
	_ = r.Close()
	return out
}
