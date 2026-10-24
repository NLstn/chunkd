package minecraft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/installer"
	"github.com/NLstn/chunkd/internal/storage"
	"github.com/NLstn/chunkd/internal/testutil"
)

type fixtureDownloads struct{}

const fixtureJar = "deterministic local fixture"

func (fixtureDownloads) Versions(context.Context) ([]installer.Version, error) {
	return []installer.Version{{ID: "1.21.11"}}, nil
}
func (fixtureDownloads) Resolve(_ context.Context, _ string, version string) (installer.Artifact, error) {
	if version == "1.21.10" {
		return installer.Artifact{}, fmt.Errorf("fixture upstream unavailable")
	}
	h := sha256.Sum256([]byte(fixtureJar))
	return installer.Artifact{Hash: hex.EncodeToString(h[:]), Algorithm: "sha256", Size: int64(len(fixtureJar)), RequiredJava: 25}, nil
}
func (fixtureDownloads) Download(_ context.Context, _ installer.Artifact, dir string) error {
	return os.WriteFile(filepath.Join(dir, "server.jar"), []byte(fixtureJar), 0600)
}

func awaitJob(t *testing.T, m *Manager, id uint) domain.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var j domain.Job
		if err := m.db.First(&j, id).Error; err != nil {
			t.Fatal(err)
		}
		if j.State == "Succeeded" || j.State == "Failed" {
			m.mu.Lock()
			busy := m.busy[j.ServerID]
			m.mu.Unlock()
			if !busy {
				return j
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not complete")
	return domain.Job{}
}

func awaitServer(t *testing.T, m *Manager, id uint, state string) domain.Server {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var s domain.Server
		if err := m.db.First(&s, id).Error; err != nil {
			t.Fatal(err)
		}
		if s.State == state {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not reach %s", state)
	return domain.Server{}
}

func TestProvisionAndControlNativeServer(t *testing.T) {
	java := testutil.FakeJava(t)
	t.Setenv("CHUNKD_JAVA_PATHS", java)
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	m, err := New(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Downloads = fixtureDownloads{}
	t.Cleanup(func() { _ = m.Close() })
	input := CreateInput{Name: "Test world", Software: "vanilla", Version: "1.21.11", JavaPath: java, MemoryMB: 512, Port: 25565, EULAAccepted: true}
	bad := input
	bad.EULAAccepted = false
	if _, err := m.Create(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted missing EULA consent")
	}
	bad = input
	bad.Port = 80
	if _, err := m.Create(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted privileged port")
	}
	job, err := m.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if j := awaitJob(t, m, job.ID); j.State != "Succeeded" {
		t.Fatalf("install: %+v", j)
	}
	s := awaitServer(t, m, job.ServerID, "Stopped")
	properties, _ := os.ReadFile(filepath.Join(m.directory(s.ID), "server.properties"))
	if string(properties) != "server-port=25565\nonline-mode=true\nenable-query=false\nenable-rcon=false\n" {
		t.Fatal("unsafe or incorrect generated properties")
	}
	if _, err := m.Create(context.Background(), input); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate port accepted: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Go(func() { _, err := m.Action(s.ID, "Start"); results <- err })
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d parallel starts", accepted)
	}
	running := awaitServer(t, m, s.ID, "Running")
	if running.PID <= 0 {
		t.Fatal("missing live PID")
	}
	// Wait for the launch job to leave the per-server operation gate.
	var startJob domain.Job
	_ = db.Where("server_id = ? AND kind = ?", s.ID, "Start").First(&startJob).Error
	_ = awaitJob(t, m, startJob.ID)
	restart, err := m.Action(s.ID, "Restart")
	if err != nil {
		t.Fatal(err)
	}
	if j := awaitJob(t, m, restart.ID); j.State != "Succeeded" {
		t.Fatalf("restart: %+v", j)
	}
	running = awaitServer(t, m, s.ID, "Running")
	stop, err := m.Action(s.ID, "Stop")
	if err != nil {
		t.Fatal(err)
	}
	if j := awaitJob(t, m, stop.ID); j.State != "Succeeded" {
		t.Fatalf("stop: %+v", j)
	}
	stopped := awaitServer(t, m, s.ID, "Stopped")
	if stopped.PID != 0 {
		t.Fatal("stale PID after stop")
	}
	if _, err := os.Stat(filepath.Join(m.directory(s.ID), "stopped.marker")); err != nil {
		t.Fatal("stop command did not reach process stdin")
	}
	if err := os.WriteFile(filepath.Join(m.directory(s.ID), "server.jar"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	start, err := m.Action(s.ID, "Start")
	if err != nil {
		t.Fatal(err)
	}
	if j := awaitJob(t, m, start.ID); j.State != "Failed" {
		t.Fatal("launched modified jar")
	}
	if _, err := m.Action(9999, "Start"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing server accepted")
	}
	// Installation failure is explicit, retryable, and never leaves a final jar.
	input.Port = 25566
	input.Version = "1.21.10"
	failed, err := m.Create(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if j := awaitJob(t, m, failed.ID); j.State != "Failed" {
		t.Fatal("failed download reported success")
	}
	_ = awaitServer(t, m, failed.ServerID, "Failed")
	if _, err := os.Stat(m.directory(failed.ServerID)); !os.IsNotExist(err) {
		t.Fatal("failed install left final server directory")
	}
	retry, err := m.Action(failed.ServerID, "RetryInstall")
	if err != nil {
		t.Fatal(err)
	}
	_ = awaitJob(t, m, retry.ID)
	// Ambiguous ownership remains blocked instead of treating a stale PID as safe.
	if err := db.Model(&domain.Server{}).Where("id = ?", s.ID).Updates(map[string]any{"state": "Running", "pid": 44444}).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m2, err := New(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	_ = awaitServer(t, m2, s.ID, "Unknown")
	if _, err := m2.Action(s.ID, "Start"); !errors.Is(err, ErrConflict) {
		t.Fatal("launched while process ownership was ambiguous")
	}
}
