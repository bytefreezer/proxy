package udp

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the UDP plugin with the global registry
func init() {
	plugins.Register("udp", NewPlugin)
}
