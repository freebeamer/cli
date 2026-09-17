package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freebeamer/core/pkg/doip"
	"github.com/freebeamer/core/pkg/mhd"
	"github.com/freebeamer/core/pkg/telemetry"
)

func runMhdCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "monitor":
		return mhdMonitor(args[1:], stdout, stderr)
	default:
		return usageError()
	}
}

// mhdMonitor connects to a DoIP entity (MHD's WiFi-ENET adapter, or any
// compatible ENET hardware — see docs/mhd-live-monitor-v0-plan.md),
// performs routing activation, then repeatedly issues MG1.adx's monitor
// exchange and writes one CSV row per sample to stdout.
//
// Acquisition (reading the adapter) is decoupled from delivery (uploading
// to --upload-endpoint) — see docs/live-data-and-vehicle-mapping-plan.md's
// Phase 3 section for why: a slow or unreachable relay must never stall the
// next scheduled DoIP read. Three concurrent stages share one durable spool
// file: this function's own loop only ever pushes a freshly read sample
// into a small in-memory telemetry.AcquisitionQueue (never blocks); a
// spool-writer goroutine drains that queue into a telemetry.Spool on disk;
// a telemetry.UploaderService goroutine continuously drains the spool over
// HTTP with retry/backoff. A periodic status line on stderr reports queue
// depth, spool backlog, upload state, catch-up progress, and measured
// acquisition rate — this replaced the old per-row CSV delivery column,
// which could only report an outcome (queued vs. eventually delivered) that
// this loop no longer learns synchronously.
func mhdMonitor(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("mhd monitor", flag.ContinueOnError)
	set.SetOutput(stderr)
	targetOverride := set.String("target", "", "diagnostic target override (decimal or 0x-prefixed; requires vehicle evidence)")
	capturePath := set.String("capture-file", "", "new private JSONL file for raw monitor replies and request timing")
	source := set.Uint("source", 0x0E00, "this client's own DoIP logical source address")
	dialTimeout := set.Duration("dial-timeout", 2*time.Second, "TCP dial timeout")
	requestTimeout := set.Duration("timeout", time.Second, "per-request timeout")
	interval := set.Duration("interval", 100*time.Millisecond, "delay between samples")
	count := set.Int("count", 0, "number of samples to read (0 = run until interrupted)")
	deviceID := set.String("device-id", defaultDeviceID(), "identifies this device to the relay API")
	uploadEndpoint := set.String("upload-endpoint", "", "relay API URL to POST samples to (empty = buffer only)")
	apiKey := set.String("api-key", os.Getenv("FREEBEAMER_RELAY_API_KEY"), "client API key for --upload-endpoint (defaults to $FREEBEAMER_RELAY_API_KEY)")
	bufferFile := set.String("buffer-file", "mhd-buffer.jsonl", "local file to buffer samples in when the relay API isn't reachable")
	queueCapacity := set.Int("queue-capacity", telemetry.DefaultQueueCapacity, "capacity of the in-memory acquisition queue between DoIP reads and the durable spool")
	bufferMaxBytes := set.Int64("buffer-max-bytes", telemetry.DefaultSpoolMaxBytes, "maximum size in bytes of the on-disk buffer/spool file")
	onSpoolFull := set.String("on-spool-full", string(telemetry.OverflowDrop), "behavior once --buffer-max-bytes is reached: drop (drop new samples, keep recording, count the drops) or stop (stop recording)")
	statusInterval := set.Duration("status-interval", 5*time.Second, "how often to print a delivery status line to stderr (0 disables it)")
	drainTimeout := set.Duration("drain-timeout", 10*time.Second, "on normal completion (--count reached, not interrupted), how long to wait for the spool to finish delivering before exiting")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return errors.New("usage: freebeamer mhd monitor [--target ADDR] [--capture-file PATH] [--source ADDR] [--timeout DURATION] [--interval DURATION] [--count N] [--upload-endpoint URL] [--api-key KEY] [--buffer-file PATH] [--queue-capacity N] [--buffer-max-bytes N] [--on-spool-full drop|stop] [--status-interval DURATION] [--drain-timeout DURATION] [--device-id ID] HOST[:PORT]")
	}

	if *source > 0xffff {
		return errors.New("mhd monitor: source must fit a 16-bit address")
	}
	if *dialTimeout <= 0 || *requestTimeout <= 0 || *interval < 0 || *count < 0 {
		return errors.New("mhd monitor: timeouts must be positive; interval and count must be non-negative")
	}
	if *queueCapacity < 0 || *bufferMaxBytes < 0 || *statusInterval < 0 || *drainTimeout < 0 {
		return errors.New("mhd monitor: --queue-capacity, --buffer-max-bytes, --status-interval, and --drain-timeout must be non-negative")
	}
	overflowPolicy := telemetry.OverflowPolicy(*onSpoolFull)
	if overflowPolicy != telemetry.OverflowDrop && overflowPolicy != telemetry.OverflowStop {
		return fmt.Errorf("mhd monitor: --on-spool-full must be %q or %q, got %q", telemetry.OverflowDrop, telemetry.OverflowStop, *onSpoolFull)
	}
	var diagnosticTarget uint64
	if *targetOverride != "" {
		var err error
		diagnosticTarget, err = strconv.ParseUint(*targetOverride, 0, 16)
		if err != nil {
			return fmt.Errorf("mhd monitor: invalid target: %w", err)
		}
	}
	var capture *json.Encoder
	if *capturePath != "" {
		file, err := os.OpenFile(*capturePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("mhd monitor: capture: %w", err)
		}
		defer file.Close()
		capture = json.NewEncoder(file)
	}

	address := set.Arg(0)
	if !strings.Contains(address, ":") {
		address = fmt.Sprintf("%s:%d", address, doip.DefaultTCPPort)
	}

	destinationID := telemetry.DestinationID(*uploadEndpoint, *apiKey)
	spoolPath, spoolNotice, err := telemetry.ResolveSpoolPath(*bufferFile, destinationID)
	if err != nil {
		return fmt.Errorf("mhd monitor: spool: %w", err)
	}
	if spoolNotice != "" {
		fmt.Fprintln(stderr, "mhd monitor: "+spoolNotice)
	}

	uploader := &telemetry.Uploader{Endpoint: *uploadEndpoint, APIKey: *apiKey, BufferPath: spoolPath}
	spool := &telemetry.Spool{Path: spoolPath, MaxBytes: *bufferMaxBytes, OnFull: overflowPolicy}
	queue := telemetry.NewAcquisitionQueue(*queueCapacity)
	status := telemetry.NewStatus()
	uploaderService := &telemetry.UploaderService{Uploader: uploader, Spool: spool, Status: status, Interval: *interval}

	version, negotiationErr := uploader.Negotiate()
	if negotiationErr != nil {
		version = telemetry.ContractVersion
		fmt.Fprintln(stderr, "mhd monitor: relay capabilities unavailable; preserving v2 identity and buffering until support is verified")
	}
	fmt.Fprintf(stderr, "mhd monitor: telemetry version %d\n", version)
	session, err := telemetry.NewSession()
	if err != nil {
		return err
	}

	client, err := doip.Dial(address, *dialTimeout)
	if err != nil {
		return err
	}
	defer client.Close()

	target, err := client.Activate(uint16(*source), doip.ActivationDefault, *requestTimeout)
	if err != nil {
		return fmt.Errorf("mhd monitor: routing activation: %w", err)
	}
	responder := target
	if *targetOverride != "" {
		target = uint16(diagnosticTarget)
		client.SetTargetAddress(target)
	}
	fmt.Fprintf(stderr, "mhd monitor: routing activation responder 0x%04X, diagnostic target 0x%04X (vehicle compatibility unverified)\n", responder, target)
	if capture != nil {
		if err := capture.Encode(map[string]any{"type": "routing", "format_version": 1, "timestamp": time.Now().UTC(), "source": *source, "responder": responder, "target": target, "target_override": *targetOverride != ""}); err != nil {
			return fmt.Errorf("mhd monitor: capture: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() {
		cancel()
		wg.Wait()
	}()

	wg.Add(1)
	go runSpoolWriter(ctx, &wg, queue, spool, status)
	wg.Add(1)
	go func() {
		defer wg.Done()
		uploaderService.Run(ctx)
	}()
	if *statusInterval > 0 {
		wg.Add(1)
		go runStatusReporter(ctx, &wg, stderr, status, *statusInterval)
	}

	names := make([]string, len(mhd.Fields))
	for i, field := range mhd.Fields {
		names[i] = field.Name
	}
	fmt.Fprintf(stdout, "timestamp,%s,queue-status\n", strings.Join(names, ","))

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	defer signal.Stop(interrupted)

	wasInterrupted := false
sampleLoop:
	for sample := 0; *count == 0 || sample < *count; sample++ {
		select {
		case <-interrupted:
			wasInterrupted = true
			break sampleLoop
		default:
		}

		started := time.Now()
		reply, readErr := client.SendDiagnostic(mhd.MonitorRequest, *requestTimeout)
		elapsed := time.Since(started)
		values, err := mhd.DecodeMonitorReply(reply)
		if readErr != nil {
			err = readErr
		}
		if capture != nil {
			record := map[string]any{"type": "exchange", "sequence": sample, "started_at": started.UTC(), "elapsed_ns": elapsed.Nanoseconds(), "request_hex": hex.EncodeToString(mhd.MonitorRequest), "reply_hex": hex.EncodeToString(reply)}
			if err != nil {
				record["error"] = err.Error()
			}
			if captureErr := capture.Encode(record); captureErr != nil {
				return fmt.Errorf("mhd monitor: capture: %w", captureErr)
			}
		}
		if err != nil {
			return fmt.Errorf("mhd monitor: read: %w", err)
		}
		now := time.Now()
		status.RecordAcquired(now)
		row := make([]string, len(values))
		for i, v := range values {
			row[i] = strconv.FormatFloat(v.Value, 'f', -1, 64)
		}

		reading := telemetry.NewSample(*deviceID, now, values)
		if version == telemetry.ContractVersion {
			sequence := uint64(sample)
			reading.Version = version
			reading.Session = &session
			reading.Sequence = &sequence
		}
		if err := reading.Validate(); err != nil {
			return err
		}
		queued := queue.Push(reading)
		status.SetQueue(queue.Depth(), queue.OverflowCount())
		queueStatus := "queued"
		if !queued {
			queueStatus = "queue-dropped"
		}
		fmt.Fprintf(stdout, "%s,%s,%s\n", now.Format(time.RFC3339Nano), strings.Join(row, ","), queueStatus)

		if *interval > 0 && (*count == 0 || sample < *count-1) {
			select {
			case <-time.After(*interval):
			case <-interrupted:
				wasInterrupted = true
				break sampleLoop
			}
		}
	}

	if !wasInterrupted && *drainTimeout > 0 {
		waitForSpoolDrain(queue, spool, *drainTimeout, interrupted)
	}
	return nil
}

// runSpoolWriter drains queue into spool until ctx is cancelled, then drains
// whatever is still queued one last time (non-blocking) before exiting, so
// a clean shutdown doesn't lose samples that were already accepted into the
// in-memory queue but not yet durably spooled.
func runSpoolWriter(ctx context.Context, wg *sync.WaitGroup, queue *telemetry.AcquisitionQueue, spool *telemetry.Spool, status *telemetry.Status) {
	defer wg.Done()
	appendSample := func(sample telemetry.Sample) {
		if err := spool.Append(sample); err != nil {
			status.SetError(fmt.Sprintf("spool: %v", err))
		}
		status.SetSpool(mustPending(spool), spool.DroppedCount())
		queue.MarkHandled()
	}
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case sample := <-queue.C():
					appendSample(sample)
				default:
					return
				}
			}
		case sample := <-queue.C():
			appendSample(sample)
		}
	}
}

