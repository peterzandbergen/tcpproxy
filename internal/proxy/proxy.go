package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ProxyConfig holds configuration for the proxy
type ProxyConfig struct {
	Name       string
	Port       string
	RemoteAddr string
}

// Proxy accepts incoming connections and forwards them to the target service
type Proxy struct {
	name       string
	port       string
	remoteAddr string
	logger     *slog.Logger
	listener   net.Listener

	tracer     trace.Tracer
	meter      metric.Meter
	bufferPool *BufferPool

	// Metrics instruments
	connTotal     metric.Int64Counter
	connActive    metric.Int64UpDownCounter
	connDuration  metric.Float64Histogram
	bytesReceived metric.Int64Counter
	bytesSent     metric.Int64Counter
	connErrors    metric.Int64Counter

	listenerCloseOnce sync.Once
}

func (p *Proxy) Name() string {
	return p.name
}

// Option configures the Proxy
type Option func(*Proxy)

// WithLogger sets the logger for the proxy
func WithLogger(logger *slog.Logger) Option {
	return func(p *Proxy) {
		p.logger = logger
	}
}

// WithListener sets a custom net.Listener for the proxy
func WithListener(listener net.Listener) Option {
	return func(p *Proxy) {
		p.listener = listener
	}
}

// WithBufferSize sets the buffer size in kilobytes for the proxy
func WithBufferSize(size int) Option {
	return func(p *Proxy) {
		p.bufferPool = NewBufferPool(1024 * size)
	}
}

// NewProxy creates a new Proxy instance with the given configuration and options
func NewProxy(cfg ProxyConfig, opts ...Option) *Proxy {
	const instrumentationName = "github.com/myhops/tcpproxy/internal/proxy"

	p := &Proxy{
		name:       cfg.Name,
		port:       cfg.Port,
		remoteAddr: cfg.RemoteAddr,
		logger:     slog.Default().With("proxy", cfg.Name),
		bufferPool: NewBufferPool(10 * 1024),
		tracer:     otel.Tracer(instrumentationName),
		meter:      otel.Meter(instrumentationName),
	}

	// Initialize metric instruments
	var err error

	p.connTotal, err = p.meter.Int64Counter("proxy.connections.total",
		metric.WithDescription("Total number of connections accepted"),
		metric.WithUnit("{connection}"))
	if err != nil {
		p.logger.Error("failed to create connTotal metric", "error", err)
	}

	p.connActive, err = p.meter.Int64UpDownCounter("proxy.connections.active",
		metric.WithDescription("Number of currently active connections"),
		metric.WithUnit("{connection}"))
	if err != nil {
		p.logger.Error("failed to create connActive metric", "error", err)
	}

	p.connDuration, err = p.meter.Float64Histogram("proxy.connection.duration",
		metric.WithDescription("Duration of connections"),
		metric.WithUnit("s"))
	if err != nil {
		p.logger.Error("failed to create connDuration metric", "error", err)
	}

	p.bytesReceived, err = p.meter.Int64Counter("proxy.bytes.received",
		metric.WithDescription("Total bytes received from clients"),
		metric.WithUnit("By"))
	if err != nil {
		p.logger.Error("failed to create bytesReceived metric", "error", err)
	}

	p.bytesSent, err = p.meter.Int64Counter("proxy.bytes.sent",
		metric.WithDescription("Total bytes sent to clients"),
		metric.WithUnit("By"))
	if err != nil {
		p.logger.Error("failed to create bytesSent metric", "error", err)
	}

	p.connErrors, err = p.meter.Int64Counter("proxy.connections.errors",
		metric.WithDescription("Total number of connection errors"),
		metric.WithUnit("{error}"))
	if err != nil {
		p.logger.Error("failed to create connErrors metric", "error", err)
	}

	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Start begins listening for incoming connections and handling them
// To stop the proxy use the provided context
func (p *Proxy) ListenAndServe(ctx context.Context) error {
	if p.listener == nil {
		// Create a listener
		hp := net.JoinHostPort("", p.port)
		l, err := net.Listen("tcp", hp)
		if err != nil {
			return err
		}
		p.listener = l
		p.logger.InfoContext(ctx, "created listener", "protocol", "tcp", "address", hp, "remoteAddr", p.remoteAddr)
	}
	// Close the listener when done
	defer func() {
		p.closeListener(ctx, p.logger)
	}()

	// Start an accept loop and wait for it to finish
	if err := p.acceptLoop(ctx); err != nil {
		return err
	}

	return nil
}

func (p *Proxy) closeListener(ctx context.Context, logger *slog.Logger) {
	p.listenerCloseOnce.Do(func() {
		if err := p.listener.Close(); err != nil {
			logger.ErrorContext(ctx, "error closing listener", "error", err)
		}
	})
}

// acceptLoop accepts incoming connections and starts a handler for each connection
// It stops when the context is cancelled
func (p *Proxy) acceptLoop(ctx context.Context) error {
	// Create a context that cancels all below when we exit
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// wait for ctx.Done() in a go routine
	go func() {
		<-ctx.Done()
		// Print the error
		p.logger.InfoContext(ctx, "shutting down proxy listener", "reason", ctx.Err())
		// Close the listener to stop accepting new connections
		p.closeListener(ctx, p.logger)
	}()

	// Use wg to wait for the connection handlers to finish
	var wg sync.WaitGroup
	var connectionID int64
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			p.logger.InfoContext(ctx, "stopping accept loop", "error", err)
			break
		}
		connectionID++
		connID := connectionID
		p.logger.InfoContext(ctx, "accepted new connection", "connectionID", connectionID, "remoteAddr", conn.RemoteAddr().String())
		wg.Go(func() {
			if err := p.handleConnection(ctx, conn, connID); err != nil {
				if isClientDisconnect(err) {
					p.logger.DebugContext(ctx, "connection closed by client", "error", err)
				} else {
					p.logger.ErrorContext(ctx, "error handling connection", "error", err)
				}
			}
		})
	}

	wg.Wait()
	return nil
}

