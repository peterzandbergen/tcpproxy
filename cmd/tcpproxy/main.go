package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/myhops/tcpproxy/internal/proxy"
)

// Build information set by ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func runProxies(ctx context.Context, config *Config) error {
	var proxies []*proxy.Proxy
	for _, proxyCfg := range config.Proxies {
		// Create a config
		cfg := proxy.ProxyConfig{
			Name:       fmt.Sprintf("%s:%s", proxyCfg.LocalPort, proxyCfg.RemoteAddr),
			Port:       proxyCfg.LocalPort,
			RemoteAddr: proxyCfg.RemoteAddr,
		}
		// Create a proxy
		pr := proxy.NewProxy(cfg)
		proxies = append(proxies, pr)
	}

	ps := proxy.NewProxies(proxies)
	return ps.ListenAndServe(ctx)
}

func InitLogger(cfg *Config) *slog.Logger {
	// 1. Determine Level
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	// 2. Determine Format (JSON vs Text)
	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// 3. Create the Logger
	logger := slog.New(handler)

	return logger
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	cfg := LoadConfig(args, getenv)
	logger := InitLogger(cfg).With("application", "tcpproxy", "version", version)
	slog.SetDefault(logger)

	return runProxies(ctx, cfg)
}

func printVersion() {
	fmt.Printf("tcpproxy %s\n", version)
	fmt.Printf("  commit: %s\n", commit)
	fmt.Printf("  built:  %s\n", date)
}

func main() {
	// Handle --version flag early
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		printVersion()
		return
	}

	// create a signal aware context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		err := run(ctx, os.Args, os.Getenv)
		if err != nil {
			slog.Error("application exited with error", "error", err)
		}
		slog.Info("application shutting down")
	}()

	// Wait for interrupt signal or run finished
	select {
	case <-runDone:
		slog.Info("application run completed")
	case <-ctx.Done():
		// We received a signal (Ctrl+C)
		slog.Info("signal received, shutting down proxies...")

		// 4. Graceful Shutdown Timeout
		// We now wait for runDone (the actual cleanup) with a timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		select {
		case <-runDone:
			slog.Info("shutdown complete")
		case <-shutdownCtx.Done():
			slog.Warn("shutdown timed out - forcing exit")
		}
	}
}
