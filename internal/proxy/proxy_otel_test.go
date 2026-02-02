//go:build integration

package proxy_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/myhops/tcpproxy/internal/proxy"
	"github.com/myhops/tcpproxy/internal/telemetry"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// otelCollectorConfig is a minimal config for the OTEL collector that exports to debug output.
const otelCollectorConfig = `
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 1s

exporters:
  debug:
    verbosity: detailed

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
`

// startOtelCollector starts an OTEL collector container and returns the gRPC endpoint.
func startOtelCollector(ctx context.Context, t *testing.T) (endpoint string, cleanup func()) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        "otel/opentelemetry-collector:latest",
		ExposedPorts: []string{"4317/tcp"},
		WaitingFor:   wait.ForListeningPort("4317/tcp").WithStartupTimeout(30 * time.Second),
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(otelCollectorConfig),
				ContainerFilePath: "/etc/otelcol/config.yaml",
				FileMode:          0644,
			},
		},
		Cmd: []string{"--config=/etc/otelcol/config.yaml"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start otel collector container: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "4317")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("failed to get mapped port: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("failed to get container host: %v", err)
	}

	endpoint = net.JoinHostPort(host, mappedPort.Port())

	cleanup = func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	}

	return endpoint, cleanup
}

func TestProxyWithOtelCollector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start OTEL collector container
	endpoint, cleanup := startOtelCollector(ctx, t)
	defer cleanup()

	t.Logf("OTEL collector started at %s", endpoint)

	// Set up telemetry with the collector endpoint
	telCfg := telemetry.Config{
		ServiceName:    "tcpproxy-test",
		ServiceVersion: "test",
		OTLPEndpoint:   endpoint,
		EnableTraces:   true,
		EnableMetrics:  true,
		EnableLogs:     true,
	}

	_, shutdownTelemetry, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		t.Fatalf("failed to setup telemetry: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			t.Logf("telemetry shutdown error: %v", err)
		}
	}()

	// Create a mock target server that echoes data
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create target listener: %v", err)
	}
	defer targetListener.Close()

	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c) // Echo
			}(conn)
		}
	}()

	// Create proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create proxy listener: %v", err)
	}

	cfg := proxy.ProxyConfig{
		Name:       "otel-test-proxy",
		RemoteAddr: targetListener.Addr().String(),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := proxy.NewProxy(cfg, proxy.WithListener(proxyListener), proxy.WithLogger(logger))

	proxyCtx, cancelProxy := context.WithCancel(ctx)
	defer cancelProxy()

	go p.ListenAndServe(proxyCtx)
	time.Sleep(50 * time.Millisecond)

	// Run multiple connections to generate telemetry
	const numConnections = 5
	var wg sync.WaitGroup

	for i := range numConnections {
		wg.Add(1)
		go func(connNum int) {
			defer wg.Done()

			client, err := net.Dial("tcp", proxyListener.Addr().String())
			if err != nil {
				t.Logf("connection %d: failed to connect: %v", connNum, err)
				return
			}
			defer client.Close()

			// Send and receive data
			testData := []byte("hello from connection")
			if _, err := client.Write(testData); err != nil {
				t.Logf("connection %d: write error: %v", connNum, err)
				return
			}

			client.SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 1024)
			n, err := client.Read(buf)
			if err != nil {
				t.Logf("connection %d: read error: %v", connNum, err)
				return
			}

			if string(buf[:n]) != string(testData) {
				t.Logf("connection %d: unexpected response: %s", connNum, string(buf[:n]))
			}
		}(i)
	}

	wg.Wait()

	// Allow time for telemetry to be exported
	time.Sleep(2 * time.Second)

	t.Log("Test completed - telemetry data should have been sent to the collector")
}

func TestProxyMetricsWithOtelCollector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start OTEL collector container
	endpoint, cleanup := startOtelCollector(ctx, t)
	defer cleanup()

	t.Logf("OTEL collector started at %s", endpoint)

	// Set up telemetry
	telCfg := telemetry.Config{
		ServiceName:    "tcpproxy-metrics-test",
		ServiceVersion: "test",
		OTLPEndpoint:   endpoint,
		EnableTraces:   true,
		EnableMetrics:  true,
		EnableLogs:     true,
		MetricInterval: time.Second, // Export metrics every second for faster test
	}

	_, shutdownTelemetry, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		t.Fatalf("failed to setup telemetry: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			t.Logf("telemetry shutdown error: %v", err)
		}
	}()

	// Create target server
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create target listener: %v", err)
	}
	defer targetListener.Close()

	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					c.Write(buf[:n])
				}
			}(conn)
		}
	}()

	// Create proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create proxy listener: %v", err)
	}

	cfg := proxy.ProxyConfig{
		Name:       "metrics-test-proxy",
		RemoteAddr: targetListener.Addr().String(),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := proxy.NewProxy(cfg, proxy.WithListener(proxyListener), proxy.WithLogger(logger))

	proxyCtx, cancelProxy := context.WithCancel(ctx)
	defer cancelProxy()

	go p.ListenAndServe(proxyCtx)
	time.Sleep(50 * time.Millisecond)

	// Make connections and transfer data to generate metrics
	for i := range 3 {
		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("connection %d: failed to connect: %v", i, err)
		}

		// Transfer some data
		data := make([]byte, 1024)
		for j := range data {
			data[j] = byte(j % 256)
		}

		client.Write(data)
		client.SetReadDeadline(time.Now().Add(time.Second))
		io.ReadAll(client)
		client.Close()

		// Small delay between connections
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for metrics to be exported
	time.Sleep(3 * time.Second)

	t.Log("Metrics test completed - metrics should have been exported to the collector")
}

func TestProxyTracingSpans(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start OTEL collector container
	endpoint, cleanup := startOtelCollector(ctx, t)
	defer cleanup()

	t.Logf("OTEL collector started at %s", endpoint)

	// Set up telemetry
	telCfg := telemetry.Config{
		ServiceName:    "tcpproxy-tracing-test",
		ServiceVersion: "test",
		OTLPEndpoint:   endpoint,
		EnableTraces:   true,
		EnableMetrics:  false,
		EnableLogs:     false,
	}

	_, shutdownTelemetry, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		t.Fatalf("failed to setup telemetry: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			t.Logf("telemetry shutdown error: %v", err)
		}
	}()

	// Create target server
	targetListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create target listener: %v", err)
	}
	defer targetListener.Close()

	go func() {
		for {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	// Create proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create proxy listener: %v", err)
	}

	cfg := proxy.ProxyConfig{
		Name:       "tracing-test-proxy",
		RemoteAddr: targetListener.Addr().String(),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := proxy.NewProxy(cfg, proxy.WithListener(proxyListener), proxy.WithLogger(logger))

	proxyCtx, cancelProxy := context.WithCancel(ctx)
	defer cancelProxy()

	go p.ListenAndServe(proxyCtx)
	time.Sleep(50 * time.Millisecond)

	// Single connection to generate a trace
	client, err := net.Dial("tcp", proxyListener.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	client.Write([]byte("trace test data"))
	client.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1024)
	client.Read(buf)
	client.Close()

	// Wait for trace to be exported
	time.Sleep(2 * time.Second)

	t.Log("Tracing test completed - trace spans should have been exported to the collector")
}
