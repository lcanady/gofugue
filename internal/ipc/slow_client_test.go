package ipc_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
)

// TestIPC_SlowClient_DoesNotStarveFastClient publishes a flood of events and
// verifies that a non-reading "slow" client does not block the broadcast loop
// from delivering to a fast client. The slow client should also get force-
// disconnected after exceeding the per-client write queue limits.
func TestIPC_SlowClient_DoesNotStarveFastClient(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)

	// Subscribe helper that returns the raw conn (so we control reads).
	subscribe := func(conn net.Conn) {
		t.Helper()
		data, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "subscribe",
			"params": map[string]any{"events": []string{"world.line"}},
		})
		data = append(data, '\n')
		if _, err := conn.Write(data); err != nil {
			t.Fatalf("subscribe write: %v", err)
		}
		// Drain the ack.
		conn.SetReadDeadline(time.Now().Add(time.Second))
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			t.Fatal("no ack from subscribe")
		}
	}

	// Client A: subscribes then never reads.
	slow, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial slow: %v", err)
	}
	defer slow.Close()
	subscribe(slow)

	// Client B: subscribes and reads normally.
	fast, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial fast: %v", err)
	}
	defer fast.Close()
	subscribe(fast)

	// Let both subscriptions register.
	time.Sleep(50 * time.Millisecond)

	const N = 1000

	// Reader goroutine for the fast client — count notifications.
	gotCh := make(chan int, 1)
	go func() {
		fast.SetReadDeadline(time.Now().Add(3 * time.Second))
		sc := bufio.NewScanner(fast)
		// Larger buffer so we can handle bursts of JSON lines.
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		count := 0
		for sc.Scan() {
			count++
			if count >= N {
				break
			}
			fast.SetReadDeadline(time.Now().Add(2 * time.Second))
		}
		gotCh <- count
	}()

	// Publish a flood of events. Pace lightly so the bus's own 256-deep
	// subscriber channel (separate from the IPC per-client queue) doesn't
	// drop publishes before the IPC broadcast loop drains them. We're
	// specifically testing the IPC layer's slow-client handling here.
	// Large payload (~4 KiB) so 1000 events = ~4 MiB, well beyond the
	// kernel TCP send buffer. A non-reading slow client will back up
	// quickly and trip the IPC write queue limit.
	bigText := make([]byte, 4096)
	for i := range bigText {
		bigText[i] = 'x'
	}
	go func() {
		for i := 0; i < N; i++ {
			b.Publish(bus.WorldLineEvent{WorldName: "w", Text: string(bigText)})
			if i%64 == 63 {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	select {
	case got := <-gotCh:
		if got < N {
			t.Fatalf("fast client received %d/%d events — slow client appears to have starved it", got, N)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fast client did not receive all events within deadline — slow client is starving the broadcast")
	}

	// Slow client must eventually be force-disconnected by the server.
	// Its Read should observe EOF (or a connection error) within a short window.
	slow.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	disconnected := false
	for time.Now().Before(deadline) {
		_, err := slow.Read(buf)
		if err == io.EOF || (err != nil && !isTimeout(err)) {
			disconnected = true
			break
		}
		if err == nil {
			// Drain some data; we eventually expect EOF once the server force-closes.
			continue
		}
		// Timeout: extend and retry until overall deadline.
		slow.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	}
	if !disconnected {
		t.Fatal("slow client was not disconnected by the server after exceeding write queue")
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

