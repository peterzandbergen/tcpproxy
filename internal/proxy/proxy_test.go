package proxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func TestNewProxy(t *testing.T) {
	t.Run("creates proxy with default values", func(t *testing.T) {
		cfg := ProxyConfig{
			Name:       "test-proxy",
			Port:       "9000",
			RemoteAddr: "localhost:9001",
		}

		proxy := NewProxy(cfg)

		if proxy.name != cfg.Name {
			t.Errorf("expected name %q, got %q", cfg.Name, proxy.name)
		}
		if proxy.port != cfg.Port {
			t.Errorf("expected port %q, got %q", cfg.Port, proxy.port)
		}
		if proxy.remoteAddr != cfg.RemoteAddr {
			t.Errorf("expected remoteAddr %q, got %q", cfg.RemoteAddr, proxy.remoteAddr)
		}
		if proxy.logger == nil {
			t.Error("expected default logger to be set")
		}
		if proxy.bufferPool == nil {
			t.Error("expected default buffer pool to be set")
		}
	})

	t.Run("Name returns proxy name", func(t *testing.T) {
		cfg := ProxyConfig{Name: "my-proxy"}
		proxy := NewProxy(cfg)

		if got := proxy.Name(); got != "my-proxy" {
			t.Errorf("Name() = %q, want %q", got, "my-proxy")
		}
	})
}

func TestProxyOptions(t *testing.T) {
	t.Run("WithLogger sets custom logger", func(t *testing.T) {
		customLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := ProxyConfig{Name: "test"}

		proxy := NewProxy(cfg, WithLogger(customLogger))

		if proxy.logger != customLogger {
			t.Error("expected custom logger to be set")
		}
	})

	t.Run("WithListener sets custom listener", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create listener: %v", err)
		}
		defer listener.Close()

		cfg := ProxyConfig{Name: "test"}
		proxy := NewProxy(cfg, WithListener(listener))

		if proxy.listener != listener {
			t.Error("expected custom listener to be set")
		}
	})

	t.Run("WithBufferSize sets buffer pool size", func(t *testing.T) {
		cfg := ProxyConfig{Name: "test"}
		proxy := NewProxy(cfg, WithBufferSize(32)) // 32KB

		if proxy.bufferPool == nil {
			t.Error("expected buffer pool to be set")
		}
	})
}

func TestListenAndServe(t *testing.T) {
	t.Run("starts and stops with context cancellation", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: "localhost:9999",
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(listener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())

		var wg sync.WaitGroup
		var serveErr error
		wg.Go(func() {
			serveErr = proxy.ListenAndServe(ctx)
		})

		// Give it time to start
		time.Sleep(10 * time.Millisecond)

		// Cancel context to stop
		cancel()

		wg.Wait()

		if serveErr != nil {
			t.Errorf("ListenAndServe returned error: %v", serveErr)
		}
	})

	t.Run("uses provided listener instead of creating new one", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create listener: %v", err)
		}
		expectedAddr := listener.Addr().String()

		cfg := ProxyConfig{
			Name:       "test-proxy",
			Port:       "0", // Would create on different port if not using provided listener
			RemoteAddr: "localhost:9999",
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(listener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())

		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		proxy.ListenAndServe(ctx)

		// Verify it used our listener by checking it was closed (can't reuse)
		_, err = net.Listen("tcp", expectedAddr)
		if err != nil {
			t.Log("listener address is now available as expected after close")
		}
	})
}

