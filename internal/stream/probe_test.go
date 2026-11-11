package stream

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeRTSP answers each DESCRIBE with the status the stream name maps to and
// returns the request lines it saw.
func fakeRTSP(t *testing.T, status map[string]string) (string, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	seen := make(chan string, 8)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				seen <- strings.TrimSpace(line)
				name := line[strings.LastIndex(line[:strings.Index(line, " RTSP/")], "/")+1 : strings.Index(line, " RTSP/")]
				st, ok := status[name]
				if !ok {
					st = "404 Not Found"
				}
				_, _ = c.Write([]byte("RTSP/1.0 " + st + "\r\nCSeq: 1\r\n\r\n"))
			}()
		}
	}()
	return "rtsp://user:secret@" + ln.Addr().String(), seen
}

func TestProbe(t *testing.T) {
	base, seen := fakeRTSP(t, map[string]string{StreamName("up"): "200 OK"})
	m, err := NewManager("http://127.0.0.1:1", base)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Probe(ctx, "up"); err != nil {
		t.Fatalf("a camera that answers: %v", err)
	}
	if line := <-seen; strings.Contains(line, "secret") || !strings.HasPrefix(line, "DESCRIBE rtsp://") {
		t.Fatalf("request line %q", line)
	}
	if err := m.Probe(ctx, "down"); err == nil {
		t.Fatal("a camera go2rtc can't reach (404) passed")
	}
	dead, _ := NewManager("http://127.0.0.1:1", "rtsp://127.0.0.1:1")
	if err := dead.Probe(ctx, "up"); err == nil {
		t.Fatal("go2rtc not listening passed")
	}
}
