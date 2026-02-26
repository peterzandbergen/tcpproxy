# AGENTS.md - Agent Guidelines for tcpproxy

tcpproxy is a Go-based TCP proxy with OpenTelemetry support for traces, metrics, and logs.

## Build/Test Commands

```bash
go build -o /tmp/tcpproxy ./cmd/tcpproxy              # Local build
goreleaser release --clean --snapshot                 # Snapshot release
go test ./...                                         # All tests
go test -v ./internal/proxy/... -run TestNewProxy      # Single test
go test -race ./...                                   # Race detector
gofmt -w .                                            # Format code
go vet ./...                                          # Vet
go build ./... && go test ./...                       # All checks
```

## Version Control (jj)

```bash
jj describe -m "message"     # Describe change
jj new                        # New change
jj bookmark set main -r @-    # Set main to current
jj git push                   # Push to remote
```

## Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting
- Prefer stdlib before external dependencies
- Use `context.Context` for cancellation/timeouts

### Imports

Group: stdlib, third-party, project. Example:
```go
import (
    "context"
    "log/slog"

    "github.com/myhops/tcpproxy/internal/proxy"
    "go.opentelemetry.io/otel"
)
```

### Naming

- Packages: short, lowercase (e.g., `proxy`, `telemetry`)
- Exported: PascalCase (e.g., `ProxyConfig`)
- Unexported: camelCase
- Short names for short scopes: `i`, `ctx`, `err`

### Documentation

- Package doc comment required
- Document all exported identifiers
- GoDoc style: start with name being documented

### Error Handling

- Handle errors explicitly; avoid naked returns
- Use `fmt.Errorf` with `%w` for wrapping
- Return errors to callers; log only at top level

### Testing

- Use `t.Run` for organized test output
- Name: `TestFunctionName_Scenario_ExpectedResult`
- Use `t.Context()` for context (Go 1.24+)

### Logging

- Use `log/slog` for structured logging
- Levels: Debug, Info, Warn, Error

### Dependency Injection

- Pass functions as parameters for testability
- Example: `func LoadConfig(args []string, getenv func(string) string)`

### Option Pattern

```go
type ProxyOption func(*Proxy)

func WithLogger(logger *slog.Logger) ProxyOption {
    return func(p *Proxy) { p.logger = logger }
}

proxy := NewProxy(cfg, WithLogger(customLogger))
```

## Project Structure

```
tcpproxy/
├── cmd/tcpproxy/     # Main app: main.go, options.go, usage.txt
├── internal/
│   ├── proxy/        # Proxy implementation
│   ├── telemetry/    # OpenTelemetry setup
│   └── slogmulti/   # Multi-handler for slog
├── scripts/          # Shell scripts
├── dist/             # Built binaries
├── .goreleaser.yaml  # Release config
└── go.mod           # Dependencies
```

## Common Tasks

**Add a flag:** options.go: add to Config struct, add in LoadConfig, update usage.txt

**Add OpenTelemetry:**
```go
tracer := otel.Tracer("package name")
logger := otel.Logger("package name")
```

## Dependencies

- `go.opentelemetry.io/otel` - Core OTEL
- `go.opentelemetry.io/otel/sdk` - OTEL SDK
- `go.opentelemetry.io/otel/exporters/*` - Exporters (OTLP, stdout)
- `github.com/testcontainers/testcontainers-go` - Integration tests
