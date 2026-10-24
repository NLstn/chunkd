package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	"github.com/NLstn/chunkd/internal/api"
	"github.com/NLstn/chunkd/internal/config"
	"github.com/NLstn/chunkd/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("chunkd stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(cfg.DataDir, 0700); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(cfg.DataDir, "daemon.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("another Chunkd daemon owns this data directory")
	}
	defer lock.Close()
	db, err := storage.Open(filepath.Join(cfg.DataDir, "chunkd.db"))
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	handler, err := api.New(db, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if closer, ok := handler.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				slog.Error("stop managed processes", "error", err)
			}
		}
	}()
	srv := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP shutdown", "error", err)
			_ = srv.Close()
		}
	}()
	slog.Info("chunkd listening", "address", cfg.Listen, "setup_token_file", filepath.Join(cfg.DataDir, "setup-token"))
	listenErr := srv.ListenAndServe()
	stop()
	<-shutdownDone
	if !errors.Is(listenErr, http.ErrServerClosed) {
		return listenErr
	}
	return nil
}
