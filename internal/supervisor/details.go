package supervisor

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Details only samples processes owned by this supervisor, never persisted PIDs.
type Details struct {
	CPUPercent    *float64
	MemoryMB      *float64
	UptimeSeconds *float64
	Players       *int
	Logs          string
	LogOffset     int64
	LogReset      bool
}

func (s *Supervisor) Details(id uint, after int64) Details {
	s.mu.Lock()
	p := s.processes[id]
	active := p != nil
	if p == nil {
		p = s.finished[id]
	}
	s.mu.Unlock()
	if p == nil {
		return Details{LogReset: after != 0}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d := Details{Logs: p.tail, LogOffset: p.offset}
	start := p.offset - int64(len(p.tail))
	if after >= start && after <= p.offset {
		d.Logs = p.tail[after-start:]
	} else {
		d.LogReset = true
	}
	if !active {
		return d
	}
	select {
	case <-p.done:
		return d
	default:
	}
	uptime := time.Since(p.started).Seconds()
	d.UptimeSeconds = &uptime
	d.MemoryMB, d.CPUPercent = sampleProcess(p)
	return d
}

func (s *Supervisor) Command(id uint, command string) error {
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 1024 || strings.ContainsAny(command, "\r\n\x00") {
		return fmt.Errorf("command must be one line of 1–1024 bytes")
	}
	// Lifecycle commands must use the lifecycle actions and their state tracking.
	words := strings.Fields(strings.TrimPrefix(command, "/"))
	if len(words) == 0 {
		return fmt.Errorf("command is empty")
	}
	first := strings.ToLower(words[0])
	if first == "stop" || first == "restart" {
		return fmt.Errorf("use the Stop or Restart control")
	}
	s.mu.Lock()
	p := s.processes[id]
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("server is not running")
	}
	p.mu.Lock()
	stopping := p.stopping
	p.mu.Unlock()
	if stopping {
		return fmt.Errorf("server is stopping")
	}
	return p.sendLine(command)
}

// Bound writes so a stalled JVM cannot block API requests or shutdown forever.
func (p *process) sendLine(line string) error {
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	if f, ok := p.stdin.(*os.File); ok {
		if err := f.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
			return err
		}
		defer f.SetWriteDeadline(time.Time{})
	}
	_, err := io.WriteString(p.stdin, line+"\n")
	return err
}
