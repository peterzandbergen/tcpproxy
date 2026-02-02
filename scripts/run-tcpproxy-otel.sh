#!/bin/sh
#
# Run tcpproxy with OpenTelemetry telemetry configured to send to
# the local OTEL collector Docker container.
#
# Prerequisites:
#   - Run scripts/run-otel-collector.sh first to start the collector
#   - Build tcpproxy with: goreleaser release --clean --snapshot
#
# Usage:
#   ./scripts/run-tcpproxy-otel.sh [proxy-args...]
#
# Example:
#   ./scripts/run-tcpproxy-otel.sh --proxy 8080=localhost:80
#

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Detect OS and architecture for binary path
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

BINARY="${PROJECT_DIR}/dist/tcpproxy_${OS}_${ARCH}_v1/tcpproxy"

# Check if binary exists
if [ ! -x "$BINARY" ]; then
    echo "Error: Binary not found at $BINARY"
    echo "Build first with: goreleaser release --clean --snapshot"
    exit 1
fi

# Standard OpenTelemetry environment variables
# https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/

# Service identification
export OTEL_SERVICE_NAME="${OTEL_SERVICE_NAME:-tcpproxy}"

# OTLP exporter endpoint (gRPC)
export OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4317}"

# Use gRPC protocol (default for Go SDK)
export OTEL_EXPORTER_OTLP_PROTOCOL="${OTEL_EXPORTER_OTLP_PROTOCOL:-grpc}"

# Disable TLS for local development
export OTEL_EXPORTER_OTLP_INSECURE="${OTEL_EXPORTER_OTLP_INSECURE:-true}"

# Optional: Configure resource attributes
# export OTEL_RESOURCE_ATTRIBUTES="deployment.environment=development,service.instance.id=$(hostname)"

# tcpproxy-specific configuration
export TCPPROXY_TELEMETRY_ENABLED=true
export TCPPROXY_TELEMETRY_EXPORTER=otlp
export TCPPROXY_LOGFORMAT=otel
export TCPPROXY_LOGLEVEL="${TCPPROXY_LOGLEVEL:-info}"

# Experiment
# export OTEL_SDK_DISABLED=true

echo "Starting tcpproxy with OpenTelemetry..."
echo "  Binary:   $BINARY"
echo "  Endpoint: $OTEL_EXPORTER_OTLP_ENDPOINT"
echo "  Service:  $OTEL_SERVICE_NAME"
echo ""

exec "$BINARY" "$@"
