package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runger/clai/internal/config"
	"github.com/runger/clai/internal/daemon"
	"github.com/runger/clai/internal/suggestions/db"
	"github.com/runger/clai/internal/suggestions/metrics"
)

var suggestDoctorJSON bool

const (
	statusDBUnavailable = "error: DB unavailable"
	statusErrorFmt      = "error: %v"
)

var suggestDoctorCmd = &cobra.Command{
	Use:    "suggest-doctor",
	Short:  "Diagnose the suggestions engine health",
	Hidden: true,
	Long: `Run diagnostic checks on the suggestions engine.

This command reports:
  - Daemon health (running, socket path, PID)
  - Schema version
  - Cache stats (hit rate, counters)
  - FTS5 availability
  - Playbook status (.clai/tasks.yaml)
  - Active V2 features
  - Shell matrix
  - Template count
  - Event count
  - Feedback stats

Examples:
  clai suggest-doctor
  clai suggest-doctor --json`,
	RunE: runSuggestDoctor,
}

func init() {
	suggestDoctorCmd.Flags().BoolVar(&suggestDoctorJSON, "json", false, "output diagnostics as JSON")
	suggestDoctorCmd.Flags().StringVar(&colorMode, "color", "auto", "color output: auto, always, or never")
}

// suggestDoctorReport holds all diagnostic sections for JSON output.
type suggestDoctorReport struct {
	ActiveFeatures  activeFeatureInfo `json:"active_features"`
	Metrics         map[string]int64  `json:"metrics"`
	SchemaVersion   schemaVersionInfo `json:"schema_version"`
	FTSAvailability ftsInfo           `json:"fts_availability"`
	TemplateCount   countInfo         `json:"template_count"`
	EventCount      countInfo         `json:"event_count"`
	ShellMatrix     shellMatrixInfo   `json:"shell_matrix"`
	DaemonHealth    daemonHealthInfo  `json:"daemon_health"`
	PlaybookStatus  playbookInfo      `json:"playbook_status"`
	CacheStats      cacheStatsInfo    `json:"cache_stats"`
	FeedbackStats   feedbackStatsInfo `json:"feedback_stats"`
}

type daemonHealthInfo struct {
	SocketPath string `json:"socket_path"`
	Status     string `json:"status"`
	PID        int    `json:"pid"`
	Running    bool   `json:"running"`
}

type schemaVersionInfo struct {
	Status  string `json:"status"`
	Version int    `json:"version"`
}

type cacheStatsInfo struct {
	Status     string  `json:"status"`
	HitRate    float64 `json:"hit_rate"`
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	EntryCount int64   `json:"entry_count"`
}

type ftsInfo struct {
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}

type playbookInfo struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	TaskCount int    `json:"task_count"`
	Present   bool   `json:"present"`
}

type activeFeatureInfo struct {
	Features map[string]bool `json:"features"`
}

type shellMatrixInfo struct {
	Shells []string `json:"shells"`
}

type countInfo struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type feedbackStatsInfo struct {
	Status    string `json:"status"`
	Accepted  int64  `json:"accepted"`
	Dismissed int64  `json:"dismissed"`
	Edited    int64  `json:"edited"`
	Total     int64  `json:"total"`
}

func runSuggestDoctor(cmd *cobra.Command, args []string) error {
	applyColorMode()

	report := buildSuggestDoctorReport()

	if suggestDoctorJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(report)
	}

	printSuggestDoctorText(&report)
	return nil
}

