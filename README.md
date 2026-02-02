# tcpproxy

## Usage

```
Usage of dist/tcpproxy_linux_amd64_v1/tcpproxy:
  -logformat string
        Log format [json, text] TCPPROXY_LOGFORMAT (default "text")
  -loglevel string
        Log level [error, warn, info, debug] TCPPROXY_LOGLEVEL (default "info")
  -proxy value
        Proxy spec: port=remote-host:remote-port (can be repeated) TCPPROXY_PROXY
  -telemetry-enabled
        Enable OpenTelemetry (default false) TCPPROXY_TELEMETRY_ENABLED
  -telemetry-endpoint string
        OTLP collector endpoint (e.g., localhost:4317)
  -telemetry-exporter
        Set the exporter [otlp, stdout] (default otlp) TCPPROXY_TELEMETRY_EXPORTER

NOTE: When -telemetry-enabled and -telemetry-exporter is otlp, then tcpproxy also sends logging to stderr
```

## OpenTelemetry Environment Variables

The application follows the [OpenTelemetry Environment Variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/) specification.

### SDK Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_SDK_DISABLED` | `false` | Set to `true` to disable the SDK (returns no-op providers) |
| `OTEL_PROPAGATORS` | `tracecontext,baggage` | Comma-separated list of propagators. Supported: `tracecontext`, `baggage`, `none` |
| `OTEL_SERVICE_NAME` | - | Service name for telemetry data |
| `OTEL_RESOURCE_ATTRIBUTES` | - | Key-value pairs for resource attributes |

### OTLP Exporter Configuration

These variables are handled by the OpenTelemetry SDK exporters:

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` (gRPC) | Target endpoint for the OTLP exporter |
| `OTEL_EXPORTER_OTLP_HEADERS` | - | Headers for OTLP requests (key=value pairs) |
| `OTEL_EXPORTER_OTLP_TIMEOUT` | `10s` | Maximum wait time for each export |
| `OTEL_EXPORTER_OTLP_COMPRESSION` | - | Compression method: `gzip` or `none` |
| `OTEL_EXPORTER_OTLP_INSECURE` | `false` | Disable TLS for gRPC connections |
| `OTEL_EXPORTER_OTLP_CERTIFICATE` | - | Path to trusted certificate for TLS |
| `OTEL_EXPORTER_OTLP_CLIENT_KEY` | - | Path to client private key for mTLS |
| `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE` | - | Path to client certificate for mTLS |

### Signal-Specific Exporter Configuration

Signal-specific variables override the general OTLP settings. Replace `<SIGNAL>` with `TRACES`, `METRICS`, or `LOGS`:

- `OTEL_EXPORTER_OTLP_<SIGNAL>_ENDPOINT`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_HEADERS`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_TIMEOUT`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_COMPRESSION`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_INSECURE`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_CERTIFICATE`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_CLIENT_KEY`
- `OTEL_EXPORTER_OTLP_<SIGNAL>_CLIENT_CERTIFICATE`

For example, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` overrides `OTEL_EXPORTER_OTLP_ENDPOINT` for traces only.
