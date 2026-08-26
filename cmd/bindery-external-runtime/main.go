package main

import (
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
	"github.com/bayleafwalker/bindery-core/pkg/relayv1"
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
	service, err := externalruntime.OpenPersistentService(newRelayAllocator(config), store)
	if err != nil {
		logger.Error("durable control state could not be restored", "error", err)
		os.Exit(2)
	}

	// The native provider is the only one this process can admit clients to.
	// For cncnet-private the tunnel is operated out of band, so the placement
	// record names it and nothing here registers against it.
	if config.Provider == binderyNativeProviderID {
		native := newNativeRelay(config, logger)
		if err := native.start(); err != nil {
			logger.Error("native relay could not start", "error", err)
			os.Exit(2)
		}
		bind, err := listenAddress(config.Endpoint)
		if err != nil {
			logger.Error("native relay listen address is invalid", "error", err)
			os.Exit(2)
		}
		connection, err := net.ListenUDP("udp", bind)
		if err != nil {
			logger.Error("native relay udp listen failed", "addr", bind.String(), "error", err)
			os.Exit(2)
		}
		defer connection.Close()
		// Installed before the HTTP server accepts a request. An enrollment
		// served without the admitter would discard a transport key that can
		// never be recovered, leaving that client permanently unable to reach
		// the relay its own placement names.
		service.SetRelayAdmitter(native.admitter())
		go native.serve(connection, envInt("BINDERY_RELAY_DATAGRAM_LIMIT", relayv1.DefaultDatagramLimit))
		logger.Info("native relay listening", "addr", bind.String(), "advertised_endpoint", config.Endpoint)
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
