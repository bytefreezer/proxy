package sflow

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the sFlow plugin with the global registry
func init() {
	plugins.Register("sflow", NewPlugin)
}
