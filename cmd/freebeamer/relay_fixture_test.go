package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/freebeamer/relay/relay"
)

const fakeRelayAdminToken = "test-admin-token"

// newFakeRelayServer builds a real internal/relay.Server (real SQLite
// store in a temp file, real dedup/session/capability/negotiation logic —
// not a hand-rolled mock) with one pre-registered client. It returns the
// server (callers wrap server.Handler() in an httptest.Server themselves,
// optionally through a fault-injecting layer like delayingHandler) and the
// client's raw API key.
func newFakeRelayServer(t *testing.T) (server *relay.Server, apiKey string) {
	t.Helper()
	store, err := relay.NewSQLiteStore(filepath.Join(t.TempDir(), "relay.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	const rawKey = "test-raw-api-key"
	sum := sha256.Sum256([]byte(rawKey))
	if _, err := store.CreateClient(context.Background(), "client-1", "Test Client", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}

	server, err = relay.NewServer("127.0.0.1:0", relay.Config{Store: store, AdminToken: fakeRelayAdminToken})
	if err != nil {
		t.Fatal(err)
	}
	return server, rawKey
}

// delayingHandler wraps h, sleeping delay() before forwarding every
// request — fault injection for cadence/backlog acceptance tests, since
// the real relay has no built-in rate limiting or artificial latency to
// exercise that against (see internal/relay's own documented lack of
// backpressure).
func delayingHandler(h http.Handler, delay func() time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d := delay(); d > 0 {
			time.Sleep(d)
		}
		h.ServeHTTP(w, r)
	})
}
