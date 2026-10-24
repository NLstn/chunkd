package minecraft

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/installer"
	"github.com/NLstn/chunkd/internal/javaruntime"
	"github.com/NLstn/chunkd/internal/supervisor"
	"gorm.io/gorm"
)

var ErrConflict = errors.New("server operation conflicts with current state")
var ErrInvalid = errors.New("invalid server configuration")
var ErrNotFound = errors.New("server not found")

type Downloads interface {
	Versions(context.Context) ([]installer.Version, error)
	Resolve(context.Context, string, string) (installer.Artifact, error)
	Download(context.Context, installer.Artifact, string) error
}
type CreateInput struct {
	Name         string
	Software     string
	Version      string
	JavaPath     string
	MemoryMB     int64
	Port         int64
	EULAAccepted bool
}
type Manager struct {
	db        *gorm.DB
	root      string
	Downloads Downloads
	Processes *supervisor.Supervisor
	mu        sync.Mutex
	busy      map[uint]bool
	closed    bool
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

func New(db *gorm.DB, dataDir string) (*Manager, error) {
	root, err := filepath.Abs(filepath.Join(dataDir, "servers"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("server root must be a real directory")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{db: db, root: root, Downloads: installer.New(), Processes: supervisor.New(), busy: map[uint]bool{}, ctx: ctx, cancel: cancel}
	// M2 never adopts or kills stale PIDs. Unknown ownership blocks launch until
	// explicit offline operator reconciliation; full recovery is C-17.
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.Server{}).Where("state IN ?", []string{"Starting", "Running", "Stopping"}).Updates(map[string]any{"state": "Unknown", "last_error": "Daemon restarted without confirmed process ownership; reconcile offline before starting"}).Error; err != nil {
			return err
		}
		if err := tx.Model(&domain.Server{}).Where("state = ?", "Installing").Updates(map[string]any{"state": "Failed", "last_error": "Installation interrupted; retry installation"}).Error; err != nil {
			return err
		}
		return tx.Model(&domain.Job{}).Where("state IN ?", []string{"Queued", "Running"}).Updates(map[string]any{"state": "Failed", "message": "Daemon restarted before operation completed", "completed_at": time.Now().UTC()}).Error
	})
	if err != nil {
		cancel()
		return nil, err
	}
	return m, nil
}

func (m *Manager) Versions(ctx context.Context) ([]installer.Version, error) {
	return m.Downloads.Versions(ctx)
}

func (m *Manager) Create(ctx context.Context, input CreateInput) (domain.Job, error) {
	name := strings.TrimSpace(input.Name)
	minimum, err := installer.MinimumJava(input.Version)
	if err != nil {
		return domain.Job{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "\n\r\x00") || (input.Software != "vanilla" && input.Software != "paper") || input.MemoryMB < 512 || input.MemoryMB > 65536 || input.Port < 1024 || input.Port > 65535 || !input.EULAAccepted {
		return domain.Job{}, fmt.Errorf("%w: name, software, memory (512–65536 MiB), port (1024–65535) and explicit EULA acceptance are required", ErrInvalid)
	}
	if _, err := javaruntime.Validate(ctx, input.JavaPath, minimum); err != nil {
		return domain.Job{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.busy) >= 4 {
		return domain.Job{}, ErrConflict
	}
	var count int64
	if err := m.db.Model(&domain.Server{}).Where("port = ?", input.Port).Count(&count).Error; err != nil {
		return domain.Job{}, err
	}
	if count > 0 {
		return domain.Job{}, fmt.Errorf("%w: port already assigned", ErrConflict)
	}
	s := domain.Server{Name: name, State: "Installing", Software: input.Software, Version: input.Version, JavaPath: input.JavaPath, MemoryMB: input.MemoryMB, Port: input.Port, EULAAccepted: true, RequiredJava: minimum}
	if err := m.db.Create(&s).Error; err != nil {
		return domain.Job{}, err
	}
	job, err := m.enqueueLocked(s, "Install", func(ctx context.Context) error { return m.install(ctx, s) })
	if err != nil {
		_ = m.db.Delete(&s).Error
	}
	return job, err
}

func (m *Manager) Action(id uint, action string) (domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.busy[id] || len(m.busy) >= 4 {
		return domain.Job{}, ErrConflict
	}
	var s domain.Server
	if err := m.db.First(&s, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Job{}, ErrNotFound
		}
		return domain.Job{}, err
	}
	switch action {
	case "Start":
		if s.State != "Stopped" && s.State != "Crashed" {
			return domain.Job{}, ErrConflict
		}
	case "Stop", "Restart":
		if s.State != "Running" && s.State != "Starting" {
			return domain.Job{}, ErrConflict
		}
	case "RetryInstall":
		if s.State != "Failed" {
			return domain.Job{}, ErrConflict
		}
	default:
		return domain.Job{}, ErrInvalid
	}
	return m.enqueueLocked(s, action, func(ctx context.Context) error {
		switch action {
		case "RetryInstall":
			return m.install(ctx, s)
		case "Stop":
			return m.stop(ctx, s)
		case "Restart":
			if err := m.stop(ctx, s); err != nil {
				return err
			}
			return m.start(ctx, s)
		default:
			return m.start(ctx, s)
		}
	})
}

func (m *Manager) enqueueLocked(s domain.Server, kind string, work func(context.Context) error) (domain.Job, error) {
	job := domain.Job{Kind: kind, State: "Queued", ServerID: s.ID}
	if err := m.db.Create(&job).Error; err != nil {
		return job, err
	}
	m.busy[s.ID] = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); delete(m.busy, s.ID); m.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(m.ctx, 8*time.Minute)
		defer cancel()
		err := m.db.Model(&domain.Job{}).Where("id = ?", job.ID).Update("state", "Running").Error
		if err == nil {
			err = work(ctx)
		}
		state, message := "Succeeded", ""
		if err != nil {
			state = "Failed"
			message = err.Error()
			_ = m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Update("last_error", message).Error
		}
		if err := m.db.Model(&domain.Job{}).Where("id = ?", job.ID).Updates(map[string]any{"state": state, "message": message, "completed_at": time.Now().UTC()}).Error; err != nil {
			slog.Error("persist operation result", "job", job.ID, "error", err)
		}
	}()
	return job, nil
}

