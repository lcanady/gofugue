package transport_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kumakun/gofugue/internal/transport"
)

// --- factory ---

func TestNew_UnknownScheme_ReturnsError(t *testing.T) {
	_, err := transport.New(transport.Config{URL: "ftp://example.com"})
	if err == nil {
		t.Fatal("expected error for unknown scheme, got nil")
	}
}

func TestNew_InvalidURL_ReturnsError(t *testing.T) {
	_, err := transport.New(transport.Config{URL: "://bad"})
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestNew_MudScheme_ReturnsTCP(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "mud://localhost:4000"})
	if err != nil {
		t.Fatalf("New(mud://): %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestNew_MudsScheme_ReturnsTLS(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "muds://localhost:4000"})
	if err != nil {
		t.Fatalf("New(muds://): %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestNew_WsScheme_ReturnsTransport(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "ws://localhost:4000/mud"})
	if err != nil {
		t.Fatalf("New(ws://): %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestNew_WtScheme_ReturnsTransport(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "wt://localhost:4000/mud"})
	if err != nil {
		t.Fatalf("New(wt://): %v", err)
	}
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

// --- TCP integration (uses loopback) ---

func TestTCP_Dial_Connect_SendReceive(t *testing.T) {
	// Start a mock TCP server.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo back what it receives.
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		conn.Write(buf[:n])
	}()

	tr, err := transport.New(transport.Config{URL: "mud://" + ln.Addr().String()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	msg := []byte("hello\r\n")
	if _, err := rwc.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 64)
	n, err := rwc.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("echo mismatch: got %q, want %q", buf[:n], msg)
	}

	<-done
}

func TestTCP_Dial_ContextCancel_FailsFast(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "mud://192.0.2.1:9999"}) // TEST-NET, unreachable
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = tr.Dial(ctx)
	if err == nil {
		t.Fatal("expected error dialing unreachable host with short timeout")
	}
}

func TestTCP_Dial_ClosedPort_ReturnsError(t *testing.T) {
	// Listen then immediately close to get a port that refuses connections.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	tr, _ := transport.New(transport.Config{URL: "mud://" + addr})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := tr.Dial(ctx)
	if err == nil {
		t.Fatal("expected error dialing closed port")
	}
}

func TestTCP_Write_Close_Read(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		io.Copy(conn, conn) // echo
	}()

	tr, _ := transport.New(transport.Config{URL: "mud://" + ln.Addr().String()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	if _, err := rwc.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 4)
	rwc.Read(buf) //nolint:errcheck

	if err := rwc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second close should not panic.
	rwc.Close() //nolint:errcheck
}


// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------

// wsEchoServer starts a gorilla/websocket echo server and returns its address.
func wsEchoServer(t *testing.T) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/mud", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, msg) //nolint:errcheck
		}
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ws listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

func TestWebSocket_Dial_SendReceive(t *testing.T) {
	addr := wsEchoServer(t)

	tr, err := transport.New(transport.Config{URL: "ws://" + addr + "/mud"})
	if err != nil {
		t.Fatalf("New(ws://): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial ws://: %v", err)
	}
	defer rwc.Close()

	msg := []byte("hello websocket")
	if _, err := rwc.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 64)
	n, err := rwc.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("echo = %q, want %q", buf[:n], msg)
	}
}

func TestWebSocket_Dial_BadURL_ReturnsError(t *testing.T) {
	tr, err := transport.New(transport.Config{URL: "ws://127.0.0.1:1/mud"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = tr.Dial(ctx)
	if err == nil {
		t.Fatal("expected error dialing closed WebSocket port")
	}
}

func TestWebSocket_MultipleFrames_ReadAsStream(t *testing.T) {
	// Verify that multiple WS frames are transparently readable as a byte stream.
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/framed", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Send three separate frames.
		for _, part := range []string{"part1", "part2", "part3"} {
			conn.WriteMessage(websocket.TextMessage, []byte(part)) //nolint:errcheck
		}
	})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })

	tr, _ := transport.New(transport.Config{URL: "ws://" + ln.Addr().String() + "/framed"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	var got strings.Builder
	buf := make([]byte, 32)
	for got.Len() < 15 { // "part1part2part3" = 15 bytes
		n, err := rwc.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if err == io.EOF || err != nil {
			break
		}
	}
	if got.String() != "part1part2part3" {
		t.Errorf("stream read = %q, want %q", got.String(), "part1part2part3")
	}
}

func TestWebSocket_ConcurrentWrites_NoRace(t *testing.T) {
	addr := wsEchoServer(t)

	tr, _ := transport.New(transport.Config{URL: "ws://" + addr + "/mud"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	// Drain reader in background.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := rwc.Read(buf); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			rwc.Write([]byte(fmt.Sprintf("msg-%d", i))) //nolint:errcheck
		}()
	}
	wg.Wait()
}

func TestWebSocket_Close_SendsCloseFrame(t *testing.T) {
	closeSeen := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/closing", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, _, err := conn.ReadMessage()
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				closeSeen <- struct{}{}
				return
			}
			if err != nil {
				return
			}
		}
	})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })

	tr, _ := transport.New(transport.Config{URL: "ws://" + ln.Addr().String() + "/closing"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	rwc.Close() //nolint:errcheck

	select {
	case <-closeSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive close frame")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------


func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}
