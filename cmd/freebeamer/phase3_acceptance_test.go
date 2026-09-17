package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freebeamer/core/pkg/telemetry"
)

// TestMhdMonitorAcquisitionCadenceUnderDelayedRelay is the direct test of
// Phase 3's core goal: a slow relay must never stall the next scheduled
// DoIP acquisition. It measures the actual inter-sample acquisition
// timestamps recorded in the CSV output (not total process wall time,
// which legitimately includes a bounded, documented shutdown grace period
// — see UploaderService.Run's doc comment: an in-flight HTTP request is
// allowed to finish rather than aborted mid-request, which is a deliberate
// trade-off, not acquisition/delivery coupling) against a relay that's
// artificially slower than the configured acquisition interval, and
// asserts every gap stays within a documented tolerance of --interval —
// then logs the measured rate, per the plan's "record actual achievable
// rates" instruction (not just a pass/fail against the configured value).
func TestMhdMonitorAcquisitionCadenceUnderDelayedRelay(t *testing.T) {
	adapterAddr := startFakeDoipAdapter(t)
	server, apiKey := newFakeRelayServer(t)
	const relayDelay = 250 * time.Millisecond
	handler := delayingHandler(server.Handler(), func() time.Duration { return relayDelay })
	httpTestServer := httptest.NewServer(handler)
	t.Cleanup(httpTestServer.Close)

	const count = 8
	const interval = 50 * time.Millisecond
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	err := mhdMonitor([]string{
		"--upload-endpoint", httpTestServer.URL + "/v1/telemetry",
		"--api-key", apiKey,
		"--count", strconv.Itoa(count),
		"--interval", interval.String(),
		"--drain-timeout", "0s",
		"--buffer-file", filepath.Join(dir, "buffer.jsonl"),
		adapterAddr,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mhdMonitor: %v (stderr: %s)", err, stderr.String())
	}

	timestamps := parseCSVTimestamps(t, stdout.String())
	if len(timestamps) != count {
		t.Fatalf("got %d sample row(s), want %d", len(timestamps), count)
	}

	// If acquisition were still coupled to delivery, every gap would be at
	// least relayDelay (each sample waiting on its own relay round trip);
	// a generous tolerance well under that still leaves no room for that
	// coupling to hide.
	tolerance := 5 * interval
	if tolerance > relayDelay/2 {
		tolerance = relayDelay / 2
	}
	var gaps []time.Duration
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		gaps = append(gaps, gap)
		if gap > interval+tolerance {
			t.Errorf("gap between sample %d and %d = %v, want within %v of the configured interval %v (relay delay was %v)", i-1, i, gap, tolerance, interval, relayDelay)
		}
	}

	total := timestamps[len(timestamps)-1].Sub(timestamps[0])
	measuredRate := float64(len(timestamps)-1) / total.Seconds()
	t.Logf("measured acquisition rate: %.1f Hz across %d gaps spanning %v (configured interval %v, injected relay delay %v/request, gaps: %v)",
		measuredRate, len(gaps), total, interval, relayDelay, gaps)
}

func parseCSVTimestamps(t *testing.T, csv string) []time.Time {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) < 1 {
		t.Fatal("empty CSV output")
	}
	var timestamps []time.Time
	for _, line := range lines[1:] { // skip header
		fields := strings.SplitN(line, ",", 2)
		if len(fields) < 2 {
			t.Fatalf("malformed CSV row: %q", line)
		}
		ts, err := time.Parse(time.RFC3339Nano, fields[0])
		if err != nil {
			t.Fatalf("parse timestamp %q: %v", fields[0], err)
		}
		timestamps = append(timestamps, ts)
	}
	return timestamps
}

