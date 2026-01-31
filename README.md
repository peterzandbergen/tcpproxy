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