func buildSuggestDoctorReport() suggestDoctorReport {
	paths := config.DefaultPaths()
	cfg, _ := config.Load()
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	report := suggestDoctorReport{
		DaemonHealth:    checkDaemonHealth(paths),
		FTSAvailability: checkFTSAvailability(),
		ActiveFeatures:  checkActiveFeatures(cfg),
		ShellMatrix:     checkShellMatrix(),
		Metrics:         metrics.Global.Snapshot(),
	}

	// Open DB read-only for stat checks
	sdb := openSuggestionsDBReadOnly()
	if sdb != nil {
		defer sdb.Close()
		ctx := context.Background()

		report.SchemaVersion = checkSchemaVersion(ctx, sdb)
		report.CacheStats = checkCacheStats(ctx, sdb)
		report.TemplateCount = checkTemplateCount(ctx, sdb)
		report.EventCount = checkEventCount(ctx, sdb)
		report.FeedbackStats = checkFeedbackStats(ctx, sdb)
	} else {
		report.SchemaVersion = schemaVersionInfo{Status: statusDBUnavailable}
		report.CacheStats = cacheStatsInfo{Status: statusDBUnavailable}
		report.TemplateCount = countInfo{Status: statusDBUnavailable}
		report.EventCount = countInfo{Status: statusDBUnavailable}
		report.FeedbackStats = feedbackStatsInfo{Status: statusDBUnavailable}
	}

	report.PlaybookStatus = checkPlaybookStatus(cfg)

	// Merge in-memory metrics for cache stats
	report.CacheStats.HitRate = metrics.Global.CacheHitRate()
	report.CacheStats.Hits = metrics.Global.CacheHits.Load()
	report.CacheStats.Misses = metrics.Global.CacheMisses.Load()

	return report
}

func checkDaemonHealth(paths *config.Paths) daemonHealthInfo {
	info := daemonHealthInfo{
		SocketPath: paths.SocketFile(),
	}

	if daemon.IsRunningWithPaths(paths) {
		info.Running = true
		info.Status = "ok"
		pid, err := daemon.ReadPID(paths.PIDFile())
		if err == nil {
			info.PID = pid
		}
	} else {
		info.Running = false
		info.Status = "not running"
	}

	return info
}

func checkSchemaVersion(ctx context.Context, sdb *sql.DB) schemaVersionInfo {
	version, err := db.GetSchemaVersion(ctx, sdb)
	if err != nil {
		return schemaVersionInfo{Status: fmt.Sprintf(statusErrorFmt, err)}
	}
	status := "ok"
	if version == 0 {
		status = "no migrations applied"
	} else if version < db.SchemaVersion {
		status = fmt.Sprintf("outdated (current: %d, latest: %d)", version, db.SchemaVersion)
	}
	return schemaVersionInfo{Version: version, Status: status}
}

func checkCacheStats(ctx context.Context, sdb *sql.DB) cacheStatsInfo {
	var count int64
	err := sdb.QueryRowContext(ctx, "SELECT COUNT(*) FROM suggestion_cache").Scan(&count)
	if err != nil {
		return cacheStatsInfo{Status: fmt.Sprintf(statusErrorFmt, err)}
	}
	return cacheStatsInfo{
		EntryCount: count,
		Status:     "ok",
	}
}

func checkFTSAvailability() ftsInfo {
	// Test FTS5 by opening a temporary in-memory DB and trying to create an FTS5 table
	tmpDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return ftsInfo{Enabled: false, Status: fmt.Sprintf(statusErrorFmt, err)}
	}
	defer tmpDB.Close()

	_, err = tmpDB.Exec("CREATE VIRTUAL TABLE fts_test USING fts5(content)")
	if err != nil {
		return ftsInfo{Enabled: false, Status: "FTS5 not available"}
	}
	return ftsInfo{Enabled: true, Status: "ok"}
}

func checkPlaybookStatus(cfg *config.Config) playbookInfo {
	playbookPath := cfg.Suggestions.TaskPlaybookPath
	if playbookPath == "" {
		playbookPath = ".clai/tasks.yaml"
	}

	// Try to resolve relative to current directory
	absPath := playbookPath
	if !filepath.IsAbs(playbookPath) {
		cwd, err := os.Getwd()
		if err == nil {
			absPath = filepath.Join(cwd, playbookPath)
		}
	}

	info := playbookInfo{
		Path: absPath,
	}

	data, err := os.ReadFile(absPath) //nolint:gosec // G304: playbook path from trusted config
	if err != nil {
		if os.IsNotExist(err) {
			info.Status = "not found"
			return info
		}
		info.Status = fmt.Sprintf(statusErrorFmt, err)
		return info
	}

	info.Present = true
	// Count tasks by counting lines that look like task entries (name: keys)
	// Simple heuristic: count non-empty, non-comment lines that start with "- name:"
	taskCount := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "name:") {
			taskCount++
		}
	}
	info.TaskCount = taskCount
	info.Status = "ok"

	return info
}

