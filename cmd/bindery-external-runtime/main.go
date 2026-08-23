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
	server := &http.Server{Addr: addr, Handler: externalruntime.NewHandler(externalruntime.NewService())}
	logger.Info("bindery external-runtime reference service listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
