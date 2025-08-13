package services

import (
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	METER = "otel-meter"
)

type Services struct {
	Config    *config.Config
	OtelMeter metric.Meter
}

type HealthService interface {
	GetHealth() bool
}

func NewServices(conf *config.Config) *Services {
	return &Services{
		Config:    conf,
		OtelMeter: otel.Meter(METER),
	}
}