func checkActiveFeatures(cfg *config.Config) activeFeatureInfo {
	s := cfg.Suggestions
	features := map[string]bool{
		"suggestions":             s.Enabled,
		"search_fts":              s.SearchFTSEnabled,
		"search_describe":         s.SearchDescribeEnabled,
		"project_type_detection":  s.ProjectTypeDetectionEnabled,
		"pipeline_awareness":      s.PipelineAwarenessEnabled,
		"failure_recovery":        s.FailureRecoveryEnabled,
		"workflow_detection":      s.WorkflowDetectionEnabled,
		"adaptive_timing":         s.AdaptiveTimingEnabled,
		"alias_resolution":        s.AliasResolutionEnabled,
		"directory_scoping":       s.DirectoryScopingEnabled,
		"explainability":          s.ExplainEnabled,
		"online_learning":         s.OnlineLearningEnabled,
		"task_playbook":           s.TaskPlaybookEnabled,
		"task_playbook_extended":  s.TaskPlaybookExtendedEnabled,
		"discovery":               s.DiscoveryEnabled,
		"redact_sensitive_tokens": s.RedactSensitiveTokens,
	}
	return activeFeatureInfo{Features: features}
}

func checkShellMatrix() shellMatrixInfo {
	shells := checkShellIntegration()
	if len(shells) == 0 {
		shells = []string{"none detected"}
	}
	return shellMatrixInfo{Shells: shells}
}

func checkTemplateCount(ctx context.Context, sdb *sql.DB) countInfo {
	var count int64
	err := sdb.QueryRowContext(ctx, "SELECT COUNT(*) FROM command_template").Scan(&count)
	if err != nil {
		return countInfo{Status: fmt.Sprintf(statusErrorFmt, err)}
	}
	return countInfo{Count: count, Status: "ok"}
}

func checkEventCount(ctx context.Context, sdb *sql.DB) countInfo {
	var count int64
	err := sdb.QueryRowContext(ctx, "SELECT COUNT(*) FROM command_event").Scan(&count)
	if err != nil {
		return countInfo{Status: fmt.Sprintf(statusErrorFmt, err)}
	}
	return countInfo{Count: count, Status: "ok"}
}

func checkFeedbackStats(ctx context.Context, sdb *sql.DB) feedbackStatsInfo {
	info := feedbackStatsInfo{}

	rows, err := sdb.QueryContext(ctx,
		"SELECT action, COUNT(*) FROM suggestion_feedback GROUP BY action")
	if err != nil {
		info.Status = fmt.Sprintf(statusErrorFmt, err)
		return info
	}
	defer rows.Close()

	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			continue
		}
		switch action {
		case "accepted":
			info.Accepted = count
		case "dismissed":
			info.Dismissed = count
		case "edited":
			info.Edited = count
		}
		info.Total += count
	}

	info.Status = "ok"
	return info
}

