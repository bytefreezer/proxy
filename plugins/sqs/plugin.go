package sqs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/mitchellh/mapstructure"

	"github.com/n0needt0/bytefreezer-proxy/plugins"
	"github.com/n0needt0/go-goodies/log"
)

// Plugin implements the SQS input plugin
type Plugin struct {
	config   Config
	client   *sqs.Client
	queueURL string
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	health   plugins.PluginHealth
	mu       sync.RWMutex
	output   chan<- *plugins.DataMessage
	metrics  PluginMetrics
}

// Config represents SQS plugin configuration
type Config struct {
	QueueName            string `mapstructure:"queue_name"`
	QueueURL             string `mapstructure:"queue_url,omitempty"` // Optional direct URL
	Region               string `mapstructure:"region"`
	TenantID             string `mapstructure:"tenant_id"`
	DatasetID            string `mapstructure:"dataset_id"`
	BearerToken          string `mapstructure:"bearer_token,omitempty"`
	DataHint             string `mapstructure:"data_hint,omitempty"`             // Data format hint for downstream processing (defaults to "ndjson")
	PollInterval         int    `mapstructure:"poll_interval_seconds,omitempty"` // Polling interval in seconds (default: 5)
	MaxMessages          int    `mapstructure:"max_messages,omitempty"`          // Max messages per receive call (default: 10, max: 10)
	VisibilityTimeout    int    `mapstructure:"visibility_timeout_seconds,omitempty"` // Visibility timeout (default: 30)
	WaitTimeSeconds      int    `mapstructure:"wait_time_seconds,omitempty"`     // Long polling wait time (default: 20, max: 20)
	DeleteAfterProcess   bool   `mapstructure:"delete_after_process"`            // Delete messages after processing (default: true)
	WorkerCount          int    `mapstructure:"worker_count,omitempty"`          // Number of concurrent workers (default: 3)
}

// PluginMetrics tracks SQS plugin metrics
type PluginMetrics struct {
	MessagesReceived uint64
	BytesReceived    uint64
	MessagesDropped  uint64
	MessagesDeleted  uint64
	LastMessageTime  time.Time
	WorkersActive    int
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "sqs"
}

// Configure initializes the plugin with config
func (p *Plugin) Configure(configData map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(configData, &p.config); err != nil {
		return fmt.Errorf("failed to decode SQS config: %w", err)
	}

	// Set defaults
	if p.config.DataHint == "" {
		p.config.DataHint = "ndjson"
	}
	if p.config.PollInterval == 0 {
		p.config.PollInterval = 5
	}
	if p.config.MaxMessages == 0 {
		p.config.MaxMessages = 10
	}
	if p.config.VisibilityTimeout == 0 {
		p.config.VisibilityTimeout = 30
	}
	if p.config.WaitTimeSeconds == 0 {
		p.config.WaitTimeSeconds = 20
	}
	if p.config.WorkerCount == 0 {
		p.config.WorkerCount = 3
	}
	// Default to deleting messages after processing
	if !p.config.DeleteAfterProcess {
		p.config.DeleteAfterProcess = true
	}

	// Validate required fields
	if p.config.QueueName == "" && p.config.QueueURL == "" {
		return fmt.Errorf("either queue_name or queue_url is required")
	}
	if p.config.Region == "" {
		return fmt.Errorf("region is required")
	}
	if p.config.TenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}
	if p.config.DatasetID == "" {
		return fmt.Errorf("dataset_id is required")
	}

	// Validate limits
	if p.config.MaxMessages > 10 {
		p.config.MaxMessages = 10
		log.Warnf("SQS max_messages limited to 10 (AWS limit)")
	}
	if p.config.WaitTimeSeconds > 20 {
		p.config.WaitTimeSeconds = 20
		log.Warnf("SQS wait_time_seconds limited to 20 (AWS limit)")
	}

	// Initialize AWS config
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(p.config.Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create SQS client
	p.client = sqs.NewFromConfig(cfg)

	// Get queue URL if not provided
	if p.config.QueueURL != "" {
		p.queueURL = p.config.QueueURL
	} else {
		urlResp, err := p.client.GetQueueUrl(context.TODO(), &sqs.GetQueueUrlInput{
			QueueName: aws.String(p.config.QueueName),
		})
		if err != nil {
			return fmt.Errorf("failed to get queue URL for %s: %w", p.config.QueueName, err)
		}
		p.queueURL = *urlResp.QueueUrl
	}

	p.health = plugins.PluginHealth{
		Status:      plugins.HealthStatusHealthy,
		Message:     fmt.Sprintf("SQS plugin configured: %s -> %s/%s", p.config.QueueName, p.config.TenantID, p.config.DatasetID),
		LastUpdated: time.Now(),
	}

	log.Infof("SQS plugin configured: %s -> %s/%s (%s)", p.config.QueueName, p.config.TenantID, p.config.DatasetID, p.config.DataHint)
	return nil
}

// Start begins consuming messages from SQS
func (p *Plugin) Start(ctx context.Context, output chan<- *plugins.DataMessage) error {
	p.mu.Lock()
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.output = output
	p.health.Status = plugins.HealthStatusStarting
	p.health.LastUpdated = time.Now()
	p.mu.Unlock()

	log.Infof("SQS plugin started with %d workers for queue %s", p.config.WorkerCount, p.config.QueueName)

	// Start message processing workers
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.messageWorker(i)
	}

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("Processing messages with %d workers", p.config.WorkerCount))
	p.metrics.WorkersActive = p.config.WorkerCount

	return nil
}

