package kafka

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the Kafka plugin with the global registry
func init() {
	plugins.Register("kafka", NewPlugin)
}
