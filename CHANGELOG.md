# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Comprehensive `-h` usage output documenting all flags and environment variables
- Support for `OTEL_SDK_DISABLED` environment variable per OpenTelemetry specification
- Support for `OTEL_PROPAGATORS` environment variable (tracecontext, baggage, none)
- `SDKDisabled()` helper function to check if telemetry is disabled
- `NewPropagator()` function to create propagators from comma-separated list
- OpenTelemetry support for traces, metrics, and logs
- `--telemetry-enabled` flag to enable OpenTelemetry (default false)
- `--telemetry-exporter` flag to select exporter [otlp, stdout] (default otlp)
- `--telemetry-endpoint` flag to configure OTLP collector endpoint
- `--logformat=otel` option to send logs through OpenTelemetry pipeline
- `internal/slogmulti` package for fanout logging to multiple handlers
- `scripts/run-otel-collector.sh` script to run OpenTelemetry Collector in Docker
- `otel-collector-config.yaml` configuration for local collector development

### Changed

- Logs are now always written to stderr regardless of telemetry configuration
- When `--logformat=otel` is used, logs go to both stderr and OpenTelemetry
- Telemetry stdout exporter writes to stderr instead of stdout
