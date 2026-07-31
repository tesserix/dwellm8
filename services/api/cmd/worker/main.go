// Command worker serves Dwellm8's durable operations. ADR-0015.
//
// It is a separate process from the API for the reason the standard gives about
// blast radius: a worker replaying a payout must not be competing for the same
// goroutines as a tenant loading their rent schedule, and a deploy of one should
// not interrupt the other. The API starts workflows; this process runs them.
//
// It serves one task queue per domain — mandate, collect, payout, refund,
// document, recon — so a payout backlog cannot stop a mandate being created.
//
// A process that registers nothing exits rather than idling: a worker that looks
// healthy and serves no queue is the failure this standard exists to prevent,
// because the workflows simply wait and nothing anywhere reports an error.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow/temporalx"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)

	// Refused rather than defaulted: a money operation may not fall back to
	// running inline, because that is a payout with no compensation and no
	// record, and nothing would error.
	if err := cfg.Workflow.Validate(); err != nil {
		return err
	}

	client, err := temporalx.Dial(cfg.Workflow, logger)
	if err != nil {
		return err
	}
	defer client.Close()

	registry := temporalx.NewRegistry()
	workers := temporalx.NewWorkers(client, logger)

	// Operations are registered as they are implemented; #80 is the first.
	// Until then this process refuses to start rather than pretending to serve
	// the queues, which is what Start's empty check is for.
	if err := workers.Mount(registry); err != nil {
		return err
	}
	if err := workers.Start(); err != nil {
		return fmt.Errorf("%w — register an operation before deploying this, or the queues "+
			"it should serve have no worker and every workflow on them waits forever", err)
	}
	defer workers.Stop()

	logger.Info("worker ready",
		"version", version,
		"namespace", cfg.Workflow.Namespace,
		"queues", workers.Queues(),
		"operations", len(registry.Definitions()))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	sig := <-stop

	// Stop drains: activities already running finish, and Temporal redelivers
	// anything this worker had not started to another replica.
	logger.Info("signal received, draining", "signal", sig.String())
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
