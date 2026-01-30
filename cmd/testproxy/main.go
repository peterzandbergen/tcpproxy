package main

import (
	"context"
	"time"

	"github.com/myhops/tcpproxy/internal/proxy"
)

func main() {
	cfg := proxy.ProxyConfig{
		Name:       "test-proxy",
		Port:       "9000",
		RemoteAddr: "localhost:9001",
	}
	p := proxy.NewProxy(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.Start(ctx)
}
