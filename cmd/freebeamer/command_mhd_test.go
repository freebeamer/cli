package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/freebeamer/core/pkg/telemetry"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMhdMonitorValidation(t *testing.T) {
	for _, args := range [][]string{{"--source", "65536"}, {"--target", "0x10000"}, {"--target", "wrong"}, {"--timeout", "0s"}, {"--dial-timeout", "-1s"}, {"--interval", "-1s"}, {"--count", "-1"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := mhdMonitor(append(args, "127.0.0.1:1"), io.Discard, io.Discard)
			if err == nil || strings.Contains(err.Error(), "dial") {
				t.Fatalf("expected validation before dialing, got %v", err)
			}
		})
	}
}

func TestMhdCaptureRefusesOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	err := mhdMonitor([]string{"--capture-file", path, "127.0.0.1:1"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "capture") {
		t.Fatalf("got %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("capture overwritten")
	}
}

func TestMhdMonitorCaptureAndTarget(t *testing.T) {
	for _, test := range []struct {
		name      string
		malformed bool
		version   int
	}{
		{"offline", false, 0}, {"malformed", true, 0}, {"v2-relay", false, 2}, {"legacy-relay", false, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			malformed := test.malformed
			endpoint := ""
			if test.version != 0 {
				relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet {
						if test.version == 1 {
							w.WriteHeader(404)
							return
						}
						json.NewEncoder(w).Encode(telemetry.RelayCapabilities())
						return
					}
					// Force recording to disk to inspect the producer's wire identity.
					w.WriteHeader(503)
				}))
				defer relay.Close()
				endpoint = relay.URL + "/v1/telemetry"
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				read := func() ([]byte, error) {
					header := make([]byte, 8)
					if _, err := io.ReadFull(conn, header); err != nil {
						return nil, err
					}
					body := make([]byte, binary.BigEndian.Uint32(header[4:]))
					_, err := io.ReadFull(conn, body)
					return body, err
				}
				write := func(kind uint16, body []byte) error {
					header := []byte{2, 253, 0, 0, 0, 0, 0, 0}
					binary.BigEndian.PutUint16(header[2:], kind)
					binary.BigEndian.PutUint32(header[4:], uint32(len(body)))
					_, err := conn.Write(append(header, body...))
					return err
				}
				if _, err = read(); err != nil {
					done <- err
					return
				}
				if err = write(6, []byte{0x0e, 0, 0x12, 0x34, 0x10, 0, 0, 0, 0}); err != nil {
					done <- err
					return
				}
				for attempt := 0; attempt < 2; attempt++ {
					request, err := read()
					if err != nil {
						done <- err
						return
					}
					if !bytes.Equal(request, []byte{0x0e, 0, 0x56, 0x78, 0x2c, 0x88, 1}) {
						done <- io.ErrUnexpectedEOF
						return
					}
					reply := append([]byte{0x56, 0x78, 0x0e, 0, 0x6c, 0x88}, make([]byte, 36)...)
					if malformed {
						reply = reply[:7]
					}
					if err := write(0x8001, reply); err != nil {
						done <- err
						return
					}
					if malformed {
						break
					}
				}
				done <- nil
			}()
			dir := t.TempDir()
			capture := filepath.Join(dir, "capture.jsonl")
			var stdout, stderr bytes.Buffer
			err = mhdMonitor([]string{"--upload-endpoint", endpoint, "--target", "0x5678", "--count", "2", "--interval", "0s", "--drain-timeout", "0s", "--capture-file", capture, "--buffer-file", filepath.Join(dir, "buffer.jsonl"), listener.Addr().String()}, &stdout, &stderr)
			if malformed && err == nil || !malformed && err != nil {
				t.Fatalf("monitor error: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(capture)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			decoder := json.NewDecoder(file)
			var routing, exchange map[string]any
			if err := decoder.Decode(&routing); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(&exchange); err != nil {
				t.Fatal(err)
			}
			if routing["responder"] != float64(0x1234) || routing["target"] != float64(0x5678) {
				t.Fatalf("routing: %v", routing)
			}
			if exchange["request_hex"] != "2c8801" || !strings.HasPrefix(exchange["reply_hex"].(string), "6c88") {
				t.Fatalf("exchange: %v", exchange)
			}
			if _, ok := exchange["error"]; ok != malformed {
				t.Fatalf("error evidence: %v", exchange)
			}
			if !malformed && !strings.Contains(stdout.String(), "queued") {
				t.Fatal("missing CSV sample")
			}
			if !malformed {
				data, err := os.ReadFile(filepath.Join(dir, "buffer.jsonl"))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				if len(lines) != 2 {
					t.Fatalf("buffer sample count %d", len(lines))
				}
				var first, second telemetry.Sample
				if err = json.Unmarshal([]byte(lines[0]), &first); err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal([]byte(lines[1]), &second); err != nil {
					t.Fatal(err)
				}
				if test.version == 1 {
					if first.Version != 0 || first.Session != nil || first.Sequence != nil || second.Session != nil {
						t.Fatal("legacy relay received v2 identity")
					}
				} else if first.Version != 2 || first.Session == nil || first.Session.ID == "" || first.Session.ID != second.Session.ID || *first.Sequence != 0 || *second.Sequence != 1 {
					t.Fatalf("invalid producer identity: %+v %+v", first, second)
				}
			}
			info, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0077 != 0 {
				t.Fatal("capture readable by other users")
			}
		})
	}
}