// Stop gracefully shuts down the plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancel != nil {
		p.health.Status = plugins.HealthStatusStopping
		p.health.LastUpdated = time.Now()
		p.cancel()
	}

	p.wg.Wait()
	p.health.Status = plugins.HealthStatusStopped
	p.health.LastUpdated = time.Now()

	log.Infof("SQS plugin stopped")
	return nil
}

// Health returns plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// messageWorker processes messages from SQS
func (p *Plugin) messageWorker(workerID int) {
	defer p.wg.Done()

	log.Debugf("SQS worker %d started", workerID)
	ticker := time.NewTicker(time.Duration(p.config.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			log.Debugf("SQS worker %d stopping", workerID)
			return
		case <-ticker.C:
			messages, err := p.receiveMessages()
			if err != nil {
				log.Errorf("SQS worker %d failed to receive messages: %v", workerID, err)
				p.updateHealth(plugins.HealthStatusUnhealthy, fmt.Sprintf("Worker %d error: %v", workerID, err))
				continue
			}

			// Process each message
			for _, message := range messages {
				if p.ctx.Err() != nil {
					return
				}
				p.processMessage(message, workerID)
			}
		}
	}
}

// receiveMessages receives messages from SQS
func (p *Plugin) receiveMessages() ([]types.Message, error) {
	// Safely convert int to int32 with bounds checking
	var maxMessages int32
	if p.config.MaxMessages > 0 && p.config.MaxMessages <= 10 {
		maxMessages = int32(p.config.MaxMessages) // #nosec G115 -- bounds checked above
	} else {
		maxMessages = 10 // Safe default (AWS max)
	}

	var visibilityTimeout int32
	if p.config.VisibilityTimeout > 0 && p.config.VisibilityTimeout <= 43200 { // AWS max 12 hours
		visibilityTimeout = int32(p.config.VisibilityTimeout) // #nosec G115 -- bounds checked above
	} else {
		visibilityTimeout = 30 // Safe default
	}

	var waitTimeSeconds int32
	if p.config.WaitTimeSeconds >= 0 && p.config.WaitTimeSeconds <= 20 { // AWS max 20 seconds
		waitTimeSeconds = int32(p.config.WaitTimeSeconds) // #nosec G115 -- bounds checked above
	} else {
		waitTimeSeconds = 20 // Safe default
	}

	resp, err := p.client.ReceiveMessage(p.ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(p.queueURL),
		MaxNumberOfMessages:   maxMessages,
		VisibilityTimeout:     visibilityTimeout,
		WaitTimeSeconds:       waitTimeSeconds,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		return nil, err
	}

	return resp.Messages, nil
}

// processMessage processes a single SQS message
func (p *Plugin) processMessage(message types.Message, workerID int) {
	if message.Body == nil {
		log.Warnf("SQS worker %d received message with no body", workerID)
		return
	}

	// Extract metadata from message attributes
	metadata := map[string]string{
		"sqs_queue":      p.config.QueueName,
		"sqs_message_id": *message.MessageId,
		"sqs_worker_id":  fmt.Sprintf("%d", workerID),
	}

	// Add message attributes to metadata
	for name, attr := range message.MessageAttributes {
		if attr.StringValue != nil {
			metadata[fmt.Sprintf("sqs_attr_%s", name)] = *attr.StringValue
		}
	}

	// Format data according to data hint
	body := []byte(*message.Body)
	formattedData := body
	if p.config.DataHint != "" {
		var err error
		formatter := plugins.GetFormatter(p.config.DataHint)
		formattedData, err = formatter.Format(body)
		if err != nil {
			log.Warnf("Data formatting failed for SQS message %s (format: %s): %v", *message.MessageId, p.config.DataHint, err)
			// Continue with original data if formatting fails
			formattedData = body
		}
	}

	msg := &plugins.DataMessage{
		Data:          formattedData,
		TenantID:      p.config.TenantID,
		DatasetID:     p.config.DatasetID,
		DataHint:      p.config.DataHint,
		Timestamp:     time.Now(), // SQS doesn't provide original timestamp
		Metadata:      metadata,
	}

	// Update metrics
	p.mu.Lock()
	p.metrics.MessagesReceived++
	p.metrics.BytesReceived += uint64(len(*message.Body))
	p.metrics.LastMessageTime = time.Now()
	p.mu.Unlock()

	// Send to output channel
	select {
	case p.output <- msg:
		log.Debugf("SQS worker %d processed message: %d bytes", workerID, len(*message.Body))

		// Delete message if configured to do so
		if p.config.DeleteAfterProcess {
			if err := p.deleteMessage(message); err != nil {
				log.Errorf("SQS worker %d failed to delete message %s: %v", workerID, *message.MessageId, err)
			} else {
				p.mu.Lock()
				p.metrics.MessagesDeleted++
				p.mu.Unlock()
			}
		}
	case <-p.ctx.Done():
		return
	default:
		log.Warnf("SQS worker %d output channel full, dropping message", workerID)
		p.mu.Lock()
		p.metrics.MessagesDropped++
		p.mu.Unlock()
	}
}

// deleteMessage deletes a message from SQS
func (p *Plugin) deleteMessage(message types.Message) error {
	_, err := p.client.DeleteMessage(p.ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(p.queueURL),
		ReceiptHandle: message.ReceiptHandle,
	})
	return err
}

// updateHealth updates plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.health.Status = status
	p.health.Message = message
	p.health.LastUpdated = time.Now()
}

// Factory function for creating SQS plugin instances
func NewSQSPlugin() plugins.InputPlugin {
	return &Plugin{}
}