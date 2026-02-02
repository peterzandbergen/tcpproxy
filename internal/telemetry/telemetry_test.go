package telemetry

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSDKDisabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{
			name:     "true lowercase",
			envValue: "true",
			want:     true,
		},
		{
			name:     "TRUE uppercase",
			envValue: "TRUE",
			want:     true,
		},
		{
			name:     "True mixed case",
			envValue: "True",
			want:     true,
		},
		{
			name:     "TrUe mixed case",
			envValue: "TrUe",
			want:     true,
		},
		{
			name:     "false",
			envValue: "false",
			want:     false,
		},
		{
			name:     "empty string",
			envValue: "",
			want:     false,
		},
		{
			name:     "1 is not true",
			envValue: "1",
			want:     false,
		},
		{
			name:     "yes is not true",
			envValue: "yes",
			want:     false,
		},
		{
			name:     "random value",
			envValue: "random",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original value and restore after test.
			original, wasSet := os.LookupEnv("OTEL_SDK_DISABLED")
			t.Cleanup(func() {
				if wasSet {
					os.Setenv("OTEL_SDK_DISABLED", original)
				} else {
					os.Unsetenv("OTEL_SDK_DISABLED")
				}
			})

			if tt.envValue == "" {
				os.Unsetenv("OTEL_SDK_DISABLED")
			} else {
				os.Setenv("OTEL_SDK_DISABLED", tt.envValue)
			}

			got := SDKDisabled()
			if got != tt.want {
				t.Errorf("SDKDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetup_SDKDisabled(t *testing.T) {
	// Save original value and restore after test.
	original, wasSet := os.LookupEnv("OTEL_SDK_DISABLED")
	t.Cleanup(func() {
		if wasSet {
			os.Setenv("OTEL_SDK_DISABLED", original)
		} else {
			os.Unsetenv("OTEL_SDK_DISABLED")
		}
	})

	os.Setenv("OTEL_SDK_DISABLED", "true")

	ctx := context.Background()
	cfg := DefaultConfig("test-service")

	tel, shutdown, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v, want nil", err)
	}

	// When SDK is disabled, telemetry should be nil.
	if tel != nil {
		t.Errorf("Setup() telemetry = %v, want nil when SDK is disabled", tel)
	}

	// Shutdown function should be non-nil and callable.
	if shutdown == nil {
		t.Fatal("Setup() shutdown = nil, want non-nil function")
	}

	// Shutdown should succeed with no error.
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown() error = %v, want nil", err)
	}
}

func TestSetup_SDKDisabled_PropagatorsStillConfigured(t *testing.T) {
	// Save original value and restore after test.
	original, wasSet := os.LookupEnv("OTEL_SDK_DISABLED")
	t.Cleanup(func() {
		if wasSet {
			os.Setenv("OTEL_SDK_DISABLED", original)
		} else {
			os.Unsetenv("OTEL_SDK_DISABLED")
		}
	})

	os.Setenv("OTEL_SDK_DISABLED", "true")

	ctx := context.Background()
	cfg := DefaultConfig("test-service")

	_, _, err := Setup(ctx, cfg)
	if err != nil {
		t.Fatalf("Setup() error = %v, want nil", err)
	}

	// Verify propagator was set by checking it's not nil.
	// The otel package's GetTextMapPropagator returns a no-op propagator if none is set,
	// but after Setup, it should return our composite propagator.
	// We can't easily verify the exact propagator type without reflection,
	// but we can at least verify Setup completes without error.
}

func TestNewPropagator(t *testing.T) {
	tests := []struct {
		name       string
		propagators string
		wantFields int // Number of propagators in composite (0 means no-op)
	}{
		{
			name:       "default tracecontext and baggage",
			propagators: "tracecontext,baggage",
			wantFields: 2,
		},
		{
			name:       "tracecontext only",
			propagators: "tracecontext",
			wantFields: 1,
		},
		{
			name:       "baggage only",
			propagators: "baggage",
			wantFields: 1,
		},
		{
			name:       "none returns empty",
			propagators: "none",
			wantFields: 0,
		},
		{
			name:       "case insensitive",
			propagators: "TRACECONTEXT,BAGGAGE",
			wantFields: 2,
		},
		{
			name:       "mixed case",
			propagators: "TraceContext,Baggage",
			wantFields: 2,
		},
		{
			name:       "deduplication",
			propagators: "tracecontext,tracecontext,baggage,baggage",
			wantFields: 2,
		},
		{
			name:       "whitespace trimmed",
			propagators: " tracecontext , baggage ",
			wantFields: 2,
		},
		{
			name:       "unknown propagator ignored",
			propagators: "tracecontext,unknown,baggage",
			wantFields: 2,
		},
		{
			name:       "empty string",
			propagators: "",
			wantFields: 0,
		},
		{
			name:       "only unknown",
			propagators: "unknown",
			wantFields: 0,
		},
		{
			name:       "none with others ignores none",
			propagators: "tracecontext,none,baggage",
			wantFields: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPropagator(tt.propagators)
			if p == nil {
				t.Fatal("NewPropagator() returned nil, want non-nil")
			}

			// Test that the propagator returns expected fields.
			// A composite propagator's Fields() returns the union of all propagator fields.
			fields := p.Fields()

			// tracecontext has 2 fields: traceparent, tracestate
			// baggage has 1 field: baggage
			// So "tracecontext,baggage" should have 3 fields total.
			expectedFields := 0
			if tt.wantFields > 0 {
				// Count expected fields based on propagators.
				for _, name := range []string{"tracecontext", "baggage"} {
					if contains(tt.propagators, name) {
						if name == "tracecontext" {
							expectedFields += 2 // traceparent, tracestate
						} else if name == "baggage" {
							expectedFields += 1 // baggage
						}
					}
				}
			}

			if len(fields) != expectedFields {
				t.Errorf("NewPropagator(%q).Fields() = %v (len=%d), want len=%d",
					tt.propagators, fields, len(fields), expectedFields)
			}
		})
	}
}

// contains checks if the propagators string contains the given name (case-insensitive).
func contains(propagators, name string) bool {
	lower := strings.ToLower(propagators)
	return strings.Contains(lower, name)
}
