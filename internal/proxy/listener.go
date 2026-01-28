package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
)

// ProxyConfig holds configuration for the proxy
type ProxyConfig struct {
	Name       string
	ListenPort int
	TargetHost string
	TargetPort int
}

// Proxy accepts incoming connections and forwards them to the target service
type Proxy struct {
	name       string
	port       int
	targetHost string
	targetPort int
	logger     *slog.Logger
	listener   net.Listener

	bufferPool *BufferPool
}

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
		port:       cfg.ListenPort,
		targetHost: cfg.TargetHost,
		targetPort: cfg.TargetPort,
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
		l, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(p.port)))
		if err != nil {
			return err
		}
		p.listener = l
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
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			// Listener closed or error occurred
			break
		}
		wg.Go(func() {
			p.handleConnection(ctx, conn)
		})
	}

	wg.Wait()
	return nil
}

func (p *Proxy) handleConnection(ctx context.Context, conn net.Conn) error {
	defer conn.Close()

	var dialer net.Dialer
	// Create a client connection to the target service
	client, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(p.targetHost, strconv.Itoa(p.targetPort)))
	if err != nil {
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
		logger := p.logger.With("direction", "downstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(client, conn, buf)
		if err != nil {
			downErr = err
			return
		}
		logger.Info("forwarded data", "bytes", written)
	})

	// Start a process that forwards data from client to conn
	upDone := make(chan struct{})
	wg.Go(func() {
		defer close(upDone)
		logger := p.logger.With("direction", "upstream")
		buf := p.bufferPool.Get()
		defer p.bufferPool.Put(buf)
		written, err := io.CopyBuffer(conn, client, buf)
		if err != nil {
			upErr = err
			return
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

	// Stop when context is done or one of the directions is done
	select {
	case <-ctx.Done():
		closeAll()
	case <-downDone:
		closeAll()
	case <-upDone:
		closeAll()
	}
	wg.Wait()
	return errors.Join(downErr, upErr)
}

// type Listener struct {
// 	listener net.Listener
// }

// type Handler struct {
// 	conn net.Conn
// }

// func NewHandler(conn net.Conn) *Handler {
// 	return &Handler{conn: conn}
// }

// func NewListener(l net.Listener) *Listener {
// 	return &Listener{listener: l}
// }

// HandleAccept listens for new connections and starts a handler
// for each accepted connection.
// It stops listening when the context is cancelled, it also closes the listener.
// func (l *Listener) HandleAccept(ctx context.Context) error {
// 	defer func() {
// 		if err := l.listener.Close(); err != nil {
// 			// log the error
// 		}
// 	}()

// 	var connChan = make(chan net.Conn, 1)
// 	var acceptDone = make(chan struct{})
// 	// Accept connections and send them to connChan
// 	var acceptErr error
// 	var wg sync.WaitGroup
// 	wg.Go(func() {
// 		for {
// 			var conn net.Conn
// 			conn, acceptErr = l.listener.Accept()
// 			if acceptErr != nil {
// 				close(acceptDone)
// 				return
// 			}
// 			connChan <- conn
// 		}
// 	})

// 	var doneErr error
// LOOP:
// 	for {
// 		select {
// 		// This also cancels the handlers
// 		case <-ctx.Done():
// 			doneErr = ctx.Err()
// 			// close the listener
// 			l.listener.Close()
// 			// Wait for the handlers and the accept goroutine to finish
// 			wg.Wait()
// 			break LOOP
// 		case <-acceptDone:
// 			// Accept loop is done
// 			// Wait for handlers to finish
// 			wg.Wait()
// 			break LOOP
// 		case conn := <-connChan:
// 			// Start a new handler
// 			wg.Go(func() {
// 				handler := NewHandler(conn)
// 				handler.Handle(ctx)
// 			})
// 		}
// 	}

// 	// return any errors encountered
// 	return errors.Join(acceptErr, doneErr)
// }

// func (h *Handler) Handle(ctx context.Context) {
// 	// Placeholder for handling the connection
// 	// Actual implementation would go here

// 	// Set up a connection with the downstream service
// 	defer h.conn.Close()
// }
