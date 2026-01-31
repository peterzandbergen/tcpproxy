// Package slogmulti provides a multi-handler that fans out log records to multiple slog handlers.
package slogmulti

import (
	"context"
	"log/slog"
)

// Handler fans out log records to multiple handlers.
type Handler struct {
	handlers []slog.Handler
}

// Fanout creates a new Handler that sends records to all provided handlers.
func New(handlers ...slog.Handler) *Handler {
	return &Handler{handlers: handlers}
}

// Enabled returns true if any handler is enabled for the given level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle sends the record to all handlers that are enabled for the record's level.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

// WithAttrs returns a new Handler with the given attributes added to all handlers.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &Handler{handlers: handlers}
}

// WithGroup returns a new Handler with the given group added to all handlers.
func (h *Handler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &Handler{handlers: handlers}
}
