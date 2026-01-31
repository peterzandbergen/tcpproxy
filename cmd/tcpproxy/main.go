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
	"github.com/myhops/tcpproxy/internal/slogmulti"
	tel "github.com/myhops/tcpproxy/internal/telemetry"
	"go.opentelemetry.io/contrib/bridges/otelslog"
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

	// 2. Determine stderr handler format (JSON vs Text)
	var stderrHandler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		stderrHandler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		stderrHandler = slog.NewTextHandler(os.Stderr, opts)
	}

	// 3. If otel format, fanout to both stderr and otel
	if strings.ToLower(cfg.LogFormat) == "otel" {
		otelHandler := otelslog.NewHandler("tcpproxy")
		return slog.New(slogmulti.New(stderrHandler, otelHandler))
	}

	return slog.New(stderrHandler)
}

func run(ctx context.Context, args []string, getenv func(string) string) {
	// Set up a basic stderr logger for early startup errors
	earlyLogger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(earlyLogger)

	// Handle --version flag early
	if len(args) > 1 && (args[1] == "--version" || args[1] == "-v") {
		printVersion()
		return
	}

	cfg, err := LoadConfig(args, getenv)
	if err != nil {
		slog.ErrorContext(ctx, "Error parsing flags", "error", err)
		return
	}

	// Set up telemetry shutdown function (may be nil if telemetry disabled)
	var shutdownTelemetry func(context.Context) error

	if cfg.TelemetryEnabled {
		telCfg := tel.Config{
			ServiceName:    "tcpproxy",
			ServiceVersion: version,
			OTLPEndpoint:   cfg.TelemetryEndpoint,
			EnableTraces:   true,
			EnableMetrics:  true,
			EnableLogs:     true,
			UseStdout:      cfg.TelemetryExporter == "stdout",
		}
		_, shutdownTelemetry, err = tel.Setup(ctx, telCfg)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to setup telemetry", "error", err)
			return
		}
	}

	// Ensure telemetry is shut down on exit
	defer func() {
		if shutdownTelemetry != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			slog.InfoContext(ctx, "Shutting down telemetry")
			if err := shutdownTelemetry(shutdownCtx); err != nil {
				slog.ErrorContext(ctx, "Telemetry shutdown error", "error", err)
			}
		}
	}()

	// Initialize the application logger
	// When telemetry is enabled with stdout exporter, use otel logger format
	// Otherwise, logs go to stderr (text or json format)
	logger := InitLogger(cfg).With("application", "tcpproxy", "version", version)
	slog.SetDefault(logger)

	// Create a signal aware context
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		err := runProxies(ctx, cfg)
		if err != nil {
			slog.Error("application exited with error", "error", err)
		}
		slog.InfoContext(ctx, "application shutting down")
	}()

	// Wait for interrupt signal or run finished
	select {
	case <-runDone:
		slog.InfoContext(ctx, "application run completed")
	case <-ctx.Done():
		// We received a signal (Ctrl+C)
		slog.InfoContext(ctx, "signal received, shutting down proxies...")

		// Graceful Shutdown Timeout
		// We now wait for runDone (the actual cleanup) with a timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		select {
		case <-runDone:
			slog.InfoContext(ctx, "shutdown complete")
		case <-shutdownCtx.Done():
			slog.WarnContext(ctx, "shutdown timed out - forcing exit")
		}
	}
}

func printVersion() {
	fmt.Printf("tcpproxy %s\n", version)
	fmt.Printf("  commit: %s\n", commit)
	fmt.Printf("  built:  %s\n", date)
}

func main() {
	run(context.Background(), os.Args, os.Getenv)
}