// dial establishes a connection to the target address with tracing.
func (p *Proxy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	ctx, span := p.tracer.Start(ctx, "dial server",
		trace.WithAttributes(
			attribute.String("network", network),
			attribute.String("address", address),
		))
	defer span.End()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	span.SetAttributes(
		attribute.String("local_addr", conn.LocalAddr().String()),
		attribute.String("remote_addr", conn.RemoteAddr().String()),
	)

	return conn, nil
}

// forward forwards traffic from src to dst and records bytes written to bytesWritten.
func (p *Proxy) forward(ctx context.Context, dst, src net.Conn, logger *slog.Logger, direction string, done chan struct{}, bytesWritten *int64) func() {
	return func() {
		ctx, span := p.tracer.Start(ctx, "forward traffic",
			trace.WithAttributes(
				attribute.String("direction", direction),
				attribute.String("src", src.RemoteAddr().String()),
				attribute.String("dst", dst.RemoteAddr().String()),
			))
		defer span.End()
		defer close(done)

		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)

		written, err := io.CopyBuffer(dst, src, *buf)
		*bytesWritten = written

		span.SetAttributes(attribute.Int64("bytes.transferred", written))

		if err != nil {
			span.RecordError(err)
			logger.DebugContext(ctx, "forward stopped", "bytes", written, "error", err)
			return
		}
		logger.InfoContext(ctx, "forward complete", "bytes", written)
	}
}

func (p *Proxy) handleConnection(ctx context.Context, conn net.Conn, connID int64) error {
	// Start a span for this connection
	ctx, span := p.tracer.Start(ctx, "handle connection",
		trace.WithAttributes(
			attribute.Int64("connection.id", connID),
			attribute.String("connection.remote_addr", conn.RemoteAddr().String()),
			attribute.String("proxy.name", p.name),
			attribute.String("proxy.target", p.remoteAddr),
		))
	defer span.End()

	// Record metrics
	attrs := metric.WithAttributes(
		attribute.String("proxy.name", p.name),
		attribute.String("proxy.target", p.remoteAddr),
	)
	p.connTotal.Add(ctx, 1, attrs)
	p.connActive.Add(ctx, 1, attrs)
	start := time.Now()
	defer func() {
		p.connActive.Add(ctx, -1, attrs)
		p.connDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	}()

	logger := p.logger.With("connectionID", connID, "remoteAddr", conn.RemoteAddr().String())
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.ErrorContext(ctx, "error closing conn", "error", err)
		}
	}()

	// Create a client connection to the target service
	client, err := p.dial(ctx, "tcp", p.remoteAddr)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to target", "remoteAddr", p.remoteAddr, "error", err)
		p.connErrors.Add(ctx, 1, attrs)
		span.RecordError(err)
		return err
	}
	defer func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.ErrorContext(ctx, "error closing client", "error", err)
		}
	}()

	// wg for the two goroutines
	var wg sync.WaitGroup

	// Start a process that forwards data between conn and client, downstream (client -> target)
	downDone := make(chan struct{})
	var bytesDown int64
	wg.Go(p.forward(ctx, client, conn, logger.With("direction", "downstream"), "downstream", downDone, &bytesDown))

	// Start a process that forwards data from client to conn, upstream (target -> client)
	upDone := make(chan struct{})
	var bytesUp int64
	wg.Go(p.forward(ctx, conn, client, logger.With("direction", "upstream"), "upstream", upDone, &bytesUp))

	closeAll := func() {
		conn.Close()
		client.Close()
	}

	closeWrite := func(c net.Conn) {
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			if err := cw.CloseWrite(); err != nil {
				logger.ErrorContext(ctx, "closeWrite failed", "remote", c.RemoteAddr(), "error", err)
			}
		}
	}

	// Stop when context is done or one of the directions is done
	select {
	case <-ctx.Done():
		closeAll()
	case <-downDone:
		closeWrite(client)
	case <-upDone:
		closeWrite(conn)
	}
	wg.Wait()

	// Record bytes transferred
	p.bytesReceived.Add(ctx, bytesDown, attrs)
	p.bytesSent.Add(ctx, bytesUp, attrs)
	span.SetAttributes(
		attribute.Int64("bytes.received", bytesDown),
		attribute.Int64("bytes.sent", bytesUp),
	)

	return nil
}
