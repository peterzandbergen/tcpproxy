#!/bin/sh
#
# Run the OpenTelemetry Collector Contrib in a Docker container.
# Exposes OTLP gRPC on port 4317 and HTTP on port 4318.
#

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_FILE="${PROJECT_DIR}/otel-collector-config.yaml"
CONTAINER_NAME="otel-collector"
IMAGE="otel/opentelemetry-collector-contrib:latest"

# Check if config file exists
if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config file not found: $CONFIG_FILE"
    exit 1
fi

# Stop existing container if running
if docker ps -q -f name="$CONTAINER_NAME" | grep -q .; then
    echo "Stopping existing container..."
    docker stop "$CONTAINER_NAME" >/dev/null
fi

# Remove existing container if exists
if docker ps -aq -f name="$CONTAINER_NAME" | grep -q .; then
    echo "Removing existing container..."
    docker rm "$CONTAINER_NAME" >/dev/null
fi

echo "Starting OpenTelemetry Collector..."
echo "  Config: $CONFIG_FILE"
echo "  OTLP gRPC: localhost:4317"
echo "  OTLP HTTP: localhost:4318"
echo ""

docker run \
    --name "$CONTAINER_NAME" \
    --rm \
    -p 4317:4317 \
    -p 4318:4318 \
    -v "${CONFIG_FILE}:/etc/otelcol-contrib/config.yaml:ro" \
    "$IMAGE"
