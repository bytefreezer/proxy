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

// Plugin implements the NATS input plugin with direct filesystem writes
type Plugin struct {
	config        Config
	conn          *nats.Conn
	subscriptions []*nats.Subscription
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	health        plugins.PluginHealth
	mu            sync.RWMutex
	spooler       plugins.SpoolingInterface
	metrics       PluginMetrics
}

// Config represents NATS plugin configuration
type Config struct {
	Servers       []string `mapstructure:"servers"`
	Subjects      []string `mapstructure:"subjects"`
	QueueGroup    string   `mapstructure:"queue_group,omitempty"`
	TenantID      string   `mapstructure:"tenant_id"`
	DatasetID     string   `mapstructure:"dataset_id"`
	DataHint      string   `mapstructure:"data_hint,omitempty"`      // data format hint (e.g., "ndjson", "raw") - use "ndjson" for JSON normalization
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

// Start begins consuming NATS messages with direct filesystem writes
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	// Set default data hint if not specified
	if p.config.DataHint == "" {
		p.config.DataHint = "raw"
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.spooler = spooler
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

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("NATS active with direct spooling on %s for subjects: %v", conn.ConnectedUrl(), p.config.Subjects), "")
	log.Infof("NATS plugin started with direct spooling for subjects %v", p.config.Subjects)

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

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if dataHint == "" {
		dataHint = "raw" // default for NATS
	}
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, formattedData, dataHint); err != nil {
		log.Errorf("Failed to store NATS message to filesystem from subject %s: %v", msg.Subject, err)
		p.mu.Lock()
		p.metrics.MessagesDropped++
		p.mu.Unlock()
		return
	}

	log.Debugf("Stored NATS message from subject %s directly to filesystem (%d bytes)",
		msg.Subject, len(formattedData))
}

// Schema returns the NATS plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "nats",
		DisplayName: "NATS Messaging",
		Description: "NATS messaging subscriber. Subscribes to subjects and outputs structured NDJSON with NATS metadata (subject, headers, reply_to).",
		Category:    "Message Queue",
		Transport:   "TCP",
		DefaultPort: 4222,
		Fields: []plugins.PluginFieldSchema{
			{
				Name:        "servers",
				Type:        "[]string",
				Required:    false,
				Default:     []string{"nats://localhost:4222"},
				Description: "List of NATS server URLs",
				Placeholder: "nats://localhost:4222",
				Group:       "Connection",
			},
			{
				Name:        "subjects",
				Type:        "[]string",
				Required:    true,
				Description: "List of subjects to subscribe to (supports wildcards: * and >)",
				Placeholder: "events.>,logs.*",
				Group:       "Connection",
			},
			{
				Name:        "queue_group",
				Type:        "string",
				Required:    false,
				Description: "Optional queue group for load balancing",
				Placeholder: "workers",
				Group:       "Connection",
			},
			{
				Name:        "max_reconnect",
				Type:        "int",
				Required:    false,
				Default:     -1,
				Description: "Max reconnection attempts (-1 = unlimited)",
				Placeholder: "-1",
				Group:       "Resilience",
			},
			{
				Name:        "reconnect_wait",
				Type:        "int",
				Required:    false,
				Default:     2,
				Description: "Reconnect wait time in seconds",
				Validation:  "min:1,max:60",
				Placeholder: "2",
				Group:       "Resilience",
			},
			{
				Name:        "timeout",
				Type:        "int",
				Required:    false,
				Default:     10,
				Description: "Connection timeout in seconds",
				Validation:  "min:1,max:300",
				Placeholder: "10",
				Group:       "Timeouts",
			},
			{
				Name:        "user_creds",
				Type:        "string",
				Required:    false,
				Description: "Path to NATS user credentials file (.creds)",
				Placeholder: "/path/to/nats.creds",
				Group:       "Security",
			},
			{
				Name:        "token",
				Type:        "string",
				Required:    false,
				Description: "NATS authentication token",
				Placeholder: "secret-token",
				Group:       "Security",
			},
		},
	}
}
