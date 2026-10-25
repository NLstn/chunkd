package supervisor

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NLstn/chunkd/internal/testutil"
)

func TestDetailsBoundedIncrementalLogs(t *testing.T) {
	p := &process{done: make(chan struct{})}
	s := New()
	s.finished[1] = p
	p.Write([]byte(strings.Repeat("x", 70000)))
	first := s.Details(1, 0)
	if len(first.Logs) != 65536 || !first.LogReset || first.LogOffset != 70000 {
		t.Fatalf("bad bounded tail: len=%d offset=%d reset=%v", len(first.Logs), first.LogOffset, first.LogReset)
	}
	p.Write([]byte("next line\n"))
	next := s.Details(1, first.LogOffset)
	if next.Logs != "next line\n" || next.LogReset {
		t.Fatalf("bad incremental result: %+v", next)
	}
	if s.Details(1, next.LogOffset).Logs != "" {
		t.Fatal("duplicate logs")
	}
	if next.MemoryMB != nil || next.CPUPercent != nil || next.UptimeSeconds != nil {
		t.Fatal("finished process has live metrics")
	}
}

func TestDetailsProcessAndCommands(t *testing.T) {
	java := testutil.FakeJava(t)
	s := New()
	defer s.Close()
	events := make(chan Event, 32)
	launch := Launch{Java: java, Directory: t.TempDir(), MemoryMB: 512}
	start := func() {
		t.Helper()
		if err := s.Start(1, launch, func(e Event) { events <- e }); err != nil {
			t.Fatal(err)
		}
		waitState(t, events, "Running")
	}
	start()
	d := s.Details(1, 0)
	if d.UptimeSeconds == nil {
		t.Fatal("missing uptime")
	}
	for _, bad := range []string{"/", "stop", "/restart", "say hi\nstop", strings.Repeat("a", 1025)} {
		if err := s.Command(1, bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if err := s.Command(1, "say hello"); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	stopped := s.Details(1, d.LogOffset)
	if stopped.UptimeSeconds != nil {
		t.Fatal("stopped process has uptime")
	}
	start()
	fresh := s.Details(1, stopped.LogOffset)
	if !fresh.LogReset || !strings.Contains(fresh.Logs, "Done (") {
		t.Fatal("restart failed to reset cursor")
	}
	time.Sleep(10 * time.Millisecond)
	sample := s.Details(1, fresh.LogOffset)
	if runtime.GOOS == "linux" && (sample.MemoryMB == nil || sample.CPUPercent == nil || *sample.MemoryMB <= 0 || *sample.CPUPercent < 0) {
		t.Fatalf("missing Linux metrics: %+v", sample)
	}
}
