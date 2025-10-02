package netflow

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the NetFlow plugin with the global registry
func init() {
	plugins.Register("netflow", NewPlugin)
}
