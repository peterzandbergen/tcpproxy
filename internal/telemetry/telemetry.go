// Package telemetry provides OpenTelemetry instrumentation setup for the tcpproxy.
// It configures OTLP exporters for traces, metrics, and logs.
package telemetry

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds configuration options for the telemetry setup.
type Config struct {
	// ServiceName is the name of the service reported to the telemetry backend.
	ServiceName string

	// ServiceVersion is the version of the service.
	ServiceVersion string

	// OTLPEndpoint is the endpoint for the OTLP collector (e.g., "localhost:4317").
	// If empty, defaults to the OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
	OTLPEndpoint string

	// EnableTraces enables trace collection. Default is true.
	EnableTraces bool

	// EnableMetrics enables metric collection. Default is true.
	EnableMetrics bool

	// EnableLogs enables log collection. Default is true.
	EnableLogs bool

	// MetricInterval is the interval at which metrics are exported.
	// Default is 60 seconds.
	MetricInterval time.Duration

	// UseStdout uses stdout exporters instead of OTLP for debugging.
	// Useful for local development without an OTLP collector.
	UseStdout bool

	// StdoutWriter is the writer for stdout exporters. Defaults to os.Stderr.
	StdoutWriter io.Writer
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig(serviceName string) Config {
	return Config{
		ServiceName:    serviceName,
		ServiceVersion: "0.0.0",
		EnableTraces:   true,
		EnableMetrics:  true,
		EnableLogs:     true,
		MetricInterval: 60 * time.Second,
	}
}

// SDKDisabled returns true if the OTEL_SDK_DISABLED environment variable
// is set to "true" (case-insensitive), per the OpenTelemetry specification.
// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
func SDKDisabled() bool {
	return strings.EqualFold(os.Getenv("OTEL_SDK_DISABLED"), "true")
}

// Telemetry holds the initialized OpenTelemetry providers.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
}

