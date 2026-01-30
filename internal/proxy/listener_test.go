package proxy

import (
	"context"
	"testing"
)

func TestProxy(t *testing.T) {

	t.Run("Create and start proxy", func(t *testing.T) {
		cfg := ProxyConfig{
			Name:       "test-proxy",
			Port: "9000",
			RemoteAddr: "localhost:9001",
		}
		proxy := NewProxy(cfg)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		err := proxy.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to create proxy: %v", err)
		}
	})
}
