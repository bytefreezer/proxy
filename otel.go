package main

import (
	"context"
	"time"

	"github.com/n0needt0/go-goodies/log"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// this initializes global otel provider
func InitOtelProvider(conf *config.Config) func() {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String(conf.App.Name),
		),
	)

	if err != nil {
		log.Errorf("Failed to init otel provider: %v", err)
	}

	metricExp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(conf.Otel.Endpoint),
	)
	if err != nil {
		log.Errorf("failed to create the collector metric exporter: %v", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(time.Duration(conf.Otel.ScrapeIntervalSeconds)*time.Second),
			),
		),
	)
	otel.SetMeterProvider(meterProvider)

	return func() {
		cxt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()

		// pushes any last exports to the receiver
		if err := meterProvider.Shutdown(cxt); err != nil {
			if err != nil {
				log.Errorf("failed to push last exports: %v", err)
			}
			otel.Handle(err)
		}
	}
}
