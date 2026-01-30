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

	bufferPool *BufferPool
}

func (p *Proxy) Name() string {
	return p.name
}

// BufferPool is a pool of reusable byte buffers with a size of multipels of 1KB
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new BufferPool with buffers of the given size
func NewBufferPool(bufferSize int) *BufferPool {
	new := func() any {
		buf := make([]byte, bufferSize)
		return buf
	}
	return &BufferPool{
		pool: sync.Pool{
			New: new,
		},
	}
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf []byte) {
	bp.pool.Put(buf)
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
func (p *Proxy) Start(ctx context.Context) error {
	if p.listener == nil {
		// Create a listener
		hp := net.JoinHostPort("", p.port)
		l, err := net.Listen("tcp", hp)
		if err != nil {
			return err
		}
		p.listener = l
		p.logger.Info("created listener", "protocol", "tcp", "address", hp, "remoteAddr", p.remoteAddr)
	}
	// Close the listener when done
	defer p.listener.Close()

	// Start an accept loop and wait for it to finish
	if err := p.acceptLoop(ctx); err != nil {
		return err
	}

	return nil
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
		p.logger.Info("shutting down proxy listener", "reason", ctx.Err())
		// Close the listener to stop accepting new connections
		p.listener.Close()
	}()

	// Use wg to wait for the connection handlers to finish
	var wg sync.WaitGroup
	var connectionID int64
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			p.logger.Info("stopping accept loop", "error", err)
			break
		}
		connectionID++
		p.logger.Info("accepted new connection", "connectionID", connectionID, "remoteAddr", conn.RemoteAddr().String())
		wg.Go(func() {
			if err := p.handleConnection(ctx, conn, connectionID); err != nil {
				if isClientDisconnect(err) {
					p.logger.Debug("connection closed by client", "error", err)
				} else {
					p.logger.Error("error handling connection", "error", err)
				}
			}
		})
	}

	wg.Wait()
	return nil
}

func (p *Proxy) handleConnection(ctx context.Context, conn net.Conn, connID int64) error {
	defer conn.Close()

	logger := p.logger.With("connectionID", connID, "remoteAddr", conn.RemoteAddr().String())

	var dialer net.Dialer
	// Create a client connection to the target service
	client, err := dialer.DialContext(ctx, "tcp", p.remoteAddr)
	if err != nil {
		logger.Error("failed to connect to target", "remoteAddr", p.remoteAddr, "error", err)
		return err
	}
	defer client.Close()

	// wg for the two goroutines
	var wg sync.WaitGroup
	// Errors from downstream and upstream
	var downErr, upErr error
	// Start a process that forwards data between conn and client
	downDone := make(chan struct{})
	wg.Go(func() {
		defer close(downDone)
		logger := logger.With("direction", "downstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(client, conn, buf)
		if err != nil {
			if isClientDisconnect(err) {
				logger.Debug("client disconnected", "error", err)
				return
			}
			if !errors.Is(err, net.ErrClosed) {
				logger.Info("copy buffer stopped unexpectedly", "error", err)
				upErr = err // or downErr
				return
			}
		}
		logger.Info("forwarded data", "bytes", written)
	})

	// Start a process that forwards data from client to conn
	upDone := make(chan struct{})
	wg.Go(func() {
		defer close(upDone)
		logger := logger.With("direction", "upstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(conn, client, buf)
		if err != nil {
			if isClientDisconnect(err) {
				logger.Debug("client disconnected", "error", err)
				return
			}
			if !errors.Is(err, net.ErrClosed) {
				logger.Info("copy buffer stopped unexpectedly", "error", err)
				upErr = err // or downErr
				return
			}
		}
		logger.Info("forwarded data", "bytes", written)
	})

	var closed bool
	closeAll := func() {
		if closed {
			return
		}
		closed = true
		conn.Close()
		client.Close()
	}

	closeWrite := func(c net.Conn) {
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
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

	return errors.Join(downErr, upErr)
}
