# AWS Plugins for ByteFreezer Proxy

ByteFreezer Proxy now includes native AWS integration with Kinesis Data Streams and SQS plugins, enabling seamless data ingestion from AWS services.

## Plugin Overview

### Kinesis Plugin
- **Type**: `kinesis`
- **Purpose**: Consume data from Amazon Kinesis Data Streams
- **Use Cases**: Real-time streaming data, IoT events, log aggregation
- **Output Format**: Configurable (ndjson, json, raw, csv)

### SQS Plugin
- **Type**: `sqs`
- **Purpose**: Process messages from Amazon SQS queues
- **Use Cases**: Event-driven processing, decoupling, batch jobs
- **Output Format**: Configurable (ndjson, json, raw, csv)

## Configuration

### Kinesis Plugin Configuration

```yaml
inputs:
  - type: "kinesis"
    name: "my-kinesis-consumer"
    config:
      stream_name: "bytefreezer-stream"        # Required: Kinesis stream name
      region: "us-west-2"                      # Required: AWS region
      tenant_id: "customer-1"                  # Required: ByteFreezer tenant
      dataset_id: "streaming-data"             # Required: ByteFreezer dataset
      data_hint: "ndjson"                      # Optional: Data format hint (default: ndjson)
      poll_interval_seconds: 5                # Optional: Polling frequency (default: 5)
      max_records: 100                        # Optional: Records per API call (default: 100)
      shard_iterator_type: "LATEST"           # Optional: LATEST, TRIM_HORIZON, etc. (default: LATEST)
```

### SQS Plugin Configuration

```yaml
inputs:
  - type: "sqs"
    name: "my-sqs-processor"
    config:
      queue_name: "bytefreezer-queue"          # Required: SQS queue name
      # queue_url: "https://sqs.region.amazonaws.com/account/queue"  # Optional: Direct URL
      region: "us-west-2"                      # Required: AWS region
      tenant_id: "customer-1"                  # Required: ByteFreezer tenant
      dataset_id: "queue-messages"             # Required: ByteFreezer dataset
      data_hint: "ndjson"                      # Optional: Data format hint (default: ndjson)
      poll_interval_seconds: 3                # Optional: Polling frequency (default: 5)
      max_messages: 10                        # Optional: Messages per receive (max: 10, default: 10)
      visibility_timeout_seconds: 30          # Optional: Visibility timeout (default: 30)
      wait_time_seconds: 20                   # Optional: Long polling wait (max: 20, default: 20)
      delete_after_process: true              # Optional: Delete after processing (default: true)
      worker_count: 3                         # Optional: Concurrent workers (default: 3)
```

## AWS Credentials

Both plugins use the AWS SDK default credential chain:

1. **Environment Variables**: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`
2. **AWS Credentials File**: `~/.aws/credentials`
3. **AWS Config File**: `~/.aws/config`
4. **IAM Roles**: EC2 instance roles, ECS task roles
5. **SSO**: AWS IAM Identity Center

## Required IAM Permissions

### Kinesis Plugin Permissions

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "kinesis:DescribeStream",
        "kinesis:GetShardIterator",
        "kinesis:GetRecords",
        "kinesis:ListStreams"
      ],
      "Resource": "arn:aws:kinesis:*:*:stream/bytefreezer-*"
    }
  ]
}
```

### SQS Plugin Permissions

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueUrl",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:*:*:bytefreezer-*"
    }
  ]
}
```

## Complete Configuration Example

```yaml
app:
  name: bytefreezer-proxy
  version: 0.0.2

logging:
  level: info
  encoding: console

server:
  api_port: 8088

inputs:
  # Kinesis streaming data
  - type: "kinesis"
    name: "kinesis-logs-consumer"
    config:
      stream_name: "application-logs"
      region: "us-west-2"
      tenant_id: "production"
      dataset_id: "app-logs"
      data_hint: "ndjson"
      poll_interval_seconds: 5
      max_records: 100
      shard_iterator_type: "LATEST"

  # SQS batch processing
  - type: "sqs"
    name: "sqs-events-processor"
    config:
      queue_name: "event-processing-queue"
      region: "us-west-2"
      tenant_id: "production"
      dataset_id: "events"
      data_hint: "ndjson"
      poll_interval_seconds: 3
      max_messages: 10
      visibility_timeout_seconds: 30
      wait_time_seconds: 20
      delete_after_process: true
      worker_count: 5

