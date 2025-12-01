package nats

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the NATS plugin with the global registry
func init() {
	plugins.Register("nats", NewPlugin)
}
