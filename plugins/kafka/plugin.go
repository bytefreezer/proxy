package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/mitchellh/mapstructure"
	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// Plugin implements the Kafka input plugin
type Plugin struct {
	config        Config
	consumer      sarama.ConsumerGroup
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	health        plugins.PluginHealth
	mu            sync.RWMutex
	output        chan<- *plugins.DataMessage
	metrics       PluginMetrics
	consumerReady chan bool
}

// Config represents Kafka plugin configuration
type Config struct {
	Brokers           []string `mapstructure:"brokers"`
	Topics            []string `mapstructure:"topics"`
	GroupID           string   `mapstructure:"group_id"`
	TenantID          string   `mapstructure:"tenant_id"`
	DatasetID         string   `mapstructure:"dataset_id"`
	BearerToken       string   `mapstructure:"bearer_token,omitempty"`
	AutoOffsetReset   string   `mapstructure:"auto_offset_reset,omitempty"`  // "earliest", "latest"
	SessionTimeout    int      `mapstructure:"session_timeout,omitempty"`    // seconds
	HeartbeatInterval int      `mapstructure:"heartbeat_interval,omitempty"` // seconds
}

// PluginMetrics tracks Kafka plugin metrics
type PluginMetrics struct {
	MessagesReceived uint64
	BytesReceived    uint64
	MessagesDropped  uint64
	LastMessageTime  time.Time
	StartTime        time.Time
	Partition        map[string]int32 // topic -> partition assignments
}

// NewPlugin creates a new Kafka plugin instance
func NewPlugin() plugins.InputPlugin {
	return &Plugin{
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		metrics: PluginMetrics{
			StartTime: time.Now(),
			Partition: make(map[string]int32),
		},
		consumerReady: make(chan bool),
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "kafka"
}

// Configure initializes the plugin with configuration
func (p *Plugin) Configure(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(config, &p.config); err != nil {
		return fmt.Errorf("failed to decode Kafka plugin config: %w", err)
	}

	// Validate required fields
	if len(p.config.Brokers) == 0 {
		return fmt.Errorf("brokers are required for Kafka plugin")
	}
	if len(p.config.Topics) == 0 {
		return fmt.Errorf("topics are required for Kafka plugin")
	}
	if p.config.GroupID == "" {
		return fmt.Errorf("group_id is required for Kafka plugin")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required for Kafka plugin")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required for Kafka plugin")
	}

	// Set defaults
	if p.config.AutoOffsetReset == "" {
		p.config.AutoOffsetReset = "latest"
	}
	if p.config.SessionTimeout == 0 {
		p.config.SessionTimeout = 30 // 30 seconds
	}
	if p.config.HeartbeatInterval == 0 {
		p.config.HeartbeatInterval = 10 // 10 seconds
	}

	p.updateHealth(plugins.HealthStatusStopped, "Plugin configured successfully", "")
	log.Infof("Kafka plugin configured: brokers=%v topics=%v group=%s -> %s/%s",
		p.config.Brokers, p.config.Topics, p.config.GroupID, p.config.TenantID, p.config.DatasetID)

	return nil
}

// Start begins consuming Kafka messages
func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ctx != nil {
		return fmt.Errorf("plugin is already started")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.output = output
	p.metrics.StartTime = time.Now()

	// Create Kafka consumer configuration
	kafkaConfig := sarama.NewConfig()
	kafkaConfig.Consumer.Group.Session.Timeout = time.Duration(p.config.SessionTimeout) * time.Second
	kafkaConfig.Consumer.Group.Heartbeat.Interval = time.Duration(p.config.HeartbeatInterval) * time.Second
	kafkaConfig.Consumer.Return.Errors = true
	kafkaConfig.Version = sarama.V2_8_0_0

	// Set offset reset behavior
	if p.config.AutoOffsetReset == "earliest" {
		kafkaConfig.Consumer.Offsets.Initial = sarama.OffsetOldest
	} else {
		kafkaConfig.Consumer.Offsets.Initial = sarama.OffsetNewest
	}

	// Create consumer group
	consumer, err := sarama.NewConsumerGroup(p.config.Brokers, p.config.GroupID, kafkaConfig)
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, "Failed to create Kafka consumer", err.Error())
		return fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	p.consumer = consumer
	p.updateHealth(plugins.HealthStatusStarting, "Starting Kafka consumer", "")

	// Start consumer goroutine
	p.wg.Add(1)
	go p.consumeLoop()

	// Start error handler
	p.wg.Add(1)
	go p.errorHandler()

	// Wait for consumer to be ready
	select {
	case <-p.consumerReady:
		p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("Kafka consumer active for topics: %v", p.config.Topics), "")
		log.Infof("Kafka plugin started for topics %v", p.config.Topics)
	case <-time.After(30 * time.Second):
		p.updateHealth(plugins.HealthStatusUnhealthy, "Timeout waiting for Kafka consumer to start", "")
		return fmt.Errorf("timeout waiting for Kafka consumer to start")
	case <-p.ctx.Done():
		return fmt.Errorf("context cancelled while starting Kafka consumer")
	}

	return nil
}

