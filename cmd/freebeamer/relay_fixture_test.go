package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/freebeamer/relay/relay"
)

const fakeRelayAdminToken = "test-admin-token"

// hashFakeToken mirrors internal/relay/auth.go's unexported hashToken
// (SHA-256, hex-encoded) — this package can't import it directly since
// it's a different module, so the fixture reproduces the same
// convention to talk to the real Store directly, same as the relay
// module's own tests do.
func hashFakeToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// newFakeRelayServer builds a real internal/relay.Server (real SQLite
// store in a temp file, real dedup/session/capability/negotiation logic —
// not a hand-rolled mock), pairs one client ("client-1") via the same
// pairing-link + Ed25519 flow a real device would go through, and mints
// it a device session token directly against the store (skipping the
// HTTP challenge/response round trip, which the relay module's own
// tests already cover). It returns the server (callers wrap
// server.Handler() in an httptest.Server themselves, optionally
// through a fault-injecting layer like delayingHandler) and the
// client's device session token.
func newFakeRelayServer(t *testing.T) (server *relay.Server, sessionToken string) {
	t.Helper()
	store, err := relay.NewSQLiteStore(filepath.Join(t.TempDir(), "relay.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	ctx := context.Background()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const pairingToken = "test-pairing-token"
	if _, err := store.CreatePairingLink(ctx, hashFakeToken(pairingToken), "Test Client", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumePairingLink(ctx, hashFakeToken(pairingToken), "client-1", pub); err != nil {
		t.Fatal(err)
	}

	const rawSessionToken = "test-device-session-token"
	if _, err := store.CreateDeviceSession(ctx, hashFakeToken(rawSessionToken), "client-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	server, err = relay.NewServer("127.0.0.1:0", relay.Config{Store: store, AdminToken: fakeRelayAdminToken})
	if err != nil {
		t.Fatal(err)
	}
	return server, rawSessionToken
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
