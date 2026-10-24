package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NLstn/chunkd/internal/testutil"
)

func waitState(t *testing.T, events <-chan Event, state string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-events:
			if e.State == state {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", state)
		}
	}
}

func TestNativeProcessLifecycle(t *testing.T) {
	java := testutil.FakeJava(t)
	t.Run("parallel launches capture output and stop gracefully", func(t *testing.T) {
		s := New()
		t.Cleanup(func() { _ = s.Close() })
		dir := t.TempDir()
		events := make(chan Event, 32)
		var wg sync.WaitGroup
		results := make(chan error, 10)
		for i := 0; i < 10; i++ {
			wg.Go(func() {
				results <- s.Start(1, Launch{Java: java, Directory: dir, MemoryMB: 512}, func(e Event) { events <- e })
			})
		}
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("started %d processes", success)
		}
		waitState(t, events, "Running")
		if tail := s.Tail(1); !strings.Contains(tail, "fixture stderr") || !strings.Contains(tail, "Done (") {
			t.Fatalf("missing output: %s", tail)
		}
		if err := s.Stop(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "stopped.marker")); err != nil {
			t.Fatal("graceful stop was not delivered")
		}
		waitState(t, events, "Stopped")
	})
	t.Run("detect unexpected exit", func(t *testing.T) {
		s := New()
		t.Cleanup(func() { _ = s.Close() })
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "fixture-mode"), []byte("crash"), 0600)
		events := make(chan Event, 8)
		if err := s.Start(2, Launch{Java: java, Directory: dir, MemoryMB: 512}, func(e Event) { events <- e }); err != nil {
			t.Fatal(err)
		}
		waitState(t, events, "Crashed")
	})
	t.Run("force terminate unresponsive process", func(t *testing.T) {
		s := New()
		s.StopTimeout = 50 * time.Millisecond
		t.Cleanup(func() { _ = s.Close() })
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "fixture-mode"), []byte("ignore-stop"), 0600)
		events := make(chan Event, 8)
		if err := s.Start(3, Launch{Java: java, Directory: dir, MemoryMB: 512}, func(e Event) { events <- e }); err != nil {
			t.Fatal(err)
		}
		waitState(t, events, "Running")
		if err := s.Stop(context.Background(), 3); err == nil {
			t.Fatal("forced stop must report timeout")
		}
		waitState(t, events, "Stopped")
	})
}

func TestOutputBufferIsBounded(t *testing.T) {
	p := &process{ready: make(chan struct{})}
	_, _ = p.Write([]byte(strings.Repeat("x", 100000)))
	if len(p.tail) != 64*1024 {
		t.Fatal("output not bounded")
	}
}
