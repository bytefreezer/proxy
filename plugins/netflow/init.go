package netflow

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the NetFlow plugin with the global registry
func init() {
	plugins.Register("netflow", NewPlugin)
}
