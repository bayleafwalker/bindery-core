package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
)

func main() {
	addr := os.Getenv("BINDERY_EXTERNAL_RUNTIME_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Without an allocator the service admits sessions with no placement, so
	// clients never receive a relay endpoint and no live match can happen.
	config, err := allocatorConfigFromEnv()
	if err != nil {
		logger.Error("placement allocator is not configured", "error", err)
		os.Exit(2)
	}
	statePath := os.Getenv("BINDERY_STATE_PATH")
	store, err := externalruntime.NewFileStateStore(statePath)
	if err != nil {
		logger.Error("durable control state is not configured", "error", err)
		os.Exit(2)
	}
	service, err := externalruntime.OpenPersistentService(newCncNetPrivateAllocator(config), store)
	if err != nil {
		logger.Error("durable control state could not be restored", "error", err)
		os.Exit(2)
	}
	server := &http.Server{Addr: addr, Handler: externalruntime.NewHandler(service)}
	logger.Info("bindery external-runtime reference service listening",
		"addr", addr,
		"relay_provider", config.Provider,
		"relay_endpoint", config.Endpoint,
		"region", config.Region,
		"policy_version", config.PolicyVersion,
		"allocator_revision", config.Revision,
		"allocator_config_digest", config.ConfigDigest,
		"state_path", statePath)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
