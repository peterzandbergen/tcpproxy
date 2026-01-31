package proxy

import (
	"context"
	"log/slog"
	"sync"
)

func (ps Proxies) runProxies(ctx context.Context) error {
	var wg sync.WaitGroup
	// Start all proxies
	for _, p := range ps {
		wg.Go(func() {
			if err := p.ListenAndServe(ctx); err != nil {
				slog.Error("proxy stopped with error", "proxy", p, "error", err)
			}
		})
	}
	wg.Wait()
	return nil
}

type Proxies []*Proxy

func NewProxies(proxies []*Proxy) Proxies {
	return Proxies(proxies)
}

func (ps Proxies) ListenAndServe(ctx context.Context) error {
	return ps.runProxies(ctx)
}
