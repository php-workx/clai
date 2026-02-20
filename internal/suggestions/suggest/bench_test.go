package suggest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/runger/clai/internal/suggestions/event"
	"github.com/runger/clai/internal/suggestions/ingest"
	"github.com/runger/clai/internal/suggestions/normalize"
	"github.com/runger/clai/internal/suggestions/score"
	"github.com/runger/clai/internal/suggestions/search"

	_ "modernc.org/sqlite"
)

// createBenchDB creates a temporary SQLite database for benchmarks.
// It uses the V2 schema tables that the scorer and write path depend on.
func createBenchDB(b *testing.B) *sql.DB {
	b.Helper()

	dir, err := os.MkdirTemp("", "clai-bench-*")
	require.NoError(b, err)
	b.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "bench.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(b, err)
	b.Cleanup(func() { db.Close() })

	// Create V2 schema tables needed by scorer and write path
	_, err = db.Exec(`
		CREATE TABLE session (
			id              TEXT PRIMARY KEY,
			shell           TEXT NOT NULL,
			started_at_ms   INTEGER NOT NULL,
			project_types   TEXT,
			host            TEXT,
			user_name       TEXT
		);

		CREATE TABLE command_event (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			ts_ms           INTEGER NOT NULL,
			cwd             TEXT NOT NULL,
			repo_key        TEXT,
			branch          TEXT,
			cmd_raw         TEXT NOT NULL,
			cmd_norm        TEXT NOT NULL,
			cmd_truncated   INTEGER NOT NULL DEFAULT 0,
			template_id     TEXT,
			exit_code       INTEGER,
			duration_ms     INTEGER,
			ephemeral       INTEGER NOT NULL DEFAULT 0
		);

		CREATE INDEX idx_event_ts ON command_event(ts_ms);
		CREATE INDEX idx_event_session_ts ON command_event(session_id, ts_ms);

		CREATE TABLE command_template (
			template_id     TEXT PRIMARY KEY,
			cmd_norm        TEXT NOT NULL,
			tags            TEXT,
			slot_count      INTEGER NOT NULL,
			first_seen_ms   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL
		);

		CREATE TABLE transition_stat (
			scope             TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_template_id, next_template_id)
		);

		CREATE TABLE command_stat (
			scope           TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			score           REAL NOT NULL,
			success_count   INTEGER NOT NULL,
			failure_count   INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id)
		);

		CREATE INDEX idx_command_stat_scope ON command_stat(scope, score DESC);

		CREATE TABLE slot_stat (
			scope           TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			slot_index      INTEGER NOT NULL,
			value           TEXT NOT NULL,
			weight          REAL NOT NULL,
			count           INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id, slot_index, value)
		);

		CREATE TABLE slot_correlation (
			scope             TEXT NOT NULL,
			template_id       TEXT NOT NULL,
			slot_key          TEXT NOT NULL,
			tuple_hash        TEXT NOT NULL,
			tuple_value_json  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, template_id, slot_key, tuple_hash)
		);

		CREATE TABLE project_type_stat (
			project_type    TEXT NOT NULL,
			template_id     TEXT NOT NULL,
			score           REAL NOT NULL,
			count           INTEGER NOT NULL,
			last_seen_ms    INTEGER NOT NULL,
			PRIMARY KEY(project_type, template_id)
		);

		CREATE TABLE project_type_transition (
			project_type      TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(project_type, prev_template_id, next_template_id)
		);

		CREATE TABLE pipeline_event (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			command_event_id  INTEGER NOT NULL,
			position          INTEGER NOT NULL,
			operator          TEXT,
			cmd_raw           TEXT NOT NULL,
			cmd_norm          TEXT NOT NULL,
			template_id       TEXT NOT NULL,
			UNIQUE(command_event_id, position)
		);

		CREATE TABLE pipeline_transition (
			scope             TEXT NOT NULL,
			prev_template_id  TEXT NOT NULL,
			next_template_id  TEXT NOT NULL,
			operator          TEXT NOT NULL,
			weight            REAL NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_template_id, next_template_id, operator)
		);

		CREATE TABLE pipeline_pattern (
			pattern_hash      TEXT PRIMARY KEY,
			template_chain    TEXT NOT NULL,
			operator_chain    TEXT NOT NULL,
			scope             TEXT NOT NULL,
			count             INTEGER NOT NULL,
			last_seen_ms      INTEGER NOT NULL,
			cmd_norm_display  TEXT NOT NULL
		);

		CREATE TABLE failure_recovery (
			scope                 TEXT NOT NULL,
			failed_template_id    TEXT NOT NULL,
			exit_code_class       TEXT NOT NULL,
			recovery_template_id  TEXT NOT NULL,
			weight                REAL NOT NULL,
			count                 INTEGER NOT NULL,
			success_rate          REAL NOT NULL,
			last_seen_ms          INTEGER NOT NULL,
			source                TEXT NOT NULL DEFAULT 'learned',
			PRIMARY KEY(scope, failed_template_id, exit_code_class, recovery_template_id)
		);

		CREATE TABLE command_score (
			scope         TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			score         REAL NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, cmd_norm)
		);

		CREATE TABLE transition (
			scope         TEXT NOT NULL,
			prev_norm     TEXT NOT NULL,
			next_norm     TEXT NOT NULL,
			count         INTEGER NOT NULL,
			last_ts       INTEGER NOT NULL,
			PRIMARY KEY(scope, prev_norm, next_norm)
		);

		CREATE INDEX idx_transition_prev ON transition(scope, prev_norm);

		CREATE TABLE project_task (
			repo_key      TEXT NOT NULL,
			kind          TEXT NOT NULL,
			name          TEXT NOT NULL,
			command       TEXT NOT NULL,
			description   TEXT,
			discovered_ts INTEGER NOT NULL,
			PRIMARY KEY(repo_key, kind, name)
		);

		CREATE TABLE dismissal_pattern (
			scope                   TEXT NOT NULL,
			context_template_id     TEXT NOT NULL,
			dismissed_template_id   TEXT NOT NULL,
			dismissal_count         INTEGER NOT NULL,
			last_dismissed_ms       INTEGER NOT NULL,
			suppression_level       TEXT NOT NULL,
			PRIMARY KEY(scope, context_template_id, dismissed_template_id)
		);

		CREATE TABLE suggestion_feedback (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id      TEXT NOT NULL,
			ts_ms           INTEGER NOT NULL,
			prompt_prefix   TEXT,
			suggested_text  TEXT NOT NULL,
			action          TEXT NOT NULL,
			executed_text   TEXT,
			latency_ms      INTEGER
		);

		CREATE TABLE schema_migrations (
			version     INTEGER PRIMARY KEY,
			applied_ms  INTEGER NOT NULL
		);
	`)
	require.NoError(b, err)

	return db
}

