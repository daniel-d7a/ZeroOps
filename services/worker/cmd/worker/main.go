package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"zeroops/worker/internal/config"
	"zeroops/worker/internal/log"
)

func main() {
	workerID, err := os.Hostname()
	if err != nil || workerID == "" {
		workerID = "unknown"
	}
	logger := log.New(workerID)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("worker started", "node_id", cfg.NodeID, "roles", cfg.Roles)

	// M2 replaces the wait with queue consumption; the shutdown contract
	// (stop consuming, finish or requeue in-flight, exit) stays the same.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("worker stopped")
}