// Setup initializes the OpenTelemetry SDK with OTLP exporters.
// It returns a Telemetry struct containing the providers and a shutdown function.
// The shutdown function should be called when the application exits to ensure
// all telemetry data is flushed.
//
// If the OTEL_SDK_DISABLED environment variable is set to "true", Setup returns
// nil providers and a no-op shutdown function, per the OpenTelemetry specification.
// Note: Propagators are always configured regardless of OTEL_SDK_DISABLED, as per spec.
func Setup(ctx context.Context, cfg Config) (*Telemetry, func(context.Context) error, error) {
	// Set up propagator for distributed tracing context.
	// This is done before checking OTEL_SDK_DISABLED because the spec states:
	// "This setting does not affect propagators configured through OTEL_PROPAGATORS."
	// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Check OTEL_SDK_DISABLED per OpenTelemetry specification.
	if SDKDisabled() {
		return nil, func(context.Context) error { return nil }, nil
	}

	var shutdownFuncs []func(context.Context) error
	var tel Telemetry

	// shutdown aggregates all shutdown functions and returns combined errors.
	shutdown := func(ctx context.Context) error {
		var errs []error
		for _, fn := range shutdownFuncs {
			if err := fn(ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	// handleErr ensures cleanup on setup failure.
	handleErr := func(inErr error) (*Telemetry, func(context.Context) error, error) {
		if err := shutdown(ctx); err != nil {
			return nil, nil, errors.Join(inErr, err)
		}
		return nil, nil, inErr
	}

	// Build the resource describing this service.
	res, err := newResource(ctx, cfg.ServiceName, cfg.ServiceVersion)
	if err != nil {
		return handleErr(err)
	}

	// Set up trace provider.
	if cfg.EnableTraces {
		tracerProvider, err := newTracerProvider(ctx, res, cfg)
		if err != nil {
			return handleErr(err)
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)
		tel.TracerProvider = tracerProvider
	}

	// Set up meter provider.
	if cfg.EnableMetrics {
		meterProvider, err := newMeterProvider(ctx, res, cfg)
		if err != nil {
			return handleErr(err)
		}
		shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
		otel.SetMeterProvider(meterProvider)
		tel.MeterProvider = meterProvider
	}

	// Set up logger provider.
	if cfg.EnableLogs {
		loggerProvider, err := newLoggerProvider(ctx, res, cfg)
		if err != nil {
			return handleErr(err)
		}
		shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
		global.SetLoggerProvider(loggerProvider)
		tel.LoggerProvider = loggerProvider
	}

	return &tel, shutdown, nil
}

// newResource creates an OpenTelemetry resource with service information.
// If serviceName or serviceVersion are empty, they can be set via OTEL_SERVICE_NAME
// and OTEL_RESOURCE_ATTRIBUTES environment variables.
func newResource(ctx context.Context, serviceName, serviceVersion string) (*resource.Resource, error) {
	opts := []resource.Option{
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	}
	if serviceName != "" {
		opts = append(opts, resource.WithAttributes(semconv.ServiceName(serviceName)))
	}
	if serviceVersion != "" {
		opts = append(opts, resource.WithAttributes(semconv.ServiceVersion(serviceVersion)))
	}
	return resource.New(ctx, opts...)
}

// newPropagator creates a composite propagator for distributed tracing.
// It reads the OTEL_PROPAGATORS environment variable to determine which
// propagators to use. If not set, defaults to "tracecontext,baggage".
// Supported values: tracecontext, baggage, none.
// Values are case-insensitive and deduplicated per the OpenTelemetry specification.
// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/
func newPropagator() propagation.TextMapPropagator {
	envValue := os.Getenv("OTEL_PROPAGATORS")
	if envValue == "" {
		envValue = "tracecontext,baggage"
	}
	return NewPropagator(envValue)
}

// NewPropagator creates a TextMapPropagator from a comma-separated list of propagator names.
// Supported values: tracecontext, baggage, none.
// Values are case-insensitive and deduplicated.
// Unknown propagator names are ignored.
func NewPropagator(propagators string) propagation.TextMapPropagator {
	names := strings.Split(propagators, ",")

	// Deduplicate and normalize to lowercase.
	seen := make(map[string]bool)
	var unique []string
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			unique = append(unique, name)
		}
	}

	// If "none" is specified alone, return a no-op propagator.
	if len(unique) == 1 && unique[0] == "none" {
		return propagation.NewCompositeTextMapPropagator()
	}

	var props []propagation.TextMapPropagator
	for _, name := range unique {
		switch name {
		case "tracecontext":
			props = append(props, propagation.TraceContext{})
		case "baggage":
			props = append(props, propagation.Baggage{})
		case "none":
			// Ignored when combined with other propagators.
		}
	}

	if len(props) == 0 {
		return propagation.NewCompositeTextMapPropagator()
	}

	return propagation.NewCompositeTextMapPropagator(props...)
}

// newTracerProvider creates a trace provider with either OTLP gRPC or stdout exporter.
func newTracerProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdktrace.TracerProvider, error) {
	var traceExporter sdktrace.SpanExporter
	var err error

	if cfg.UseStdout {
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stderr
		}
		traceExporter, err = stdouttrace.New(stdouttrace.WithWriter(w))
	} else {
		opts := []otlptracegrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		traceExporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	return tracerProvider, nil
}

// newMeterProvider creates a meter provider with either OTLP gRPC or stdout exporter.
func newMeterProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdkmetric.MeterProvider, error) {
	var metricExporter sdkmetric.Exporter
	var err error

	if cfg.UseStdout {
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stderr
		}
		metricExporter, err = stdoutmetric.New(stdoutmetric.WithWriter(w))
	} else {
		opts := []otlpmetricgrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		metricExporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	interval := cfg.MetricInterval
	if interval == 0 {
		interval = 60 * time.Second
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExporter,
				sdkmetric.WithInterval(interval),
			),
		),
	)

	return meterProvider, nil
}

// newLoggerProvider creates a logger provider with either OTLP gRPC or stdout exporter.
func newLoggerProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdklog.LoggerProvider, error) {
	var logExporter sdklog.Exporter
	var err error

	if cfg.UseStdout {
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stderr
		}
		logExporter, err = stdoutlog.New(stdoutlog.WithWriter(w))
	} else {
		opts := []otlploggrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlploggrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		logExporter, err = otlploggrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}

	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	return loggerProvider, nil
}
