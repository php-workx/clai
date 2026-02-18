package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/suggestions/metrics"
)

func TestSuggestDoctorCmdRegistration(t *testing.T) {
	if suggestDoctorCmd == nil {
		t.Fatal("suggestDoctorCmd should not be nil")
	}

	if suggestDoctorCmd.Use != "suggest-doctor" {
		t.Errorf("suggestDoctorCmd.Use = %q, want %q", suggestDoctorCmd.Use, "suggest-doctor")
	}

	if suggestDoctorCmd.Short == "" {
		t.Error("suggestDoctorCmd.Short should not be empty")
	}

	if suggestDoctorCmd.RunE == nil {
		t.Error("suggestDoctorCmd.RunE should not be nil")
	}
}

func TestBuildSuggestDoctorReport(t *testing.T) {
	// Reset metrics for a clean test
	metrics.Global.Reset()

	report := buildSuggestDoctorReport()

	// Daemon health should have a socket path
	if report.DaemonHealth.SocketPath == "" {
		t.Error("DaemonHealth.SocketPath should not be empty")
	}

	// FTS availability should report a status
	if report.FTSAvailability.Status == "" {
		t.Error("FTSAvailability.Status should not be empty")
	}

	// Active features should have entries
	if len(report.ActiveFeatures.Features) == 0 {
		t.Error("ActiveFeatures.Features should not be empty")
	}

	// Shell matrix should have at least one entry
	if len(report.ShellMatrix.Shells) == 0 {
		t.Error("ShellMatrix.Shells should not be empty")
	}

	// Metrics should have all expected keys
	expectedMetricsKeys := []string{
		"suggest_requests",
		"suggest_hits",
		"suggest_misses",
		"cache_hits",
		"cache_misses",
		"feedback_accepted",
		"feedback_dismissed",
		"feedback_edited",
		"ingest_commands",
		"ingest_errors",
		"latency_sum_ms",
	}
	for _, key := range expectedMetricsKeys {
		if _, ok := report.Metrics[key]; !ok {
			t.Errorf("Metrics missing key %q", key)
		}
	}
}

func TestBuildSuggestDoctorReport_ActiveFeatures(t *testing.T) {
	report := buildSuggestDoctorReport()

	expectedFeatures := []string{
		"suggestions",
		"search_fts",
		"search_describe",
		"project_type_detection",
		"pipeline_awareness",
		"failure_recovery",
		"workflow_detection",
		"adaptive_timing",
		"alias_resolution",
		"directory_scoping",
		"explainability",
		"online_learning",
		"task_playbook",
		"task_playbook_extended",
		"discovery",
		"redact_sensitive_tokens",
	}

	for _, feat := range expectedFeatures {
		if _, ok := report.ActiveFeatures.Features[feat]; !ok {
			t.Errorf("ActiveFeatures missing feature %q", feat)
		}
	}
}

func TestCheckFTSAvailability(t *testing.T) {
	info := checkFTSAvailability()

	// modernc.org/sqlite supports FTS5
	if !info.Enabled {
		t.Log("FTS5 not available - this may be expected on some build configurations")
	}

	if info.Status == "" {
		t.Error("FTS status should not be empty")
	}
}

func TestCheckShellMatrix(t *testing.T) {
	info := checkShellMatrix()

	if len(info.Shells) == 0 {
		t.Error("Shell matrix should have at least one entry")
	}
}

func TestBoolStatus(t *testing.T) {
	// Disable colors for deterministic testing
	disableColors()
	defer enableColors()

	yes := boolStatus(true)
	if yes != "yes" {
		t.Errorf("boolStatus(true) = %q, want %q", yes, "yes")
	}

	no := boolStatus(false)
	if no != "no" {
		t.Errorf("boolStatus(false) = %q, want %q", no, "no")
	}
}

func TestRunSuggestDoctorText(t *testing.T) {
	// Disable colors for deterministic output
	disableColors()
	defer enableColors()

	// Capture stdout
	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdout = w

	// Run with text output
	suggestDoctorJSON = false
	_ = runSuggestDoctor(suggestDoctorCmd, []string{})

	w.Close()
	outBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	output := string(outBytes)

	// Verify required sections are present
	sections := []string{
		"Suggestions Doctor",
		"Daemon Health",
		"Schema",
		"Cache Stats",
		"FTS5",
		"Playbook",
		"Active Features",
		"Shell Matrix",
		"Templates",
		"Events",
		"Feedback",
		"Runtime Metrics",
	}

	for _, section := range sections {
		if !strings.Contains(output, section) {
			t.Errorf("output missing section %q", section)
		}
	}
}

func TestRunSuggestDoctorJSON(t *testing.T) {
	// Capture stdout
	origStdout := os.Stdout
	defer func() { os.Stdout = origStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}

	os.Stdout = w

	// Run with JSON output
	suggestDoctorJSON = true
	defer func() { suggestDoctorJSON = false }()

	_ = runSuggestDoctor(suggestDoctorCmd, []string{})

	w.Close()
	outBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	// Verify valid JSON
	var report suggestDoctorReport
	if err := json.Unmarshal(outBytes, &report); err != nil {
		t.Fatalf("JSON output is not valid: %v\nOutput: %s", err, string(outBytes))
	}

	// Verify key fields are present
	if report.DaemonHealth.SocketPath == "" {
		t.Error("JSON report missing daemon_health.socket_path")
	}
	if report.FTSAvailability.Status == "" {
		t.Error("JSON report missing fts_availability.status")
	}
	if len(report.ActiveFeatures.Features) == 0 {
		t.Error("JSON report missing active_features")
	}
	if len(report.Metrics) == 0 {
		t.Error("JSON report missing metrics")
	}
}

func TestCheckPlaybookStatus_NotFound(t *testing.T) {
	// Use a temp directory as working directory so .clai/tasks.yaml won't exist
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	cfg := defaultConfigForTest()
	info := checkPlaybookStatus(cfg)

	if info.Present {
		t.Error("playbook should not be present in temp directory")
	}
	if info.Status != "not found" {
		t.Errorf("status = %q, want %q", info.Status, "not found")
	}
}

func TestCheckPlaybookStatus_Present(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Create .clai/tasks.yaml
	os.MkdirAll(tmpDir+"/.clai", 0755)
	content := `- name: build
  command: make build
- name: test
  command: make test
- name: lint
  command: make lint
`
	os.WriteFile(tmpDir+"/.clai/tasks.yaml", []byte(content), 0644)

	cfg := defaultConfigForTest()
	info := checkPlaybookStatus(cfg)

	if !info.Present {
		t.Error("playbook should be present")
	}
	if info.TaskCount != 3 {
		t.Errorf("TaskCount = %d, want 3", info.TaskCount)
	}
	if info.Status != "ok" {
		t.Errorf("status = %q, want %q", info.Status, "ok")
	}
}

func TestCheckActiveFeatures_DefaultConfig(t *testing.T) {
	cfg := defaultConfigForTest()
	info := checkActiveFeatures(cfg)

	// Default config should have suggestions enabled
	if !info.Features["suggestions"] {
		t.Error("suggestions should be enabled by default")
	}
	if !info.Features["search_fts"] {
		t.Error("search_fts should be enabled by default")
	}
	if !info.Features["workflow_detection"] {
		t.Error("workflow_detection should be enabled by default")
	}
}

func defaultConfigForTest() *config.Config {
	return config.DefaultConfig()
}