// populateCommands populates the database with n commands via the scorer's frequency store.
func populateCommands(b *testing.B, _ *sql.DB, freqStore *score.FrequencyStore, transStore *score.TransitionStore, n int) {
	b.Helper()

	ctx := context.Background()
	nowMs := time.Now().UnixMilli()

	// Generate realistic command set
	commands := []string{
		"git status", "git commit -m <msg>", "git push", "git pull",
		"git add .", "git diff", "git log", "git checkout <arg>",
		"npm install", "npm test", "npm run build", "npm run dev",
		"make build", "make test", "make lint", "make clean",
		"docker build -t <arg> .", "docker run <arg>", "docker ps",
		"kubectl get pods", "kubectl apply -f <path>",
		"curl <url>", "ls -la", "cd <path>", "cat <path>",
		"go test ./...", "go build", "go run <path>",
		"python3 <path>", "pytest <path>",
	}

	for i := 0; i < n; i++ {
		cmd := commands[i%len(commands)]
		require.NoError(b, freqStore.Update(ctx, score.ScopeGlobal, cmd, nowMs+int64(i)))

		if i > 0 {
			prev := commands[(i-1)%len(commands)]
			require.NoError(b, transStore.RecordTransition(ctx, score.ScopeGlobal, prev, cmd, nowMs+int64(i)))
		}
	}
}

// BenchmarkSuggest_10Commands benchmarks suggestion with 10 command history.
func BenchmarkSuggest_10Commands(b *testing.B) {
	benchmarkSuggestWithN(b, 10)
}