// Stop gracefully shuts down the Kafka plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel == nil {
		return nil // Already stopped
	}

	p.updateHealth(plugins.HealthStatusStopping, "Shutting down Kafka consumer", "")

	// Cancel context
	p.cancel()

	// Close consumer
	if p.consumer != nil {
		if err := p.consumer.Close(); err != nil {
			log.Errorf("Error closing Kafka consumer: %v", err)
		}
	}

	// Wait for all goroutines to finish
	p.wg.Wait()

	p.ctx = nil
	p.cancel = nil
	p.consumer = nil

	p.updateHealth(plugins.HealthStatusStopped, "Kafka consumer stopped", "")
	log.Infof("Kafka plugin stopped")

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

// consumeLoop runs the main Kafka consumption loop
func (p *Plugin) consumeLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			err := p.consumer.Consume(p.ctx, p.config.Topics, &consumerHandler{plugin: p})
			if err != nil {
				log.Errorf("Kafka consume error: %v", err)
				p.updateHealth(plugins.HealthStatusUnhealthy, "Consumer error", err.Error())
				// Add backoff before retrying
				select {
				case <-time.After(5 * time.Second):
				case <-p.ctx.Done():
					return
				}
			}
		}
	}
}

// errorHandler handles Kafka consumer errors
func (p *Plugin) errorHandler() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case err, ok := <-p.consumer.Errors():
			if !ok {
				return
			}
			log.Errorf("Kafka consumer error: %v", err)
			p.updateHealth(plugins.HealthStatusUnhealthy, "Consumer error", err.Error())
		}
	}
}

// consumerHandler implements sarama.ConsumerGroupHandler
type consumerHandler struct {
	plugin *Plugin
}

func (h *consumerHandler) Setup(sarama.ConsumerGroupSession) error {
	// Signal that consumer is ready
	close(h.plugin.consumerReady)
	return nil
}

func (h *consumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *consumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case <-h.plugin.ctx.Done():
			return nil
		case message, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			h.plugin.processMessage(message, session)
		}
	}
}

// processMessage processes a Kafka message
func (p *Plugin) processMessage(msg *sarama.ConsumerMessage, session sarama.ConsumerGroupSession) {
	// Update metrics
	p.metrics.MessagesReceived++
	p.metrics.BytesReceived += uint64(len(msg.Value))
	p.metrics.LastMessageTime = time.Now()
	p.metrics.Partition[msg.Topic] = msg.Partition

	// Create data message for output
	dataMsg := &plugins.DataMessage{
		Data:      msg.Value,
		TenantID:  p.config.TenantID,
		DatasetID: p.config.DatasetID,
		Timestamp: msg.Timestamp,
		Metadata: map[string]string{
			"topic":     msg.Topic,
			"partition": fmt.Sprintf("%d", msg.Partition),
			"offset":    fmt.Sprintf("%d", msg.Offset),
			"plugin":    "kafka",
		},
	}

	// Add message key if present
	if len(msg.Key) > 0 {
		dataMsg.Metadata["key"] = string(msg.Key)
	}

	// Add bearer token if configured
	if p.config.BearerToken != "" {
		dataMsg.Metadata["bearer_token"] = p.config.BearerToken
	}

	// Add message headers
	for _, header := range msg.Headers {
		dataMsg.Metadata[fmt.Sprintf("header_%s", string(header.Key))] = string(header.Value)
	}

	// Send to output channel
	select {
	case p.output <- dataMsg:
		// Mark message as processed
		session.MarkMessage(msg, "")
	case <-p.ctx.Done():
		return
	default:
		log.Warnf("Output channel full, dropping Kafka message from topic %s", msg.Topic)
		p.metrics.MessagesDropped++
	}
}
