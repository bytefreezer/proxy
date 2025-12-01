package kinesis

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/mitchellh/mapstructure"

	"github.com/bytefreezer/proxy/plugins"
	"github.com/bytefreezer/goodies/log"
)

// Plugin implements the Kinesis input plugin with direct filesystem writes
type Plugin struct {
	config     Config
	client     *kinesis.Client
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	health     plugins.PluginHealth
	mu         sync.RWMutex
	spooler    plugins.SpoolingInterface
	metrics    PluginMetrics
	textProcessor *plugins.TextProcessor
	shardIters map[string]string // track shard iterators
}

// Config represents Kinesis plugin configuration
type Config struct {
	StreamName      string `mapstructure:"stream_name"`
	Region          string `mapstructure:"region"`
	TenantID        string `mapstructure:"tenant_id"`
	DatasetID       string `mapstructure:"dataset_id"`
	BearerToken     string `mapstructure:"bearer_token,omitempty"`
	DataHint        string `mapstructure:"data_hint,omitempty"` // Data format hint for downstream processing (defaults to "ndjson")
	DataFormat        string   `mapstructure:"data_format,omitempty"`           // data format mode: "ndjson", "text", "auto" (default)
	PollInterval    int    `mapstructure:"poll_interval_seconds,omitempty"` // Polling interval in seconds (default: 5)
	MaxRecords      int    `mapstructure:"max_records,omitempty"`      // Max records per GetRecords call (default: 100)
	ShardIteratorType string `mapstructure:"shard_iterator_type,omitempty"` // LATEST, TRIM_HORIZON, etc.
}

// PluginMetrics tracks Kinesis plugin metrics
type PluginMetrics struct {
	RecordsReceived uint64
	BytesReceived   uint64
	RecordsDropped  uint64
	LastRecordTime  time.Time
	ShardsActive    int
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "kinesis"
}

