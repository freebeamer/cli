package main

import (
	"bytes"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freebeamer/core/pkg/enet"
	"github.com/freebeamer/core/pkg/hsfz"
	"github.com/freebeamer/core/pkg/mhd"
)

// startFakeHsfzGateway serves MHD's monitor reply over HSFZ, the way MHD's
// WiFi-ENET adapter was observed to on port 6801.
func startFakeHsfzGateway(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				for {
					var header [6]byte
					if _, err := io.ReadFull(conn, header[:]); err != nil {
						return
					}
					length, _ := hsfz.ParseHeader(header)
					body := make([]byte, length)
					if _, err := io.ReadFull(conn, body); err != nil {
						return
					}
					conn.Write(hsfz.PackFrame(hsfz.ControlAcknowledge, body))
					reply := []byte{body[1], body[0]}
					if bytes.Equal(body[2:], []byte{0x3E, 0x00}) {
						reply = append(reply, 0x7E, 0x00)
					} else {
						reply = append(reply, mhd.MonitorReplyHeader...)
						reply = append(reply, make([]byte, mhd.MonitorReplyBodyLength)...)
					}
					conn.Write(hsfz.PackFrame(hsfz.ControlDiagnostic, reply))
				}
			}(conn)
		}
	}()
	return listener.Addr().String()
}

func TestMhdMonitorOverHSFZ(t *testing.T) {
	address := startFakeHsfzGateway(t)
	var stdout, stderr bytes.Buffer
	err := mhdMonitor([]string{
		"--count", "2", "--interval", "0s", "--drain-timeout", "0s",
		"--buffer-file", filepath.Join(t.TempDir(), "buffer.jsonl"),
		address,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("mhdMonitor: %v (stderr: %s)", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "hsfz on port") {
		t.Fatalf("stderr should report the detected protocol:\n%s", stderr.String())
	}
	if rows := strings.Count(strings.TrimSpace(stdout.String()), "\n"); rows < 2 {
		t.Fatalf("want a header and 2 sample rows, got:\n%s", stdout.String())
	}
}

func TestSplitAdapterAddress(t *testing.T) {
	for in, want := range map[string]struct {
		host string
		port int
	}{
		"192.168.4.1":       {"192.168.4.1", enet.DefaultPort},
		"192.168.4.1:13400": {"192.168.4.1", 13400},
		"[fe80::1]:6801":    {"fe80::1", 6801},
	} {
		host, port, err := splitAdapterAddress(in)
		if err != nil || host != want.host || port != want.port {
			t.Errorf("splitAdapterAddress(%q) = %q, %d, %v", in, host, port, err)
		}
	}
	for _, bad := range []string{"192.168.4.1:134000", "192.168.4.1:0", "192.168.4.1:x"} {
		if _, _, err := splitAdapterAddress(bad); err == nil {
			t.Errorf("splitAdapterAddress(%q) should fail", bad)
		}
	}
}