func printSuggestDoctorText(report *suggestDoctorReport) {
	fmt.Printf("%sclai Suggestions Doctor%s\n", colorBold, colorReset)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Println()

	// Daemon health
	printSection("Daemon Health")
	printField("Running", boolStatus(report.DaemonHealth.Running))
	printField("Socket", report.DaemonHealth.SocketPath)
	if report.DaemonHealth.PID > 0 {
		printField("PID", fmt.Sprintf("%d", report.DaemonHealth.PID))
	}
	fmt.Println()

	// Schema version
	printSection("Schema")
	printField("Version", fmt.Sprintf("%d", report.SchemaVersion.Version))
	printField("Status", report.SchemaVersion.Status)
	fmt.Println()

	// Cache stats
	printSection("Cache Stats")
	printField("Hit Rate", fmt.Sprintf("%.1f%%", report.CacheStats.HitRate*100))
	printField("Hits", fmt.Sprintf("%d", report.CacheStats.Hits))
	printField("Misses", fmt.Sprintf("%d", report.CacheStats.Misses))
	printField("DB Entries", fmt.Sprintf("%d", report.CacheStats.EntryCount))
	fmt.Println()

	// FTS availability
	printSection("FTS5")
	printField("Available", boolStatus(report.FTSAvailability.Enabled))
	fmt.Println()

	// Playbook status
	printSection("Playbook")
	printField("Present", boolStatus(report.PlaybookStatus.Present))
	printField("Path", report.PlaybookStatus.Path)
	if report.PlaybookStatus.Present {
		printField("Tasks", fmt.Sprintf("%d", report.PlaybookStatus.TaskCount))
	}
	fmt.Println()

	// Active features
	printSection("Active Features")
	for name, enabled := range report.ActiveFeatures.Features {
		printField(name, boolStatus(enabled))
	}
	fmt.Println()

	// Shell matrix
	printSection("Shell Matrix")
	printField("Configured", strings.Join(report.ShellMatrix.Shells, ", "))
	fmt.Println()

	// Template count
	printSection("Templates")
	printField("Count", fmt.Sprintf("%d", report.TemplateCount.Count))
	fmt.Println()

	// Event count
	printSection("Events")
	printField("Count", fmt.Sprintf("%d", report.EventCount.Count))
	fmt.Println()

	// Feedback stats
	printSection("Feedback")
	printField("Accepted", fmt.Sprintf("%d", report.FeedbackStats.Accepted))
	printField("Dismissed", fmt.Sprintf("%d", report.FeedbackStats.Dismissed))
	printField("Edited", fmt.Sprintf("%d", report.FeedbackStats.Edited))
	printField("Total", fmt.Sprintf("%d", report.FeedbackStats.Total))
	fmt.Println()

	// Metrics counters
	printSection("Runtime Metrics")
	snap := report.Metrics
	printField("Suggest Requests", fmt.Sprintf("%d", snap["suggest_requests"]))
	printField("Suggest Hits", fmt.Sprintf("%d", snap["suggest_hits"]))
	printField("Suggest Misses", fmt.Sprintf("%d", snap["suggest_misses"]))
	printField("Ingest Commands", fmt.Sprintf("%d", snap["ingest_commands"]))
	printField("Ingest Errors", fmt.Sprintf("%d", snap["ingest_errors"]))
	avgLatency := float64(0)
	if snap["suggest_requests"] > 0 {
		avgLatency = float64(snap["latency_sum_ms"]) / float64(snap["suggest_requests"])
	}
	printField("Avg Latency", fmt.Sprintf("%.1fms", avgLatency))
	fmt.Println()
}

func printSection(title string) {
	fmt.Printf("  %s%s%s\n", colorCyan, title, colorReset)
}

func printField(name, value string) {
	fmt.Printf("    %-24s %s\n", name+":", value)
}

func boolStatus(b bool) string {
	if b {
		return colorGreen + "yes" + colorReset
	}
	return colorYellow + "no" + colorReset
}

// openSuggestionsDBReadOnly opens the V2 suggestions database in read-only mode
// for diagnostic queries. Returns nil if the DB cannot be opened.
func openSuggestionsDBReadOnly() *sql.DB {
	dbPath, err := db.DefaultDBPath()
	if err != nil {
		return nil
	}

	if _, statErr := os.Stat(dbPath); os.IsNotExist(statErr) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sdb, err := db.Open(ctx, db.Options{
		Path:     dbPath,
		ReadOnly: true,
		SkipLock: true,
	})
	if err != nil {
		return nil
	}

	return sdb.DB()
}