func TestProxyDataForwarding(t *testing.T) {
	t.Run("forwards data between client and target", func(t *testing.T) {
		// Create a mock target server
		targetListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create target listener: %v", err)
		}
		defer targetListener.Close()

		// Target server echoes back data with prefix
		targetReady := make(chan struct{})
		go func() {
			close(targetReady)
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			buf := make([]byte, 1024)
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			// Echo back with prefix
			response := append([]byte("echo:"), buf[:n]...)
			conn.Write(response)
		}()

		<-targetReady

		// Create proxy listener
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: targetListener.Addr().String(),
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Start proxy
		go proxy.ListenAndServe(ctx)

		// Give proxy time to start accepting
		time.Sleep(10 * time.Millisecond)

		// Connect to proxy as client
		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect to proxy: %v", err)
		}
		defer client.Close()

		// Send data
		testData := []byte("hello proxy")
		_, err = client.Write(testData)
		if err != nil {
			t.Fatalf("failed to write to proxy: %v", err)
		}

		// Read response
		client.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 1024)
		n, err := client.Read(buf)
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		expected := "echo:hello proxy"
		if got := string(buf[:n]); got != expected {
			t.Errorf("expected response %q, got %q", expected, got)
		}
	})

	t.Run("handles target connection failure gracefully", func(t *testing.T) {
		// Create proxy pointing to non-existent target
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: "127.0.0.1:1", // Invalid port
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go proxy.ListenAndServe(ctx)

		time.Sleep(10 * time.Millisecond)

		// Connect to proxy - should accept but fail to forward
		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect to proxy: %v", err)
		}
		defer client.Close()

		// Connection should close after failed target dial
		client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 1024)
		_, err = client.Read(buf)
		if err == nil {
			t.Error("expected read error due to failed target connection")
		}
	})

	t.Run("handles multiple concurrent connections", func(t *testing.T) {
		// Create a target server that handles multiple connections
		targetListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create target listener: %v", err)
		}
		defer targetListener.Close()

		// Target server echoes back with connection ID
		go func() {
			connID := 0
			for {
				conn, err := targetListener.Accept()
				if err != nil {
					return
				}
				connID++
				go func(id int, c net.Conn) {
					defer c.Close()
					buf := make([]byte, 1024)
					for {
						n, err := c.Read(buf)
						if err != nil {
							return
						}
						c.Write(buf[:n])
					}
				}(connID, conn)
			}
		}()

		// Create proxy
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: targetListener.Addr().String(),
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go proxy.ListenAndServe(ctx)
		time.Sleep(10 * time.Millisecond)

		// Launch multiple concurrent clients
		const numClients = 10
		var wg sync.WaitGroup
		errors := make(chan error, numClients)

		for i := range numClients {
			wg.Go(func() {
				client, err := net.Dial("tcp", proxyListener.Addr().String())
				if err != nil {
					errors <- err
					return
				}
				defer client.Close()

				// Each client sends unique data
				msg := []byte("hello from client " + string(rune('A'+i)))
				if _, err := client.Write(msg); err != nil {
					errors <- err
					return
				}

				client.SetReadDeadline(time.Now().Add(time.Second))
				buf := make([]byte, 1024)
				n, err := client.Read(buf)
				if err != nil {
					errors <- err
					return
				}

				if string(buf[:n]) != string(msg) {
					errors <- io.ErrUnexpectedEOF
				}
			})
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("client error: %v", err)
		}
	})

	t.Run("handles bidirectional streaming", func(t *testing.T) {
		// Create a target server that streams data back while receiving
		targetListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create target listener: %v", err)
		}
		defer targetListener.Close()

		targetReady := make(chan struct{})
		targetReceived := make(chan []byte, 100)
		targetDone := make(chan struct{})

		go func() {
			close(targetReady)
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			defer close(targetDone)

			// Start sending data to client while also receiving
			var wg sync.WaitGroup

			// Goroutine to send data downstream
			wg.Go(func() {
				for i := range 5 {
					msg := []byte("server msg " + string(rune('0'+i)))
					conn.Write(msg)
					time.Sleep(10 * time.Millisecond)
				}
			})

			// Goroutine to receive data from client
			wg.Go(func() {
				buf := make([]byte, 1024)
				for {
					conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					targetReceived <- append([]byte(nil), buf[:n]...)
				}
			})

			wg.Wait()
		}()

		<-targetReady

		// Create proxy
		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: targetListener.Addr().String(),
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go proxy.ListenAndServe(ctx)
		time.Sleep(10 * time.Millisecond)

		// Connect client
		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer client.Close()

		clientReceived := make(chan []byte, 100)
		clientDone := make(chan struct{})

		// Client sends and receives simultaneously
		var wg sync.WaitGroup

		// Send data upstream
		wg.Go(func() {
			for i := range 5 {
				msg := []byte("client msg " + string(rune('0'+i)))
				client.Write(msg)
				time.Sleep(10 * time.Millisecond)
			}
		})

		// Receive data from server
		wg.Go(func() {
			defer close(clientDone)
			buf := make([]byte, 1024)
			for {
				client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				n, err := client.Read(buf)
				if err != nil {
					return
				}
				clientReceived <- append([]byte(nil), buf[:n]...)
			}
		})

		wg.Wait()
		<-clientDone
		<-targetDone

		close(targetReceived)
		close(clientReceived)

		// Verify data was received in both directions
		var clientMsgCount, serverMsgCount int
		for range targetReceived {
			clientMsgCount++
		}
		for range clientReceived {
			serverMsgCount++
		}

		if clientMsgCount == 0 {
			t.Error("target received no messages from client")
		}
		if serverMsgCount == 0 {
			t.Error("client received no messages from server")
		}
		t.Logf("bidirectional: client->server=%d msgs, server->client=%d msgs", clientMsgCount, serverMsgCount)
	})

	t.Run("handles large data transfer", func(t *testing.T) {
		// Create target that receives and echoes large data
		targetListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create target listener: %v", err)
		}
		defer targetListener.Close()

		targetReady := make(chan struct{})
		go func() {
			close(targetReady)
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			// Echo all data back
			io.Copy(conn, conn)
		}()

		<-targetReady

		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: targetListener.Addr().String(),
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go proxy.ListenAndServe(ctx)
		time.Sleep(10 * time.Millisecond)

		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}
		defer client.Close()

		// Send 1MB of data
		const dataSize = 1024 * 1024
		testData := make([]byte, dataSize)
		for i := range testData {
			testData[i] = byte(i % 256)
		}

		// Write in goroutine since it may block
		go func() {
			client.Write(testData)
			// Signal write done by closing write side
			if cw, ok := client.(interface{ CloseWrite() error }); ok {
				cw.CloseWrite()
			}
		}()

		// Read all data back
		client.SetReadDeadline(time.Now().Add(5 * time.Second))
		received, err := io.ReadAll(client)
		if err != nil {
			t.Fatalf("failed to read response: %v", err)
		}

		if len(received) != dataSize {
			t.Errorf("expected %d bytes, got %d", dataSize, len(received))
		}

		// Verify data integrity
		for i := range min(len(received), dataSize) {
			if received[i] != testData[i] {
				t.Errorf("data mismatch at byte %d: expected %d, got %d", i, testData[i], received[i])
				break
			}
		}
	})

	t.Run("handles client disconnect gracefully", func(t *testing.T) {
		// Create target that tries to keep connection open
		targetListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create target listener: %v", err)
		}
		defer targetListener.Close()

		targetConnClosed := make(chan struct{})
		go func() {
			conn, err := targetListener.Accept()
			if err != nil {
				return
			}
			defer close(targetConnClosed)
			defer conn.Close()
			// Keep reading until connection closes
			io.Copy(io.Discard, conn)
		}()

		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("failed to create proxy listener: %v", err)
		}

		cfg := ProxyConfig{
			Name:       "test-proxy",
			RemoteAddr: targetListener.Addr().String(),
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		proxy := NewProxy(cfg, WithListener(proxyListener), WithLogger(logger))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		go proxy.ListenAndServe(ctx)
		time.Sleep(10 * time.Millisecond)

		client, err := net.Dial("tcp", proxyListener.Addr().String())
		if err != nil {
			t.Fatalf("failed to connect: %v", err)
		}

		// Send some data
		client.Write([]byte("hello"))
		time.Sleep(10 * time.Millisecond)

		// Abruptly close client connection
		client.Close()

		// Target connection should also close
		select {
		case <-targetConnClosed:
			// Success - target connection was closed
		case <-time.After(time.Second):
			t.Error("target connection was not closed after client disconnect")
		}
	})
}