# Global configuration
tenant_id: "default-tenant"
bearer_token: "${BYTEFREEZER_TOKEN}"

receiver:
  base_url: "https://receiver.example.com/{tenantid}/{datasetid}"
  timeout_seconds: 30
  upload_worker_count: 5

spooling:
  enabled: true
  directory: "/var/spool/bytefreezer-proxy"
  max_size_bytes: 5368709120  # 5GB
  retry_attempts: 5
  retry_interval_seconds: 60
```

## Data Flow Architecture

### Kinesis Plugin Flow
```
Kinesis Stream → Shard Iterator → GetRecords → ByteFreezer Message → Queue → Receiver
                      ↓
              Multiple Shards Processed Concurrently
```

### SQS Plugin Flow
```
SQS Queue → ReceiveMessage → ByteFreezer Message → Queue → Receiver → DeleteMessage (if successful)
                ↓
      Multiple Workers Processing Concurrently
```

## Monitoring and Health

Both plugins provide comprehensive health monitoring through the `/api/v2/health` endpoint:

```json
{
  "status": "ok",
  "plugins": {
    "kinesis": "healthy",
    "sqs": "healthy"
  },
  "spooling": {
    "enabled": true,
    "queue_files": 0,
    "retry_files": 0
  }
}
```

## Plugin Metrics

### Kinesis Metrics
- Records received count
- Bytes processed
- Records dropped
- Active shards count
- Last record timestamp

### SQS Metrics
- Messages received count
- Bytes processed
- Messages dropped
- Messages deleted
- Active workers count
- Last message timestamp

## Error Handling

### Spool-First Architecture
Both plugins follow ByteFreezer's spool-first architecture:

1. **Data Reception**: AWS service → Plugin processing
2. **Immediate Spooling**: Data written to local queue directory
3. **Upload Attempt**: Queue → Receiver upload
4. **Retry Logic**: Failed uploads → retry directory → eventual success or DLQ

### AWS-Specific Error Handling
- **Connection Failures**: Automatic retry with exponential backoff
- **Authentication Errors**: Plugin marked unhealthy, requires credential fix
- **Rate Limiting**: Built-in AWS SDK retry with jitter
- **Resource Not Found**: Stream/queue validation on startup

## Filename Format

Both plugins generate filenames using the new format:
```
{tenant}--{dataset}--{timestamp}--{extension}.gz
```

Examples:
- `production--app-logs--1736938245123456789--ndjson.gz`
- `customer-1--events--1736938245123456789--json.gz`

## Performance Tuning

### Kinesis Plugin
- **Shard Parallelism**: Each shard processed by separate goroutine
- **Batch Size**: Adjust `max_records` (1-10000, default: 100)
- **Polling Frequency**: Balance `poll_interval_seconds` vs latency
- **Memory Usage**: Higher `max_records` = more memory per batch

### SQS Plugin
- **Worker Scaling**: Adjust `worker_count` based on message volume
- **Batch Processing**: Use `max_messages` = 10 for maximum throughput
- **Long Polling**: `wait_time_seconds` = 20 reduces API calls
- **Visibility Timeout**: Match processing time to avoid duplicate processing

## Deployment Examples

### Docker Compose with AWS Plugins

```yaml
version: '3.8'
services:
  bytefreezer-proxy:
    image: bytefreezer-proxy:latest
    environment:
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - AWS_REGION=us-west-2
    volumes:
      - ./config-aws.yaml:/app/config.yaml
      - ./spool:/var/spool/bytefreezer-proxy
    ports:
      - "8088:8088"
