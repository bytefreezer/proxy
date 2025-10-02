package sflow

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the sFlow plugin with the global registry
func init() {
	plugins.Register("sflow", NewPlugin)
}
