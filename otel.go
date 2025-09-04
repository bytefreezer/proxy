package main

import (
	"context"
	"fmt"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/go-goodies/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func initOTEL(cfg *config.Config) (func(), error) {
	ctx := context.Background()

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.Otel.ServiceName),
			semconv.ServiceVersionKey.String(cfg.App.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Set up trace provider
	traceCleanup, err := setupTracing(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("failed to setup tracing: %w", err)
	}

	// Set up metric provider
	metricCleanup, err := setupMetrics(ctx, cfg, res)
	if err != nil {
		traceCleanup()
		return nil, fmt.Errorf("failed to setup metrics: %w", err)
	}

	log.Infof("OTEL initialized with endpoint: %s", cfg.Otel.Endpoint)

	return func() {
		metricCleanup()
		traceCleanup()
		log.Info("OTEL cleanup completed")
	}, nil
}

func setupTracing(ctx context.Context, cfg *config.Config, res *resource.Resource) (func(), error) {
	// Create trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Otel.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create trace provider
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(traceProvider)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := traceProvider.Shutdown(ctx); err != nil {
			log.Errorf("Failed to shutdown trace provider: %v", err)
		}
	}, nil
}

func setupMetrics(ctx context.Context, cfg *config.Config, res *resource.Resource) (func(), error) {
	// Create metric exporter
	metricExporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Otel.Endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Create metric provider
	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(
			metricExporter,
			sdkmetric.WithInterval(time.Duration(cfg.Otel.ScrapeIntervalSeconds)*time.Second),
		)),
		sdkmetric.WithResource(res),
		sdkmetric.WithView(
			// Add custom views if needed
			sdkmetric.NewView(
				sdkmetric.Instrument{
					Name: "bytefreezer_proxy_*",
					Scope: instrumentation.Scope{
						Name: cfg.Otel.ServiceName,
					},
				},
				sdkmetric.Stream{
					Aggregation: sdkmetric.AggregationDefault{},
				},
			),
		),
	)

	otel.SetMeterProvider(metricProvider)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := metricProvider.Shutdown(ctx); err != nil {
			log.Errorf("Failed to shutdown metric provider: %v", err)
		}
	}, nil
}

// GetMeter returns an OTEL meter for the proxy service
func GetMeter() metric.Meter {
	return otel.Meter("bytefreezer-proxy")
}