package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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
	srv := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP shutdown", "error", err)
		}
	}()
	slog.Info("chunkd listening", "address", cfg.Listen, "setup_token_file", filepath.Join(cfg.DataDir, "setup-token"))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