// TestMhdMonitorReprovisioningIsolatesSpool covers the plan's explicit
// "isolate spool data by relay/client destination" requirement: a device
// reprovisioned to point at a different relay/client must never flush its
// old buffered samples using the new destination's credentials, and the
// old destination's buffered data must stay exactly as it was.
func TestMhdMonitorReprovisioningIsolatesSpool(t *testing.T) {
	adapterAddr := startFakeDoipAdapter(t)

	// First destination: capability-negotiates v2 but always fails
	// delivery, so its samples stay durably spooled for inspection.
	firstRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(telemetry.RelayCapabilities())
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(firstRelay.Close)

	dir := t.TempDir()
	bufferFile := filepath.Join(dir, "buffer.jsonl")

	var stdout1, stderr1 bytes.Buffer
	if err := mhdMonitor([]string{
		"--upload-endpoint", firstRelay.URL + "/v1/telemetry",
		"--api-key", "old-client-key",
		"--count", "2", "--interval", "0s", "--drain-timeout", "0s",
		"--buffer-file", bufferFile,
		adapterAddr,
	}, &stdout1, &stderr1); err != nil {
		t.Fatalf("first run: %v (stderr: %s)", err, stderr1.String())
	}

	originalPending, err := (&telemetry.Spool{Path: bufferFile}).Pending()
	if err != nil {
		t.Fatal(err)
	}
	if originalPending != 2 {
		t.Fatalf("original spool pending = %d, want 2", originalPending)
	}

	// Reprovision: same operator-chosen --buffer-file, but a different
	// relay and API key.
	server2, apiKey2 := newFakeRelayServer(t)
	httpTestServer2 := httptest.NewServer(server2.Handler())
	t.Cleanup(httpTestServer2.Close)

	var stdout2, stderr2 bytes.Buffer
	if err := mhdMonitor([]string{
		"--upload-endpoint", httpTestServer2.URL + "/v1/telemetry",
		"--api-key", apiKey2,
		"--count", "1", "--interval", "0s", "--drain-timeout", "2s",
		"--buffer-file", bufferFile,
		adapterAddr,
	}, &stdout2, &stderr2); err != nil {
		t.Fatalf("second (reprovisioned) run: %v (stderr: %s)", err, stderr2.String())
	}

	if !strings.Contains(stderr2.String(), "different relay/client destination") {
		t.Fatalf("expected a reprovisioning notice on stderr, got: %q", stderr2.String())
	}

	stillPending, err := (&telemetry.Spool{Path: bufferFile}).Pending()
	if err != nil {
		t.Fatal(err)
	}
	if stillPending != originalPending {
		t.Fatalf("original destination's spool changed: pending = %d, want unchanged %d (must not be touched by the reprovisioned run)", stillPending, originalPending)
	}
}

// TestMhdMonitorRepeatedStartStopNoGoroutineLeak runs mhdMonitor
// back-to-back several times in one process and asserts no goroutines
// accumulate across runs — the queue/spool/uploader goroutines introduced
// in Phase 3 must all shut down cleanly on every return path.
func TestMhdMonitorRepeatedStartStopNoGoroutineLeak(t *testing.T) {
	adapterAddr := startFakeDoipAdapter(t)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 4; i++ {
		dir := t.TempDir()
		var stdout, stderr bytes.Buffer
		err := mhdMonitor([]string{
			"--count", "3", "--interval", "0s", "--drain-timeout", "0s",
			"--buffer-file", filepath.Join(dir, "buffer.jsonl"),
			adapterAddr,
		}, &stdout, &stderr)
		if err != nil {
			t.Fatalf("run %d: %v (stderr: %s)", i, err, stderr.String())
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := runtime.NumGoroutine(); n <= baseline+2 { // small slack for runtime bookkeeping
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count after repeated start/stop = %d, baseline = %d (possible leak)", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestMhdMonitorAdapterDisconnectMidStream simulates the adapter hanging up
// partway through a run and asserts mhdMonitor returns a clear error
// (rather than hanging) and that samples already spooled before the
// disconnect remain intact.
func TestMhdMonitorAdapterDisconnectMidStream(t *testing.T) {
	adapterAddr := startFakeDoipAdapterWithLimit(t, 2) // disconnect after 2 successful replies

	dir := t.TempDir()
	bufferFile := filepath.Join(dir, "buffer.jsonl")
	var stdout, stderr bytes.Buffer
	err := mhdMonitor([]string{
		"--count", "5", "--interval", "10ms", "--drain-timeout", "0s",
		"--buffer-file", bufferFile,
		adapterAddr,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected an error after the adapter disconnected mid-stream")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Fatalf("error = %v, want a read-related error", err)
	}

	pending, perr := (&telemetry.Spool{Path: bufferFile}).Pending()
	if perr != nil {
		t.Fatal(perr)
	}
	if pending != 2 {
		t.Fatalf("spool pending after disconnect = %d, want 2 (the samples acquired before the disconnect must survive)", pending)
	}
}
