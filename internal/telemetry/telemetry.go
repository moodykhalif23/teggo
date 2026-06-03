// Package telemetry wires OpenTelemetry metrics. Export is via OTLP/HTTP and is
// opt-in: when no OTLP endpoint is configured the global no-op MeterProvider
// stays in place, so instruments compile and run at zero cost and nothing is
// emitted. Point OTEL_EXPORTER_OTLP_ENDPOINT (or the metrics-specific variant)
// at a collector to turn it on.
package telemetry

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// enabled reports whether an OTLP endpoint is configured.
func enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
}

func Setup(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !enabled() {
		return noop, nil
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
	))
	if err != nil {
		return nil, err
	}
	exp, err := otlpmetrichttp.New(ctx) // honours OTEL_EXPORTER_OTLP_* env
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
	)
	otel.SetMeterProvider(mp)
	return mp.Shutdown, nil
}

// RegisterPoolMetrics publishes pgx connection-pool gauges via the global meter.
// No-op in effect when metrics are disabled (the callback runs against the
// no-op provider). Safe to call once at startup.
func RegisterPoolMetrics(pool *pgxpool.Pool) error {
	m := otel.Meter("b2bcommerce/db")
	total, err := m.Int64ObservableGauge("db.pool.connections.total")
	if err != nil {
		return err
	}
	idle, err := m.Int64ObservableGauge("db.pool.connections.idle")
	if err != nil {
		return err
	}
	acquired, err := m.Int64ObservableGauge("db.pool.connections.acquired")
	if err != nil {
		return err
	}
	_, err = m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(total, int64(s.TotalConns()))
		o.ObserveInt64(idle, int64(s.IdleConns()))
		o.ObserveInt64(acquired, int64(s.AcquiredConns()))
		return nil
	}, total, idle, acquired)
	return err
}
