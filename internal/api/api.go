package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/n0needt0/go-goodies/log"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/services"
	"github.com/swaggest/openapi-go/openapi3"
	"github.com/swaggest/rest/web"
	swgui "github.com/swaggest/swgui/v5emb"
	"go.opentelemetry.io/otel/metric"
)

type API struct {
	Services   *services.Services
	ApiMetrics map[string]metric.Int64Counter
	HttpServer *http.Server
	sync.RWMutex
	Config *config.Config
}

// new api
func NewAPI(services *services.Services, conf *config.Config) *API {

	//here we set map of metrics
	//for now counter only
	//each function can have its own metric
	//each metric can have its own label and is created and kept as a global map of counters
	//this is not the best way to do it but it is the simplest way to do it

	return &API{
		Services:   services,
		ApiMetrics: make(map[string]metric.Int64Counter),
		Config:     conf,
	}
}

// UseMetric returns a metric with a label and description
// if the metric is already there it will return it
// metric label is root/something

func (api *API) UseMetric(label, description string) metric.Int64Counter {

	//here we create a new metrics unless it is already there
	mtr, ok := api.ApiMetrics[label]
	if !ok {
		//initialize
		m, err := api.Services.OtelMeter.Int64Counter(label, metric.WithDescription(description))

		if err != nil {
			log.Error("failed to init the metrics" + err.Error())
			//TODO not sure if we should return nothing or panic

		} else {
			api.Lock()
			api.ApiMetrics[label] = m
			api.Unlock()
			mtr = m
		}
	}

	return mtr

}

// NewRouter returns a new router serving API endpoints
func (api *API) NewRouter() *web.Service {
	service := web.DefaultService()
	service.OpenAPI.Info.Title = "bytefreezer-proxy API"
	service.OpenAPI.Info.WithDescription("This bytefreezer-proxy API")
	service.OpenAPI.Info.Version = "v1.0.0"
	tags := []struct{ name, description string }{
		{"bytefreezer-proxy", "Provides API for bytefreezer-proxy"},
	}
	apiTags := make([]openapi3.Tag, len(tags))
	for i, t := range tags {
		apiTags[i] = openapi3.Tag{Name: t.name, Description: &t.description}
	}
	service.OpenAPI.WithTags(apiTags...)
	service.DecoderFactory.ApplyDefaults = true

	service.Get("/api/v2/health", api.HealthCheck())

	// use /docs for docs UI and redirect from / to /docs
	service.Docs("/v2/docs", swgui.New)

	service.Router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.RequestURI+"v2/docs", http.StatusFound)
	})

	return service
}

// Serve serves http endpoints
func (api *API) Serve(address string, router http.Handler) {
	log.Infof("api server started: on %s", address)

	api.HttpServer = &http.Server{Addr: address, Handler: router}
	err := api.HttpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		log.Info("api server closed")
	} else {
		log.Errorf("api server failed and closed: %v", err)
	}
}

// Stop stops the server
func (api *API) Stop() {

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer func() {
		api.HttpServer = nil
		cancel()
	}()

	if err := api.HttpServer.Shutdown(ctx); err != nil {
		log.Fatal("error shutting down server")
	}
}
