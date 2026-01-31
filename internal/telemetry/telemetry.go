// Package telemetry provides OpenTelemetry instrumentation setup for the tcpproxy.
// It configures OTLP exporters for traces, metrics, and logs.
package telemetry

import (
	"context"
	"errors"
	"io"
	"os"
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
func Setup(ctx context.Context, cfg Config) (*Telemetry, func(context.Context) error, error) {
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

	// Set up propagator for distributed tracing context.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

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
func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
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