```

### Kubernetes Deployment with IAM Roles

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bytefreezer-proxy
spec:
  template:
    spec:
      serviceAccountName: bytefreezer-proxy-sa  # With IAM role annotation
      containers:
      - name: proxy
        image: bytefreezer-proxy:latest
        env:
        - name: AWS_REGION
          value: "us-west-2"
        volumeMounts:
        - name: config
          mountPath: /app/config.yaml
          subPath: config.yaml
```

## Troubleshooting

### Common Issues

1. **Plugin Not Registered**
   ```
   Error: unknown plugin type: kinesis
   ```
   - **Solution**: Ensure AWS-enabled build includes plugin imports

2. **AWS Credentials Not Found**
   ```
   Error: failed to load AWS config: NoCredentialProviders
   ```
   - **Solution**: Configure AWS credentials via environment, files, or IAM roles

3. **Stream/Queue Not Found**
   ```
   Error: ResourceNotFoundException: Stream mystream under account 123 not found
   ```
   - **Solution**: Verify stream/queue exists and region is correct

4. **Permission Denied**
   ```
   Error: AccessDeniedException: User is not authorized to perform kinesis:DescribeStream
   ```
   - **Solution**: Add required IAM permissions

### Plugin Health Debugging

Check plugin status:
```bash
curl http://localhost:8088/api/v2/health | jq '.plugins'
```

Expected healthy response:
```json
{
  "kinesis": "healthy",
  "sqs": "healthy",
  "udp": "healthy"
}
```

## Advanced Configuration

### Multi-Region Setup
```yaml
inputs:
  - type: "kinesis"
    name: "us-west-kinesis"
    config:
      stream_name: "logs-west"
      region: "us-west-2"
      tenant_id: "prod-west"
      dataset_id: "logs"

  - type: "kinesis"
    name: "eu-west-kinesis"
    config:
      stream_name: "logs-eu"
      region: "eu-west-1"
      tenant_id: "prod-eu"
      dataset_id: "logs"
```

### Cross-Account Access
```yaml
inputs:
  - type: "sqs"
    name: "cross-account-queue"
    config:
      queue_url: "https://sqs.us-west-2.amazonaws.com/EXTERNAL-ACCOUNT/shared-queue"
      region: "us-west-2"
      tenant_id: "partner"
      dataset_id: "shared-data"
```

## Migration from Legacy Systems

### From Kinesis Consumer Library (KCL)
- Replace KCL checkpointing with ByteFreezer's spool-first architecture
- Configure `shard_iterator_type: "TRIM_HORIZON"` for historical data
- Adjust `poll_interval_seconds` to match KCL polling frequency

### From SQS Polling Applications
- Replace custom polling loops with SQS plugin configuration
- Set `delete_after_process: false` if you need manual message handling
- Configure `worker_count` to match existing concurrency patterns

## Security Best Practices

1. **Least Privilege**: Grant minimal required IAM permissions
2. **Credential Rotation**: Use IAM roles instead of long-term keys
3. **Network Security**: Use VPC endpoints for AWS service access
4. **Encryption**: Enable encryption at rest for Kinesis streams and SQS queues
5. **Monitoring**: Enable CloudTrail logging for AWS API calls

## Cost Optimization

### Kinesis
- Use `poll_interval_seconds` > 5 to reduce GetRecords API calls
- Optimize `max_records` to balance cost vs latency
- Monitor shard count vs data volume

### SQS
- Use long polling (`wait_time_seconds: 20`) to reduce ReceiveMessage calls
- Set appropriate `visibility_timeout_seconds` to minimize duplicate processing
- Configure `max_messages: 10` for batch processing efficiency

## Support and Documentation

- **Plugin Source Code**: `plugins/kinesis/` and `plugins/sqs/`
- **Configuration Examples**: `config-aws-plugins-example.yaml`
- **Health Monitoring**: `/api/v2/health` endpoint
- **AWS Documentation**: [Kinesis](https://docs.aws.amazon.com/kinesis/) | [SQS](https://docs.aws.amazon.com/sqs/)

Both plugins are production-ready and follow ByteFreezer's proven spool-first architecture for maximum data reliability.