func mustPending(spool *telemetry.Spool) int {
	pending, err := spool.Pending()
	if err != nil {
		return -1
	}
	return pending
}

func runStatusReporter(ctx context.Context, wg *sync.WaitGroup, stderr io.Writer, status *telemetry.Status, interval time.Duration) {
	defer wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			printStatus(stderr, status.Snapshot())
		}
	}
}

func printStatus(w io.Writer, snap telemetry.StatusSnapshot) {
	lastFresh := "never"
	if !snap.LastFreshSampleAt.IsZero() {
		lastFresh = fmt.Sprintf("%s (%s ago)", snap.LastFreshSampleAt.Format(time.RFC3339), time.Since(snap.LastFreshSampleAt).Round(100*time.Millisecond))
	}
	catchup := "-"
	if snap.CatchupInProgress {
		catchup = fmt.Sprintf("%.0f%%", snap.CatchupProgress*100)
	}
	errSuffix := ""
	if snap.LastError != "" {
		errSuffix = fmt.Sprintf(" error=%q", snap.LastError)
	}
	fmt.Fprintf(w, "mhd monitor: rate=%.1fHz queued=%d spooled=%d upload=%s catchup=%s last_fresh=%s dropped_queue=%d dropped_spool=%d%s\n",
		snap.AcquisitionRateHz, snap.QueueDepth, snap.SpoolPending, snap.UploadState, catchup, lastFresh, snap.QueueDropped, snap.SpoolDropped, errSuffix)
}

// waitForSpoolDrain polls until the queue is fully drained and the spool is
// empty, timeout elapses, or an interrupt arrives, whichever comes first —
// giving a bounded, finite (--count) run a real chance to actually deliver
// before exiting, without hanging indefinitely against an unreachable
// relay. It checks queue.Drained(), not queue.Depth() == 0: right after the
// last sample is acquired, the channel can read empty the instant its value
// is handed to the spool-writer goroutine, well before that goroutine
// actually finishes appending it to disk — Drained() is race-free against
// that gap (see AcquisitionQueue.Drained's doc comment) where a bare depth
// check is not.
func waitForSpoolDrain(queue *telemetry.AcquisitionQueue, spool *telemetry.Spool, timeout time.Duration, interrupted <-chan os.Signal) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pending, err := spool.Pending()
		if (err != nil || pending == 0) && queue.Drained() {
			return
		}
		select {
		case <-interrupted:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// defaultDeviceID falls back to the machine's hostname, so samples are
// attributable to a device even when --device-id isn't set explicitly.
func defaultDeviceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		return ""
	}
	return hostname
}
