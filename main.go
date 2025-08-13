package main

import (
	"encoding/json"
	"flag"
	"path/filepath"
	"sort"

	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/n0needt0/go-goodies/log"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/api"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/bytefreezer"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/services"
	"github.com/pkg/errors"
)

var (
	conf      = config.Config{}
	envPrefix = "TAR_"
)

// Execute executes the root command.
func Run() error {

	var cfgFilePath string

	flag.StringVar(&cfgFilePath, "config", "config.yaml", "--config <FILE>")
	flag.Parse()

	err := config.LoadConfig(cfgFilePath, envPrefix, &conf)
	if err != nil {
		return errors.Wrap(err, "failed to load config")
	}

	setLogLevel(conf.Logging.Level)

	var otelshutdown func()

	if conf.Otel.Enabled {
		//this initializes global otel provider
		otelshutdown = InitOtelProvider(&conf)
	}

	// Business Logic
	services := services.NewServices(&conf)

	//start stab server
	server := NewServer(services, &conf)

	server.HttpApi = api.NewAPI(services, &conf)

	//create datachannel
	datachan := make(chan bytefreezer.UploadTask, conf.Bytefreezer.DataChannelSize)

	server.S3Uploader = bytefreezer.NewUploader(services, &conf, datachan)

	//start consumers
	go server.S3Uploader.Start()

	//create shared schema
	//this is used to share schema between udp and webhook listeners

	schema := &bytefreezer.SharedSchema{Current: make(map[string]string)}

	server.UdpListener = bytefreezer.NewUdpListener(services, &conf, datachan, schema)

	server.WebhookListener = bytefreezer.NewWebhookListener(services, &conf, datachan, schema)

	//start listeners
	go server.UdpListener.Listen()
	go server.WebhookListener.Listen()

	//start server
	go server.Start(nil, nil)

	//start api server
	server.HttpApi.Serve(":"+strconv.Itoa(conf.Server.Port), server.HttpApi.NewRouter())

	if conf.Otel.Enabled {
		//cleanup otel
		otelshutdown()
	}

	return nil
}

func setLogLevel(levelStr string) {
	switch strings.ToLower(levelStr) {
	case "debug":
		log.SetMinLogLevel(log.MinLevelDebug)
	case "info":
		log.SetMinLogLevel(log.MinLevelInfo)
	case "warn":
		log.SetMinLogLevel(log.MinLevelWarn)
	case "error":
		log.SetMinLogLevel(log.MinLevelError)
	}
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

// Server provides basic service functions and state common to all service types
type Server struct {
	Config          *config.Config
	Name            string
	quitterC        chan time.Duration // also internal-only
	HttpApi         *api.API
	UdpListener     *bytefreezer.UdpListener
	WebhookListener *bytefreezer.WebhookListener
	S3Uploader      *bytefreezer.Uploader
	Services        *services.Services
	//here you can add other services
}

// New creates a new Server
// modify it at will
//notice each provider runs in onw goroutine

func NewServer(services *services.Services, conf *config.Config) *Server {
	return &Server{
		Config:   conf,
		Name:     conf.App.Name,
		quitterC: make(chan time.Duration),
		Services: services,
	}
}

func (svc *Server) Start(housekeepingFn func(), quitterFn func(time.Duration)) {

	// exit cleanly on signal
	signalC := make(chan os.Signal, 1)
	signal.Notify(signalC, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGABRT, syscall.SIGTERM)
	go func() {
		sig := <-signalC
		log.Debugf("Received signal %v", sig)

		if err := svc.Stop(2 * time.Second); err != nil {
			log.Fatalf("error stopping service: %v", err)
		}
	}()

	interval := time.Duration(svc.Config.Housekeeping.IntervalSeconds) * time.Second

	if interval <= 0 {
		interval = 10 * time.Second
		log.Errorf("invalid housekeeping-interval: %d", interval)
	}

	ticker := time.NewTicker(interval)

	// wait for quit, run housekeeping (if any)
	//
	for {
		select {
		case <-ticker.C:
			log.Debug("cleaning old schemas")

			matches, _ := filepath.Glob("/tmp/bytefreezer-*.schema.json")
			if len(matches) > 10 {
				sort.Strings(matches)
				old := matches[:len(matches)-10]
				for _, f := range old {
					_ = os.Remove(f)
				}
			}

			if housekeepingFn != nil && svc.Config.Housekeeping.Enabled {
				housekeepingFn()
			}

			//here we call back housekeeping function to home ship
			//getting config data for this instance
		case timeout := <-svc.quitterC:
			log.Debug("housekeeping")

			if quitterFn != nil {
				quitterFn(timeout)
			}

			//lets bring em down one by one
			svc.UdpListener.Shutdown()
			svc.WebhookListener.Shutdown()

			svc.HttpApi.Stop()

			return
		}
	}
}

func (svc *Server) Stop(timeout time.Duration) error {
	defer close(svc.quitterC)

	log.Debugf("sending timeout %s to quitterC:", timeout)

	select {
	case svc.quitterC <- timeout:
		log.Debug("sent")
	case <-time.After(timeout + (100 * time.Millisecond)):
		log.Debug("timed out")
	default:
		log.Debug("must have already closed")
	}
	return nil
}

func main() {

	err := Run()
	if err != nil {
		log.Fatalf("failed to start: %s\n", err.Error())
		os.Exit(11)
	}
}
