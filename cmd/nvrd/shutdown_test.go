package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestServicesWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &services{ctx: ctx}
	release := make(chan struct{})
	s.run("prompt", func(ctx context.Context) { <-ctx.Done() })
	s.run("stuck", func(context.Context) { <-release })
	s.run("finishing", func(ctx context.Context) { <-ctx.Done(); time.Sleep(50 * time.Millisecond) })

	cancel()
	start := time.Now()
	pending := s.wait(300 * time.Millisecond)
	if took := time.Since(start); took < 300*time.Millisecond || took > 2*time.Second {
		t.Fatalf("wait took %s, want its 300ms bound", took)
	}
	if want := []string{"stuck"}; !reflect.DeepEqual(pending, want) {
		t.Fatalf("pending = %v, want %v", pending, want)
	}

	close(release)
	if pending := s.wait(time.Second); pending != nil {
		t.Fatalf("all returned, still pending: %v", pending)
	}
	if name, took := s.slowest(start); name != "stuck" || took < 300*time.Millisecond {
		t.Fatalf("slowest = %s after %s, want stuck after >300ms", name, took)
	}
}
