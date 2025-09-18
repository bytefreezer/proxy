package kafka

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the Kafka plugin with the global registry
func init() {
	plugins.Register("kafka", NewPlugin)
}
