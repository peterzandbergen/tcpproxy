package slogmulti

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestFanout_Handle(t *testing.T) {
	tests := []struct {
		name       string
		level      slog.Level
		message    string
		wantInBoth bool
	}{
		{
			name:       "info message logged to both handlers",
			level:      slog.LevelInfo,
			message:    "test info message",
			wantInBoth: true,
		},
		{
			name:       "error message logged to both handlers",
			level:      slog.LevelError,
			message:    "test error message",
			wantInBoth: true,
		},
		{
			name:       "debug message logged to both handlers",
			level:      slog.LevelDebug,
			message:    "test debug message",
			wantInBoth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf1 := &bytes.Buffer{}
			buf2 := &bytes.Buffer{}

			opts := &slog.HandlerOptions{Level: slog.LevelDebug}
			h1 := slog.NewTextHandler(buf1, opts)
			h2 := slog.NewTextHandler(buf2, opts)

			logger := slog.New(New(h1, h2))
			logger.Log(context.Background(), tt.level, tt.message)

			if tt.wantInBoth {
				if !strings.Contains(buf1.String(), tt.message) {
					t.Errorf("handler 1 missing message, got: %s", buf1.String())
				}
				if !strings.Contains(buf2.String(), tt.message) {
					t.Errorf("handler 2 missing message, got: %s", buf2.String())
				}
			}
		})
	}
}

func TestFanout_Enabled(t *testing.T) {
	tests := []struct {
		name        string
		h1Level     slog.Level
		h2Level     slog.Level
		checkLevel  slog.Level
		wantEnabled bool
	}{
		{
			name:        "enabled when both handlers accept level",
			h1Level:     slog.LevelDebug,
			h2Level:     slog.LevelDebug,
			checkLevel:  slog.LevelInfo,
			wantEnabled: true,
		},
		{
			name:        "enabled when one handler accepts level",
			h1Level:     slog.LevelError,
			h2Level:     slog.LevelDebug,
			checkLevel:  slog.LevelInfo,
			wantEnabled: true,
		},
		{
			name:        "disabled when no handler accepts level",
			h1Level:     slog.LevelError,
			h2Level:     slog.LevelError,
			checkLevel:  slog.LevelInfo,
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			h1 := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: tt.h1Level})
			h2 := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: tt.h2Level})

			handler := New(h1, h2)
			got := handler.Enabled(context.Background(), tt.checkLevel)

			if got != tt.wantEnabled {
				t.Errorf("Enabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

func TestFanout_WithAttrs(t *testing.T) {
	buf1 := &bytes.Buffer{}
	buf2 := &bytes.Buffer{}

	h1 := slog.NewTextHandler(buf1, nil)
	h2 := slog.NewTextHandler(buf2, nil)

	handler := New(h1, h2)
	handlerWithAttrs := handler.WithAttrs([]slog.Attr{slog.String("key", "value")})

	logger := slog.New(handlerWithAttrs)
	logger.Info("test message")

	if !strings.Contains(buf1.String(), "key=value") {
		t.Errorf("handler 1 missing attribute, got: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "key=value") {
		t.Errorf("handler 2 missing attribute, got: %s", buf2.String())
	}
}

func TestFanout_WithGroup(t *testing.T) {
	buf1 := &bytes.Buffer{}
	buf2 := &bytes.Buffer{}

	h1 := slog.NewTextHandler(buf1, nil)
	h2 := slog.NewTextHandler(buf2, nil)

	handler := New(h1, h2)
	handlerWithGroup := handler.WithGroup("mygroup")

	logger := slog.New(handlerWithGroup)
	logger.Info("test message", "key", "value")

	if !strings.Contains(buf1.String(), "mygroup.key=value") {
		t.Errorf("handler 1 missing group, got: %s", buf1.String())
	}
	if !strings.Contains(buf2.String(), "mygroup.key=value") {
		t.Errorf("handler 2 missing group, got: %s", buf2.String())
	}
}

func TestFanout_HandleRespectsLevel(t *testing.T) {
	buf1 := &bytes.Buffer{}
	buf2 := &bytes.Buffer{}

	// h1 only accepts Error and above
	h1 := slog.NewTextHandler(buf1, &slog.HandlerOptions{Level: slog.LevelError})
	// h2 accepts Info and above
	h2 := slog.NewTextHandler(buf2, &slog.HandlerOptions{Level: slog.LevelInfo})

	logger := slog.New(New(h1, h2))
	logger.Info("info message")

	// h1 should not have the message (level too low)
	if strings.Contains(buf1.String(), "info message") {
		t.Errorf("handler 1 should not have info message, got: %s", buf1.String())
	}
	// h2 should have the message
	if !strings.Contains(buf2.String(), "info message") {
		t.Errorf("handler 2 should have info message, got: %s", buf2.String())
	}
}

func TestFanout_EmptyHandlers(t *testing.T) {
	handler := New()

	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled() should return false with no handlers")
	}

	// Should not panic
	err := handler.Handle(context.Background(), slog.Record{})
	if err != nil {
		t.Errorf("Handle() returned error: %v", err)
	}
}
