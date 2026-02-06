package metrics

import (
	"sync"
	"testing"
)

func TestCounters_Snapshot_AllFields(t *testing.T) {
	c := &Counters{}

	// All fields should start at zero
	snap := c.Snapshot()

	expectedKeys := []string{
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

	if len(snap) != len(expectedKeys) {
		t.Errorf("Snapshot() returned %d fields, want %d", len(snap), len(expectedKeys))
	}

	for _, key := range expectedKeys {
		val, ok := snap[key]
		if !ok {
			t.Errorf("Snapshot() missing key %q", key)
			continue
		}
		if val != 0 {
			t.Errorf("Snapshot()[%q] = %d, want 0 for fresh counters", key, val)
		}
	}
}

func TestCounters_IncrementAndSnapshot(t *testing.T) {
	c := &Counters{}

	c.SuggestRequests.Add(10)
	c.SuggestHits.Add(7)
	c.SuggestMisses.Add(3)
	c.CacheHits.Add(5)
	c.CacheMisses.Add(5)
	c.FeedbackAccepted.Add(4)
	c.FeedbackDismissed.Add(2)
	c.FeedbackEdited.Add(1)
	c.IngestCommands.Add(100)
	c.IngestErrors.Add(2)
	c.LatencySumMs.Add(500)

	snap := c.Snapshot()

	expected := map[string]int64{
		"suggest_requests":   10,
		"suggest_hits":       7,
		"suggest_misses":     3,
		"cache_hits":         5,
		"cache_misses":       5,
		"feedback_accepted":  4,
		"feedback_dismissed": 2,
		"feedback_edited":    1,
		"ingest_commands":    100,
		"ingest_errors":      2,
		"latency_sum_ms":     500,
	}

	for key, want := range expected {
		got, ok := snap[key]
		if !ok {
			t.Errorf("Snapshot() missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("Snapshot()[%q] = %d, want %d", key, got, want)
		}
	}
}

func TestCounters_Reset(t *testing.T) {
	c := &Counters{}

	// Increment everything
	c.SuggestRequests.Add(10)
	c.SuggestHits.Add(7)
	c.SuggestMisses.Add(3)
	c.CacheHits.Add(5)
	c.CacheMisses.Add(5)
	c.FeedbackAccepted.Add(4)
	c.FeedbackDismissed.Add(2)
	c.FeedbackEdited.Add(1)
	c.IngestCommands.Add(100)
	c.IngestErrors.Add(2)
	c.LatencySumMs.Add(500)

	c.Reset()

	snap := c.Snapshot()
	for key, val := range snap {
		if val != 0 {
			t.Errorf("after Reset(), Snapshot()[%q] = %d, want 0", key, val)
		}
	}
}

func TestCounters_ConcurrentAccess(t *testing.T) {
	c := &Counters{}

	const goroutines = 100
	const increments = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				c.SuggestRequests.Add(1)
				c.IngestCommands.Add(1)
				c.LatencySumMs.Add(1)
			}
		}()
	}

	wg.Wait()

	snap := c.Snapshot()
	want := int64(goroutines * increments)

	if snap["suggest_requests"] != want {
		t.Errorf("suggest_requests = %d, want %d", snap["suggest_requests"], want)
	}
	if snap["ingest_commands"] != want {
		t.Errorf("ingest_commands = %d, want %d", snap["ingest_commands"], want)
	}
	if snap["latency_sum_ms"] != want {
		t.Errorf("latency_sum_ms = %d, want %d", snap["latency_sum_ms"], want)
	}
}

func TestCounters_AverageSuggestLatencyMs(t *testing.T) {
	c := &Counters{}

	// Zero requests should return 0
	if avg := c.AverageSuggestLatencyMs(); avg != 0 {
		t.Errorf("AverageSuggestLatencyMs() with no requests = %f, want 0", avg)
	}

	c.SuggestRequests.Add(4)
	c.LatencySumMs.Add(100)

	avg := c.AverageSuggestLatencyMs()
	if avg != 25.0 {
		t.Errorf("AverageSuggestLatencyMs() = %f, want 25.0", avg)
	}
}

func TestCounters_CacheHitRate(t *testing.T) {
	c := &Counters{}

	// Zero lookups should return 0
	if rate := c.CacheHitRate(); rate != 0 {
		t.Errorf("CacheHitRate() with no lookups = %f, want 0", rate)
	}

	c.CacheHits.Add(3)
	c.CacheMisses.Add(7)

	rate := c.CacheHitRate()
	if rate != 0.3 {
		t.Errorf("CacheHitRate() = %f, want 0.3", rate)
	}
}

func TestGlobal_IsSingleton(t *testing.T) {
	if Global == nil {
		t.Fatal("Global should not be nil")
	}

	// Verify Global works like any other Counters
	Global.Reset()
	Global.SuggestRequests.Add(1)

	snap := Global.Snapshot()
	if snap["suggest_requests"] != 1 {
		t.Errorf("Global.Snapshot()[suggest_requests] = %d, want 1", snap["suggest_requests"])
	}

	// Clean up
	Global.Reset()
}
