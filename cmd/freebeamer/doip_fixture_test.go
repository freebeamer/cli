package main

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// startFakeDoipAdapter starts a minimal DoIP TCP server that performs one
// routing activation per connection (always responding with responder
// 0x1234) and then answers every subsequent request with a well-formed
// (all-zero) 36-byte monitor reply, addressed back to whatever source/target
// the request used — reusable across Phase 3 acceptance tests that need a
// real DoIP-shaped peer without caring about its decoded field values. The
// listener is closed automatically at test cleanup.
func startFakeDoipAdapter(t *testing.T) (addr string) {
	t.Helper()
	return startFakeDoipAdapterWithLimit(t, 0)
}

// startFakeDoipAdapterWithLimit is startFakeDoipAdapter, but each accepted
// connection closes itself after replying to maxReplies monitor requests
// (0 = unlimited) — used to simulate an adapter disconnecting mid-stream.
func startFakeDoipAdapterWithLimit(t *testing.T, maxReplies int) (addr string) {
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
			go serveFakeDoipConnection(conn, maxReplies)
		}
	}()
	return listener.Addr().String()
}

func serveFakeDoipConnection(conn net.Conn, maxReplies int) {
	defer conn.Close()
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

	if _, err := read(); err != nil {
		return
	}
	// Routing activation response: responder 0x1234.
	if err := write(6, []byte{0x0e, 0, 0x12, 0x34, 0x10, 0, 0, 0, 0}); err != nil {
		return
	}

	for sent := 0; maxReplies <= 0 || sent < maxReplies; sent++ {
		request, err := read()
		if err != nil || len(request) < 4 {
			return
		}
		reqSource := request[0:2]
		reqTarget := request[2:4]
		reply := make([]byte, 0, 6+36)
		reply = append(reply, reqTarget...) // reply source = request's target (the responder)
		reply = append(reply, reqSource...) // reply target = request's source (echo back)
		reply = append(reply, 0x6c, 0x88)
		reply = append(reply, make([]byte, 36)...)
		if err := write(0x8001, reply); err != nil {
			return
		}
	}
	// maxReplies reached: simulate the adapter disconnecting mid-stream by
	// closing the connection instead of answering further requests.
}
