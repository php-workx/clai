package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_NoArgs(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run(nil, &out, &errOut)
	if code != 1 {
		t.Fatalf("run(nil) code = %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Usage: clai-hook") {
		t.Fatalf("stderr missing usage, got %q", errOut.String())
	}
}

func TestRun_Version(t *testing.T) {
	oldVersion, oldCommit, oldBuild := Version, GitCommit, BuildDate
	Version = "1.2.3"
	GitCommit = "abc123"
	BuildDate = "2026-02-11"
	defer func() {
		Version = oldVersion
		GitCommit = oldCommit
		BuildDate = oldBuild
	}()

	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"version"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run(version) code = %d, want 0", code)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr should be empty, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "clai-hook 1.2.3") {
		t.Fatalf("stdout missing version, got %q", out.String())
	}
}

func TestRun_Help(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"help"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run(help) code = %d, want 0", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "Commands:") {
		t.Fatalf("stderr missing help content, got %q", errOut.String())
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"wat"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("run(unknown) code = %d, want 1", code)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "unknown command: wat") {
		t.Fatalf("stderr missing unknown command message, got %q", errOut.String())
	}
}

func TestRun_SessionStart(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := run([]string{"session-start"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run(session-start) code = %d, want 0", code)
	}
}
