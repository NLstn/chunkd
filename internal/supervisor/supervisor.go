package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Event struct {
	State   string
	PID     int64
	Message string
}
type Launch struct {
	Java      string
	Directory string
	MemoryMB  int64
}
type process struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	ready    chan struct{}
	once     sync.Once
	mu       sync.Mutex
	tail     string
	stopping bool
}
type Supervisor struct {
	mu          sync.Mutex
	processes   map[uint]*process
	StopTimeout time.Duration
	closed      bool
	wg          sync.WaitGroup
}

func New() *Supervisor {
	return &Supervisor{processes: map[uint]*process{}, StopTimeout: 30 * time.Second}
}

// Start launches Java directly with fixed argument boundaries, never a shell.
func (s *Supervisor) Start(id uint, l Launch, onEvent func(Event)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("supervisor is closed")
	}
	if _, ok := s.processes[id]; ok {
		return fmt.Errorf("server process already exists")
	}
	cmd := exec.Command(l.Java, fmt.Sprintf("-Xms%dM", min(l.MemoryMB, 512)), fmt.Sprintf("-Xmx%dM", l.MemoryMB), "-jar", "server.jar", "nogui")
	cmd.Dir = l.Directory
	cmd.WaitDelay = 5 * time.Second
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	p := &process{cmd: cmd, stdin: stdin, done: make(chan struct{}), ready: make(chan struct{})}
	cmd.Stdout = p
	cmd.Stderr = p
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return err
	}
	s.processes[id] = p
	onEvent(Event{State: "Starting", PID: int64(cmd.Process.Pid)})
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		err := cmd.Wait()
		_ = stdin.Close()
		p.mu.Lock()
		stopping := p.stopping
		p.mu.Unlock()
		event := Event{State: "Stopped", PID: int64(cmd.Process.Pid)}
		if err != nil && !stopping {
			event.State = "Crashed"
			event.Message = err.Error()
		}
		if !stopping {
			select {
			case <-p.ready:
			default:
				event.State = "Crashed"
				if event.Message == "" {
					event.Message = "Process exited before Minecraft reported ready"
				}
			}
		}
		s.mu.Lock()
		delete(s.processes, id)
		s.mu.Unlock()
		onEvent(event)
		close(p.done)
	}()
	go func() {
		defer s.wg.Done()
		select {
		case <-p.ready:
			s.mu.Lock()
			current := s.processes[id] == p
			s.mu.Unlock()
			if current {
				onEvent(Event{State: "Running", PID: int64(cmd.Process.Pid)})
			}
		case <-p.done:
		}
	}()
	return nil
}

// Write keeps a bounded output tail and recognizes Minecraft's ready message.
// stdout and stderr may call Write concurrently.
func (p *process) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tail += string(b)
	if len(p.tail) > 64*1024 {
		p.tail = p.tail[len(p.tail)-64*1024:]
	}
	if strings.Contains(p.tail, "Done (") && strings.Contains(p.tail, "For help, type") {
		p.once.Do(func() { close(p.ready) })
	}
	return len(b), nil
}

func (s *Supervisor) Stop(ctx context.Context, id uint) error {
	s.mu.Lock()
	p := s.processes[id]
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	_, writeErr := io.WriteString(p.stdin, "stop\n")
	timer := time.NewTimer(s.StopTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
	case <-timer.C:
	}
	killErr := killProcess(p.cmd)
	// Wait for reaping before releasing the server for a subsequent start.
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		return errors.New("process did not exit after forced termination")
	}
	if killErr != nil {
		return fmt.Errorf("forced termination: %w", killErr)
	}
	if writeErr != nil {
		return fmt.Errorf("graceful stop failed: %w", writeErr)
	}
	return errors.New("graceful stop timed out; process was forcibly terminated")
}

func (s *Supervisor) Tail(id uint) string {
	s.mu.Lock()
	p := s.processes[id]
	s.mu.Unlock()
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tail
}

func (s *Supervisor) Close() error {
	s.mu.Lock()
	s.closed = true
	ids := []uint{}
	for id := range s.processes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var result error
	for _, id := range ids {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			if err := s.Stop(ctx, id); err != nil {
				mu.Lock()
				result = errors.Join(result, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	s.wg.Wait()
	return result
}