// Configure initializes the plugin with config
func (p *Plugin) Configure(configData map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decode configuration
	if err := mapstructure.Decode(configData, &p.config); err != nil {
		return fmt.Errorf("failed to decode Kinesis config: %w", err)
	}

	// Set defaults
	if p.config.DataHint == "" {
		p.config.DataHint = "ndjson"
	}
	if p.config.PollInterval == 0 {
		p.config.PollInterval = 5
	}
	if p.config.MaxRecords == 0 {
		p.config.MaxRecords = 100
	}
	if p.config.ShardIteratorType == "" {
		p.config.ShardIteratorType = "LATEST"
	}

	// Validate required fields
	if p.config.StreamName == "" {
		return fmt.Errorf("stream_name is required")
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

	// Initialize AWS config
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(p.config.Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Kinesis client
	p.client = kinesis.NewFromConfig(cfg)
	p.shardIters = make(map[string]string)

	p.health = plugins.PluginHealth{
		Status:      plugins.HealthStatusHealthy,
		Message:     fmt.Sprintf("Kinesis plugin configured: %s -> %s/%s", p.config.StreamName, p.config.TenantID, p.config.DatasetID),
		LastUpdated: time.Now(),
	}

	log.Infof("Kinesis plugin configured: %s -> %s/%s (%s)", p.config.StreamName, p.config.TenantID, p.config.DatasetID, p.config.DataHint)
	if p.config.DataFormat == "" {
		p.config.DataFormat = plugins.DataFormatAuto // default to auto-detect
	}
	return nil
}

// Start begins consuming data from Kinesis with direct filesystem writes
func (p *Plugin) Start(ctx context.Context, spooler plugins.SpoolingInterface) error {
	p.mu.Lock()
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.spooler = spooler
	p.health.Status = plugins.HealthStatusStarting
	p.health.LastUpdated = time.Now()
	p.mu.Unlock()

	// Get stream shards
	shards, err := p.getShards()
	if err != nil {
		p.updateHealth(plugins.HealthStatusUnhealthy, fmt.Sprintf("Failed to get shards: %v", err))
		return fmt.Errorf("failed to get shards: %w", err)
	}

	log.Infof("Kinesis plugin started with direct spooling for stream %s with %d shards", p.config.StreamName, len(shards))

	// Start shard readers
	for _, shard := range shards {
		p.wg.Add(1)
		go p.readShard(&shard)
	}

	p.updateHealth(plugins.HealthStatusHealthy, fmt.Sprintf("Processing %d shards with direct spooling", len(shards)))
	p.metrics.ShardsActive = len(shards)

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

	log.Infof("Kinesis plugin stopped")
	return nil
}

// Health returns plugin health status
func (p *Plugin) Health() plugins.PluginHealth {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

// getShards retrieves all shards for the stream
func (p *Plugin) getShards() ([]types.Shard, error) {
	resp, err := p.client.DescribeStream(p.ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(p.config.StreamName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe stream: %w", err)
	}

	return resp.StreamDescription.Shards, nil
}

// readShard reads records from a single shard
func (p *Plugin) readShard(shard *types.Shard) {
	defer p.wg.Done()

	shardID := *shard.ShardId
	log.Debugf("Starting Kinesis shard reader for %s", shardID)

	// Get initial shard iterator
	iterResp, err := p.client.GetShardIterator(p.ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String(p.config.StreamName),
		ShardId:           aws.String(shardID),
		ShardIteratorType: types.ShardIteratorType(p.config.ShardIteratorType),
	})
	if err != nil {
		log.Errorf("Failed to get shard iterator for %s: %v", shardID, err)
		return
	}

	shardIterator := iterResp.ShardIterator
	ticker := time.NewTicker(time.Duration(p.config.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			log.Debugf("Shard reader for %s stopping", shardID)
			return
		case <-ticker.C:
			if shardIterator == nil {
				log.Debugf("No more records in shard %s", shardID)
				return
			}

			records, nextIterator, err := p.getRecords(shardIterator)
			if err != nil {
				log.Errorf("Failed to get records from shard %s: %v", shardID, err)
				p.updateHealth(plugins.HealthStatusUnhealthy, fmt.Sprintf("Shard %s error: %v", shardID, err))
				continue
			}

			shardIterator = nextIterator

			// Process records
			for _, record := range records {
				p.processRecord(record, shardID)
			}
		}
	}
}

// getRecords retrieves records from Kinesis
func (p *Plugin) getRecords(shardIterator *string) ([]types.Record, *string, error) {
	// Safely convert int to int32 with bounds checking
	var limit int32
	if p.config.MaxRecords > 0 && p.config.MaxRecords <= 10000 {
		limit = int32(p.config.MaxRecords) // #nosec G115 -- bounds checked above
	} else {
		limit = 100 // Safe default
	}

	resp, err := p.client.GetRecords(p.ctx, &kinesis.GetRecordsInput{
		ShardIterator: shardIterator,
		Limit:         aws.Int32(limit),
	})
	if err != nil {
		return nil, nil, err
	}

	return resp.Records, resp.NextShardIterator, nil
}

// processRecord processes a single Kinesis record
func (p *Plugin) processRecord(record types.Record, shardID string) {
	// Process through text processor (line-by-line detection and wrapping)
	formattedData, linesWrapped := p.processRecordData(record.Data)
	if linesWrapped > 0 {
		log.Debugf("Kinesis: Wrapped %d text lines from shard %s (data_format: %s)",
			linesWrapped, shardID, p.config.DataFormat)
	}

	// Update metrics
	p.mu.Lock()
	p.metrics.RecordsReceived++
	p.metrics.BytesReceived += uint64(len(record.Data))
	p.metrics.LastRecordTime = time.Now()
	p.mu.Unlock()

	// Write directly to filesystem - NO CHANNEL DROPS POSSIBLE
	bearerToken := p.config.BearerToken
	dataHint := p.config.DataHint
	if dataHint == "" {
		dataHint = "raw" // default for Kinesis
	}
	if err := p.spooler.StoreRawMessage(p.config.TenantID, p.config.DatasetID, bearerToken, formattedData, dataHint); err != nil {
		log.Errorf("Failed to store Kinesis record to filesystem from shard %s: %v", shardID, err)
		p.mu.Lock()
		p.metrics.RecordsDropped++
		p.mu.Unlock()
		return
	}

	log.Debugf("Stored Kinesis record from shard %s directly to filesystem: %d bytes", shardID, len(record.Data))
}

// processRecordData processes Kinesis record data line-by-line
// Returns processed data and count of wrapped lines
func (p *Plugin) processRecordData(data []byte) ([]byte, int) {
	// Kinesis records may contain multiple lines - split and process each
	lines := bytes.Split(data, []byte("\n"))

	var result []byte
	linesWrapped := 0

	for _, line := range lines {
		// Skip empty lines
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		// Process line through text processor
		processed, wrapped, err := p.textProcessor.ProcessLine(line, p.config.TenantID, p.config.DatasetID, p.config.DataFormat)
		if err != nil {
			log.Warnf("Failed to process line for %s/%s: %v", p.config.TenantID, p.config.DatasetID, err)
			continue
		}

		if wrapped {
			linesWrapped++
		}

		// Append processed line (already has \n if wrapped, add if not)
		result = append(result, processed...)
		if !wrapped && len(processed) > 0 && processed[len(processed)-1] != '\n' {
			result = append(result, '\n')
		}
	}

	return result, linesWrapped
}

// updateHealth updates plugin health status
func (p *Plugin) updateHealth(status plugins.HealthStatus, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.health.Status = status
	p.health.Message = message
	p.health.LastUpdated = time.Now()
}

// Schema returns the Kinesis plugin configuration schema
func (p *Plugin) Schema() plugins.PluginSchema {
	return plugins.PluginSchema{
		Name:        "kinesis",
		DisplayName: "AWS Kinesis Data Streams",
		Description: "AWS Kinesis Data Streams consumer. Consumes records from Kinesis streams and outputs structured NDJSON.",
		Category:    "Message Queue",
		Transport:   "AWS API",
		Fields: []plugins.PluginFieldSchema{
			{
				Name:        "stream_name",
				Type:        "string",
				Required:    true,
				Description: "Name of the Kinesis stream",
				Placeholder: "my-stream",
				Group:       "Connection",
			},
			{
				Name:        "region",
				Type:        "string",
				Required:    true,
				Description: "AWS region (e.g., us-east-1, us-west-2)",
				Placeholder: "us-east-1",
				Group:       "Connection",
			},
			{
				Name:        "shard_iterator_type",
				Type:        "string",
				Required:    false,
				Default:     "LATEST",
				Description: "Starting position in the stream",
				Options:     []string{"LATEST", "TRIM_HORIZON", "AT_TIMESTAMP"},
				Placeholder: "LATEST",
				Group:       "Consumption",
			},
			{
				Name:        "poll_interval_seconds",
				Type:        "int",
				Required:    false,
				Default:     5,
				Description: "Polling interval in seconds",
				Validation:  "min:1,max:60",
				Placeholder: "5",
				Group:       "Performance",
			},
			{
				Name:        "max_records",
				Type:        "int",
				Required:    false,
				Default:     100,
				Description: "Maximum records per GetRecords call",
				Validation:  "min:1,max:10000",
				Placeholder: "100",
				Group:       "Performance",
			},
		},
	}
}

// Factory function for creating Kinesis plugin instances
func NewKinesisPlugin() plugins.InputPlugin {
	return &Plugin{
		textProcessor: plugins.NewTextProcessor(),
		health: plugins.PluginHealth{
			Status:      plugins.HealthStatusStopped,
			Message:     "Plugin created but not started",
			LastUpdated: time.Now(),
		},
		shardIters: make(map[string]string),
	}
}