// BenchmarkSuggest_100Commands benchmarks suggestion with 100 command history.
func BenchmarkSuggest_100Commands(b *testing.B) {
	benchmarkSuggestWithN(b, 100)
}

// BenchmarkSuggest_1000Commands benchmarks suggestion with 1000 command history.
func BenchmarkSuggest_1000Commands(b *testing.B) {
	benchmarkSuggestWithN(b, 1000)
}

// BenchmarkSuggest_10000Commands benchmarks suggestion with 10000 command history.
// Per spec: suggest latency < 50ms for 10K command history.
func BenchmarkSuggest_10000Commands(b *testing.B) {
	benchmarkSuggestWithN(b, 10000)
}

func benchmarkSuggestWithN(b *testing.B, n int) {
	db := createBenchDB(b)

	freqStore, err := score.NewFrequencyStore(db, score.DefaultFrequencyOptions())
	require.NoError(b, err)
	b.Cleanup(func() { freqStore.Close() })

	transStore, err := score.NewTransitionStore(db)
	require.NoError(b, err)
	b.Cleanup(func() { transStore.Close() })

	populateCommands(b, db, freqStore, transStore, n)

	scorer, err := NewScorer(&ScorerDependencies{
		DB:              db,
		FreqStore:       freqStore,
		TransitionStore: transStore,
	}, DefaultScorerConfig())
	require.NoError(b, err)

	ctx := context.Background()
	suggestCtx := &SuggestContext{
		LastCmd: "git status",
		NowMs:   time.Now().UnixMilli(),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := scorer.Suggest(ctx, suggestCtx)
		if err != nil {
			b.Fatalf("Suggest error: %v", err)
		}
	}

	b.StopTimer()
	avg := time.Duration(int64(b.Elapsed()) / int64(b.N))
	b.ReportMetric(float64(avg.Microseconds()), "us/op")

	// Target: suggest latency < 50ms for 10K command history
	if n <= 10000 && avg > 50*time.Millisecond {
		b.Logf("WARNING: average suggest latency %v exceeds 50ms target for %d commands", avg, n)
	}
}

// BenchmarkWritePath benchmarks the full write path transaction.
func BenchmarkWritePath(b *testing.B) {
	db := createBenchDB(b)
	ctx := context.Background()

	// Insert a session for foreign key reference
	_, err := db.Exec(`INSERT INTO session (id, shell, started_at_ms) VALUES (?, ?, ?)`,
		"bench-session", "bash", time.Now().UnixMilli())
	require.NoError(b, err)

	commands := []string{
		"git status",
		"git commit -m 'fix bug'",
		"docker build -t myapp .",
		"kubectl get pods -n default",
		"npm run test -- --coverage",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cmd := commands[i%len(commands)]

		ev := &event.CommandEvent{
			Version:   1,
			Type:      "command_end",
			SessionID: "bench-session",
			Shell:     event.ShellBash,
			Cwd:       "/home/user/project",
			CmdRaw:    cmd,
			ExitCode:  0,
			TS:        time.Now().UnixMilli() + int64(i),
		}

		wctx := ingest.PrepareWriteContext(ev, "repo:/bench", "main", "", 0, false, nil)

		_, err := ingest.WritePath(ctx, db, wctx, &ingest.WritePathConfig{})
		if err != nil {
			b.Fatalf("WritePath error: %v", err)
		}
	}

	b.StopTimer()
	avg := time.Duration(int64(b.Elapsed()) / int64(b.N))
	b.ReportMetric(float64(avg.Microseconds()), "us/op")

	// Target: write path < 25ms p95 per spec Section 4.4
	if avg > 25*time.Millisecond {
		b.Logf("WARNING: average write path latency %v exceeds 25ms target", avg)
	}
}

// BenchmarkNormalize benchmarks the normalizer across various command types.
func BenchmarkNormalize(b *testing.B) {
	commands := []struct {
		name string
		cmd  string
	}{
		{"simple", "ls -la"},
		{"git_commit", "git commit -m 'fix: resolve issue with path handling' --no-verify"},
		{"docker_run", "docker run --name myapp -p 8080:80 -v /data:/app/data -e ENV=prod nginx:latest"},
		{"kubectl", "kubectl get pods -n kube-system -o json --sort-by=.metadata.creationTimestamp"},
		{"pipeline", "cat /var/log/syslog | grep error | sort | uniq -c | sort -rn | head -20"},
		{"long_args", fmt.Sprintf("echo %s", makeString(500, 'a'))},
	}

	n := normalize.NewNormalizer()

	for _, tc := range commands {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				n.Normalize(tc.cmd)
			}
		})
	}
}

