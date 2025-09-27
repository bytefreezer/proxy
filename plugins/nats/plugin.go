package nats

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
	"github.com/nats-io/nats.go"
)

// Plugin implements the NATS input plugin
type Plugin struct {
	config        Config
	conn          *nats.Conn
	subscriptions []*nats.Subscription
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	health        plugins.PluginHealth
	mu            sync.RWMutex
	output        chan<- *plugins.DataMessage
	metrics       PluginMetrics
}

// Config represents NATS plugin configuration
type Config struct {
	Servers       []string `mapstructure:"servers"`
	Subjects      []string `mapstructure:"subjects"`
	QueueGroup    string   `mapstructure:"queue_group,omitempty"`
	TenantID      string   `mapstructure:"tenant_id"`
	DatasetID     string   `mapstructure:"dataset_id"`
	DataHint      string   `mapstructure:"data_hint,omitempty"`      // data format hint (e.g., "json", "ndjson", "raw")
	BearerToken   string   `mapstructure:"bearer_token,omitempty"`
	MaxReconnect  int      `mapstructure:"max_reconnect,omitempty"`
	ReconnectWait int      `mapstructure:"reconnect_wait,omitempty"` // seconds
	Timeout       int      `mapstructure:"timeout,omitempty"`        // seconds
	UserCreds     string   `mapstructure:"user_creds,omitempty"`     // path to user credentials file
	Token         string   `mapstructure:"token,omitempty"`          // auth token
}

// PluginMetrics tracks NATS plugin metrics
type PluginMetrics struct {
	MessagesReceived uint64
	BytesReceived    uint64
	MessagesDropped  uint64
	LastMessageTime  time.Time
	StartTime        time.Time
	Subjects         map[string]uint64 // subject -> message count
}

// NewPlugin creates a new NATS plugin instance
func NewPlugin() plugins.InputPlugin {
	return &Plugin{
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		metrics: PluginMetrics{
			StartTime: time.Now(),
			Subjects:  make(map[string]uint64),
		},
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "nats"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode NATS plugin config: %w", err)
	}

	// Validate required fields
	if len(p.config.Servers) == 0 {
		p.config.Servers = []string{nats.DefaultURL}
	}
	if len(p.config.Subjects) == 0 {
		return fmt.Errorf("subjects are required for NATS plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for NATS plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for NATS plugin")
	}

	// Set defaults
	if p.config.MaxReconnect == 0 {
		p.config.MaxReconnect = -1 // Unlimited reconnects
	}
	if p.config.ReconnectWait == 0 {
		p.config.ReconnectWait = 2 // 2 seconds
	}
	if p.config.Timeout == 0 {
		p.config.Timeout = 10 // 10 seconds
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("NATS plugin configured: servers=%v subjects=%v -> %s/%s",
		p.config.Servers, p.config.Subjects, p.config.TenantID, p.config.DatasetID)

	return nil
}

// Start begins consuming NATS messages
func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.output = output
	p.metrics.StartTime = time.Now()

	// Create NATS connection options
	opts := []nats.Option{
		nats.MaxReconnects(p.config.MaxReconnect),
		nats.ReconnectWait(time.Duration(p.config.ReconnectWait) * time.Second),
		nats.Timeout(time.Duration(p.config.Timeout) * time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				log.Warnf("NATS disconnected: %v", err)
				p.updateHealth(plugins.HealthStatusUnhealthy, "NATS disconnected", err.Error())
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Infof("NATS reconnected to %s", nc.ConnectedUrl())
			p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("NATS connected to %s", nc.ConnectedUrl()), "")
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			log.Info("NATS connection closed")
		}),
	}

	// Add authentication if configured
	if p.config.UserCreds != "" {
		opts = append(opts, nats.UserCredentials(p.config.UserCreds))
	}
	if p.config.Token != "" {
		opts = append(opts, nats.Token(p.config.Token))
	}

	// Connect to NATS
	serverURLs := strings.Join(p.config.Servers, ",")
	conn, err := nats.Connect(serverURLs, opts...)
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to connect to NATS", err.Error())
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}

	p.conn = conn
	p.updateHealth(plugins.HealthStatusStarting, "Creating NATS subscriptions", "")

	// Create subscriptions for each subject
	for _, subject := range p.config.Subjects {
		var sub *nats.Subscription
		var err error

		if p.config.QueueGroup != "" {
			sub, err = conn.QueueSubscribe(subject, p.config.QueueGroup, p.messageHandler)
		} else {
			sub, err = conn.Subscribe(subject, p.messageHandler)
		}

		if err != nil {
			p.updateHealth(plugins.HealthStatusUnhealthy, fmt.Sprintf("Failed to subscribe to %s", subject), err.Error())
			return fmt.Errorf("failed to subscribe to subject %s: %w", subject, err)
		}

		p.subscriptions = append(p.subscriptions, sub)
		log.Infof("NATS plugin subscribed to subject: %s", subject)
	}

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("NATS active on %s for subjects: %v", conn.ConnectedUrl(), p.config.Subjects), "")
	log.Infof("NATS plugin started for subjects %v", p.config.Subjects)

	return nil
}

