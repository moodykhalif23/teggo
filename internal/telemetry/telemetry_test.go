package telemetry

import (
	"context"
	"testing"
)

func TestSetupDisabled(t *testing.T) {
	// No OTLP endpoint in the environment → metrics disabled, no-op shutdown.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	shutdown, err := Setup(context.Background(), "test", "0")
	if err != nil {
		t.Fatalf("Setup (disabled): %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown must be non-nil even when disabled")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown: %v", err)
	}
}

func TestSetupEnabled(t *testing.T) {
	// An endpoint turns on the OTLP exporter + MeterProvider. The exporter is
	// lazy (no dial here), so Setup must succeed without a live collector.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	shutdown, err := Setup(context.Background(), "test", "0")
	if err != nil {
		t.Fatalf("Setup (enabled): %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown must be non-nil when enabled")
	}
	// Shutdown flushes; against an absent collector it returns a connection
	// error, which itself confirms the export path is live. We only require it
	// not to panic/hang.
	_ = shutdown(context.Background())
}