// BenchmarkPreNormalize benchmarks the full pre-normalization pipeline.
func BenchmarkPreNormalize(b *testing.B) {
	commands := []struct {
		name string
		cmd  string
	}{
		{"simple", "git status"},
		{"pipeline", "cat file.txt | grep pattern | sort | uniq"},
		{"compound", "git add . && git commit -m 'update' && git push"},
		{"complex", "docker build -t myapp:v1.2.3 . && docker push myapp:v1.2.3 || echo 'push failed'"},
	}

	for _, tc := range commands {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				normalize.PreNormalize(tc.cmd, normalize.PreNormConfig{})
			}
		})
	}
}

// BenchmarkSearch_FTS benchmarks FTS5 search at various history sizes.
func BenchmarkSearch_FTS(b *testing.B) {
	sizes := []int{100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("history_%d", size), func(b *testing.B) {
			db := createBenchSearchDB(b, size)

			svc, err := search.NewService(db, search.Config{EnableFallback: true})
			require.NoError(b, err)
			b.Cleanup(func() { svc.Close() })

			if !svc.FTS5Available() {
				b.Skip("FTS5 not available")
			}

			ctx := context.Background()
			queries := []string{"git", "docker run", "npm install", "kubectl get"}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				query := queries[i%len(queries)]
				_, err := svc.Search(ctx, query, search.SearchOptions{Limit: 20})
				if err != nil {
					b.Fatalf("Search error: %v", err)
				}
			}

			b.StopTimer()
			avg := time.Duration(int64(b.Elapsed()) / int64(b.N))
			b.ReportMetric(float64(avg.Microseconds()), "us/op")
		})
	}
}

// createBenchSearchDB creates a database with n command events for search benchmarks.
func createBenchSearchDB(b *testing.B, n int) *sql.DB {
	b.Helper()

	dir, err := os.MkdirTemp("", "clai-bench-search-*")
	require.NoError(b, err)
	b.Cleanup(func() { os.RemoveAll(dir) })

	dbPath := filepath.Join(dir, "bench-search.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(b, err)
	b.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE command_event (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id    TEXT NOT NULL,
			ts            INTEGER NOT NULL,
			cmd_raw       TEXT NOT NULL,
			cmd_norm      TEXT NOT NULL,
			cwd           TEXT NOT NULL,
			repo_key      TEXT,
			ephemeral     INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX idx_event_ts ON command_event(ts);
	`)
	require.NoError(b, err)

	svc, err := search.NewService(db, search.Config{EnableFallback: true})
	require.NoError(b, err)
	defer svc.Close()

	commands := []string{
		"git status", "git commit -m 'update'", "git push origin main",
		"git pull", "git add .", "git diff", "git log --oneline",
		"docker build -t app .", "docker run -p 8080:80 app", "docker ps",
		"npm install", "npm test", "npm run build", "npm run dev",
		"kubectl get pods", "kubectl apply -f deploy.yaml", "kubectl logs pod-name",
		"make build", "make test", "make lint",
		"go test ./...", "go build -o app .", "go run main.go",
		"curl https://api.example.com", "ls -la /tmp", "cd ~/projects",
	}

	ctx := context.Background()
	nowMs := time.Now().UnixMilli()

	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		cmd := commands[i%len(commands)]
		result, err := db.Exec(`
			INSERT INTO command_event (session_id, ts, cmd_raw, cmd_norm, cwd, repo_key, ephemeral)
			VALUES ('session1', ?, ?, ?, '/home/user/project', 'repo:/bench', 0)
		`, nowMs+int64(i), cmd, cmd)
		require.NoError(b, err)

		id, err := result.LastInsertId()
		require.NoError(b, err)
		ids = append(ids, id)
	}

	// Index all events
	require.NoError(b, svc.IndexEventBatch(ctx, ids))

	return db
}

// makeString creates a string of length n filled with character c.
func makeString(n int, c byte) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