func (m *Manager) install(ctx context.Context, s domain.Server) (result error) {
	defer func() {
		if result != nil {
			_ = m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Failed", "last_error": result.Error()}).Error
		}
	}()
	if err := m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Installing", "last_error": ""}).Error; err != nil {
		return err
	}
	a, err := m.Downloads.Resolve(ctx, s.Software, s.Version)
	if err != nil {
		return err
	}
	if _, err := javaruntime.Validate(ctx, s.JavaPath, a.RequiredJava); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(m.root, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := m.Downloads.Download(ctx, a, staging); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "eula.txt"), []byte("# Explicitly accepted by the local administrator in Chunkd\neula=true\n"), 0600); err != nil {
		return err
	}
	properties := fmt.Sprintf("server-port=%d\nonline-mode=true\nenable-query=false\nenable-rcon=false\n", s.Port)
	if err := os.WriteFile(filepath.Join(staging, "server.properties"), []byte(properties), 0600); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(staging, m.directory(s.ID)); err != nil {
		return err
	}
	return m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Stopped", "build": a.Build, "artifact_hash": a.Hash, "hash_algorithm": a.Algorithm, "required_java": a.RequiredJava, "last_error": ""}).Error
}

func (m *Manager) directory(id uint) string {
	return filepath.Join(m.root, strconv.FormatUint(uint64(id), 10))
}

func (m *Manager) start(ctx context.Context, s domain.Server) error {
	if _, err := javaruntime.Validate(ctx, s.JavaPath, s.RequiredJava); err != nil {
		return err
	}
	dir := m.directory(s.ID)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("server directory is unsafe")
	}
	jar := filepath.Join(dir, "server.jar")
	info, err = os.Lstat(jar)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("server jar must be a regular file")
	}
	if err := installer.VerifyFile(jar, s.HashAlgorithm, s.ArtifactHash); err != nil {
		return err
	}
	if !s.EULAAccepted {
		return fmt.Errorf("EULA acceptance is required")
	}
	if err := m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Starting", "last_error": ""}).Error; err != nil {
		return err
	}
	err = m.Processes.Start(s.ID, supervisor.Launch{Java: s.JavaPath, Directory: dir, MemoryMB: s.MemoryMB}, func(e supervisor.Event) {
		query := m.db.Model(&domain.Server{}).Where("id = ?", s.ID)
		values := map[string]any{"state": e.State, "pid": e.PID, "last_error": e.Message}
		if e.State == "Running" {
			query = query.Where("state = ? AND pid = ?", "Starting", e.PID)
		}
		if e.State == "Stopped" || e.State == "Crashed" {
			query = query.Where("pid = ?", e.PID)
			values["pid"] = 0
		}
		if err := query.Updates(values).Error; err != nil {
			slog.Error("persist process event", "server", s.ID, "error", err)
		}
	})
	if err != nil {
		_ = m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Crashed", "pid": 0, "last_error": err.Error()}).Error
	}
	return err
}

func (m *Manager) stop(ctx context.Context, s domain.Server) error {
	if err := m.db.Model(&domain.Server{}).Where("id = ?", s.ID).Update("state", "Stopping").Error; err != nil {
		return err
	}
	err := m.Processes.Stop(ctx, s.ID)
	if err == nil {
		return m.db.Model(&domain.Server{}).Where("id = ? AND state = ?", s.ID, "Stopping").Updates(map[string]any{"state": "Stopped", "pid": 0}).Error
	}
	if err != nil {
		_ = m.db.Model(&domain.Server{}).Where("id = ? AND state = ?", s.ID, "Stopping").Updates(map[string]any{"state": "Unknown", "last_error": err.Error()}).Error
	}
	return err
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return m.Processes.Close()
}
