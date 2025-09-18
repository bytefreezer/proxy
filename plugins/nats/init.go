package nats

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the NATS plugin with the global registry
func init() {
	plugins.Register("nats", NewPlugin)
}
