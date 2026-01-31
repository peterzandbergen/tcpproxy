package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
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

	bufferPool        *BufferPool
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
	p := &Proxy{
		name:       cfg.Name,
		port:       cfg.Port,
		remoteAddr: cfg.RemoteAddr,
		logger:     slog.Default().With("proxy", cfg.Name),
		bufferPool: NewBufferPool(10 * 1024),
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

func (p *Proxy) handleConnection(ctx context.Context, conn net.Conn, connID int64) error {
	logger := p.logger.With("connectionID", connID, "remoteAddr", conn.RemoteAddr().String())
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.ErrorContext(ctx, "error closing conn", "error", err)
		}
	}()

	var dialer net.Dialer
	// Create a client connection to the target service
	client, err := dialer.DialContext(ctx, "tcp", p.remoteAddr)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to target", "remoteAddr", p.remoteAddr, "error", err)
		return err
	}
	defer func() {
		if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.ErrorContext(ctx, "error closing client", "error", err)
		}
	}()

	// wg for the two goroutines
	var wg sync.WaitGroup
	// Start a process that forwards data between conn and client
	downDone := make(chan struct{})
	wg.Go(func() {
		defer close(downDone)
		logger := logger.With("direction", "downstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(client, conn, *buf)
		if err != nil {
			logger.DebugContext(ctx, "downstream copy stopped", "bytes", written, "error", err)
			return
		}
		logger.InfoContext(ctx, "downstream complete", "bytes", written)
	})

	// Start a process that forwards data from client to conn
	upDone := make(chan struct{})
	wg.Go(func() {
		defer close(upDone)
		logger := logger.With("direction", "upstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(conn, client, *buf)
		if err != nil {
			logger.DebugContext(ctx, "upstream copy stopped", "bytes", written, "error", err)
			return
		}
		logger.InfoContext(ctx, "upstream complete", "bytes", written)
	})

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

	return nil
}