// Stop gracefully shuts down the NATS plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down NATS connection", "")

	// Cancel context
	p.cancel()

	// Unsubscribe from all subjects
	for i, sub := range p.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			log.Errorf("Error unsubscribing from NATS subscription %d: %v", i, err)
		}
	}
	p.subscriptions = nil

	// Close NATS connection
	if p.conn != nil {
		p.conn.Close()
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.conn = nil

	p.updateHealth(plugins.HealthStatusStopped, "NATS connection stopped", "")
	log.Infof("NATS plugin stopped")

	return nil
}

// Health returns the current plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// updateHealth updates the plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message, lastError string) {
	p.health = plugins.PluginHealth{
		Status:      status,
		Message:     message,
		LastError:   lastError,
		LastUpdated: time.Now(),
	}
}

// messageHandler processes incoming NATS messages
func (p *Plugin) messageHandler(msg *nats.Msg) {
	// Update metrics
	p.mu.Lock()
	p.metrics.MessagesReceived++
	p.metrics.BytesReceived += uint64(len(msg.Data))
	p.metrics.LastMessageTime = time.Now()
	p.metrics.Subjects[msg.Subject]++
	p.mu.Unlock()

	// Format data according to data hint
	formattedData := msg.Data
	if p.config.DataHint != "" {
		var err error
		formatter := plugins.GetFormatter(p.config.DataHint)
		formattedData, err = formatter.Format(msg.Data)
		if err != nil {
			log.Warnf("Data formatting failed for NATS message from subject %s (format: %s): %v", msg.Subject, p.config.DataHint, err)
			// Continue with original data if formatting fails
			formattedData = msg.Data
		}
	}

	// Create data message for output
	dataMsg := &plugins.DataMessage{
		Data:      formattedData,
		TenantID:  p.config.TenantID,
		DatasetID: p.config.DatasetID,
		DataHint:  p.config.DataHint,
		Timestamp: time.Now(),
		Metadata: map[string]string{
			"subject": msg.Subject,
			"plugin":  "nats",
		},
	}

	// Add reply subject if present
	if msg.Reply != "" {
		dataMsg.Metadata["reply"] = msg.Reply
	}

	// Add queue group if configured
	if p.config.QueueGroup != "" {
		dataMsg.Metadata["queue_group"] = p.config.QueueGroup
	}

	// Add bearer token if configured
	if p.config.BearerToken != "" {
		dataMsg.Metadata["bearer_token"] = p.config.BearerToken
	}

	// Add message headers if present (NATS 2.2+)
	if msg.Header != nil {
		for key, values := range msg.Header {
			if len(values) > 0 {
				dataMsg.Metadata[fmt.Sprintf("header_%s", key)] = values[0]
			}
		}
	}

	// Send to output channel
	select {
	case p.output <- dataMsg:
	case <-p.ctx.Done():
		return
	default:
		log.Warnf("Output channel full, dropping NATS message from subject %s", msg.Subject)
		p.mu.Lock()
		p.metrics.MessagesDropped++
		p.mu.Unlock()
	}
}
