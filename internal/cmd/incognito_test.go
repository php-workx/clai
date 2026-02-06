package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestIsIncognito(t *testing.T) {
	tests := []struct {
		name       string
		noRecord   string
		ephemeral  string
		wantResult bool
	}{
		{
			name:       "neither set",
			noRecord:   "",
			ephemeral:  "",
			wantResult: false,
		},
		{
			name:       "no_record set",
			noRecord:   "1",
			ephemeral:  "",
			wantResult: true,
		},
		{
			name:       "ephemeral set",
			noRecord:   "",
			ephemeral:  "1",
			wantResult: true,
		},
		{
			name:       "both set",
			noRecord:   "1",
			ephemeral:  "1",
			wantResult: true,
		},
		{
			name:       "no_record set to 0",
			noRecord:   "0",
			ephemeral:  "",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original values
			origNoRecord := os.Getenv("CLAI_NO_RECORD")
			origEphemeral := os.Getenv("CLAI_EPHEMERAL")
			defer func() {
				if origNoRecord != "" {
					os.Setenv("CLAI_NO_RECORD", origNoRecord)
				} else {
					os.Unsetenv("CLAI_NO_RECORD")
				}
				if origEphemeral != "" {
					os.Setenv("CLAI_EPHEMERAL", origEphemeral)
				} else {
					os.Unsetenv("CLAI_EPHEMERAL")
				}
			}()

			// Set test values
			if tt.noRecord != "" {
				os.Setenv("CLAI_NO_RECORD", tt.noRecord)
			} else {
				os.Unsetenv("CLAI_NO_RECORD")
			}
			if tt.ephemeral != "" {
				os.Setenv("CLAI_EPHEMERAL", tt.ephemeral)
			} else {
				os.Unsetenv("CLAI_EPHEMERAL")
			}

			got := IsIncognito()
			if got != tt.wantResult {
				t.Errorf("IsIncognito() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestIsNoRecord(t *testing.T) {
	tests := []struct {
		name       string
		noRecord   string
		wantResult bool
	}{
		{"not set", "", false},
		{"set to 1", "1", true},
		{"set to 0", "0", false},
		{"set to true", "true", false}, // only "1" is true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Getenv("CLAI_NO_RECORD")
			defer func() {
				if orig != "" {
					os.Setenv("CLAI_NO_RECORD", orig)
				} else {
					os.Unsetenv("CLAI_NO_RECORD")
				}
			}()

			if tt.noRecord != "" {
				os.Setenv("CLAI_NO_RECORD", tt.noRecord)
			} else {
				os.Unsetenv("CLAI_NO_RECORD")
			}

			got := IsNoRecord()
			if got != tt.wantResult {
				t.Errorf("IsNoRecord() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestIsEphemeral(t *testing.T) {
	tests := []struct {
		name       string
		ephemeral  string
		wantResult bool
	}{
		{"not set", "", false},
		{"set to 1", "1", true},
		{"set to 0", "0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := os.Getenv("CLAI_EPHEMERAL")
			defer func() {
				if orig != "" {
					os.Setenv("CLAI_EPHEMERAL", orig)
				} else {
					os.Unsetenv("CLAI_EPHEMERAL")
				}
			}()

			if tt.ephemeral != "" {
				os.Setenv("CLAI_EPHEMERAL", tt.ephemeral)
			} else {
				os.Unsetenv("CLAI_EPHEMERAL")
			}

			got := IsEphemeral()
			if got != tt.wantResult {
				t.Errorf("IsEphemeral() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestEnableIncognito(t *testing.T) {
	t.Run("ephemeral mode (default)", func(t *testing.T) {
		var stdout bytes.Buffer
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := enableIncognito(false)

		w.Close()
		stdout.ReadFrom(r)
		os.Stdout = oldStdout

		if err != nil {
			t.Errorf("enableIncognito(false) returned error: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "export CLAI_EPHEMERAL=1") {
			t.Errorf("expected 'export CLAI_EPHEMERAL=1' in output, got: %s", output)
		}
		if !strings.Contains(output, "unset CLAI_NO_RECORD") {
			t.Errorf("expected 'unset CLAI_NO_RECORD' in output, got: %s", output)
		}
	})

	t.Run("no-send mode", func(t *testing.T) {
		var stdout bytes.Buffer
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := enableIncognito(true)

		w.Close()
		stdout.ReadFrom(r)
		os.Stdout = oldStdout

		if err != nil {
			t.Errorf("enableIncognito(true) returned error: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "export CLAI_NO_RECORD=1") {
			t.Errorf("expected 'export CLAI_NO_RECORD=1' in output, got: %s", output)
		}
		if !strings.Contains(output, "unset CLAI_EPHEMERAL") {
			t.Errorf("expected 'unset CLAI_EPHEMERAL' in output, got: %s", output)
		}
	})
}

func TestDisableIncognito(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	done := make(chan struct{})
	go func() {
		_, _ = stdout.ReadFrom(rOut)
		_, _ = stderr.ReadFrom(rErr)
		close(done)
	}()

	err := disableIncognito()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	<-done

	if err != nil {
		t.Fatalf("disableIncognito returned error: %v", err)
	}

	outStr := stdout.String()
	errStr := stderr.String()
	if !strings.Contains(outStr, "unset CLAI_NO_RECORD") {
		t.Errorf("expected unset CLAI_NO_RECORD in output, got: %s", outStr)
	}
	if !strings.Contains(outStr, "unset CLAI_EPHEMERAL") {
		t.Errorf("expected unset CLAI_EPHEMERAL in output, got: %s", outStr)
	}
	if !strings.Contains(errStr, "disabled") {
		t.Errorf("expected disabled message on stderr, got: %s", errStr)
	}
}
