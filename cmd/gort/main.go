package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olmesm/gort/internal/web"
)

// version is stamped by the release build via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false,
		"probe the local /rest/health endpoint and exit (for container health checks)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("gort", version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := web.ConfigFromEnv()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	if *healthcheck {
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/rest/health", cfg.Port))
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	app, err := web.NewApp(cfg, logger)
	if err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}
