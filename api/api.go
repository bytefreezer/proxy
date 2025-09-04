package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/n0needt0/bytefreezer-proxy/config"
	"github.com/n0needt0/bytefreezer-proxy/services"
	"github.com/n0needt0/go-goodies/log"
	"github.com/swaggest/openapi-go/openapi3"
	"github.com/swaggest/rest/web"
	swgui "github.com/swaggest/swgui/v5emb"
	"go.opentelemetry.io/otel/metric"
)

type APIServer struct {
	Services   *services.Services
	ApiMetrics map[string]metric.Int64Counter
	HttpServer *http.Server
	sync.RWMutex
	Config *config.Config
}

// NewAPIServer creates a new API server instance
func NewAPIServer(services *services.Services, conf *config.Config) *APIServer {
	return &APIServer{
		Services:   services,
		ApiMetrics: make(map[string]metric.Int64Counter),
		Config:     conf,
	}
}

// NewRouter returns a new router serving API endpoints
func (apiServer *APIServer) NewRouter() *web.Service {
	service := web.NewService(openapi3.NewReflector())

	// Configure OpenAPI schema
	service.OpenAPISchema().SetTitle("ByteFreezer Proxy API")
	service.OpenAPISchema().SetDescription("ByteFreezer Proxy UDP data collection and forwarding API")
	service.OpenAPISchema().SetVersion("v2.0.0")

	// Apply defaults for decoder factory
	service.DecoderFactory.ApplyDefaults = true

	// Wrap to finalize middleware setup
	service.Wrap()

	// Create API instance for handlers
	api := NewAPI(apiServer.Services, apiServer.Config)

	// Health check endpoint
	service.Get("/api/v2/health", api.HealthCheck())

	// Configuration endpoints
	service.Get("/api/v2/config", api.GetConfig())

	// API documentation
	service.Docs("/v2/docs", swgui.New)

	// Root redirect to documentation
	service.Router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v2/docs", http.StatusFound)
	})

	return service
}

// Serve serves http endpoints
func (apiServer *APIServer) Serve(address string, router http.Handler) {
	log.Infof("API server started on %s", address)

	apiServer.HttpServer = &http.Server{
		Addr:           address,
		Handler:        router,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	err := apiServer.HttpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		log.Info("API server closed")
	} else {
		log.Errorf("API server failed and closed: %v", err)
	}
}

// Stop stops the server
func (apiServer *APIServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer func() {
		apiServer.HttpServer = nil
		cancel()
	}()

	if apiServer.HttpServer != nil {
		if err := apiServer.HttpServer.Shutdown(ctx); err != nil {
			log.Errorf("error shutting down API server: %v", err)
		}
	}

	log.Info("API server shut down gracefully")
}
