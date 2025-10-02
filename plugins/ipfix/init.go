package ipfix

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the IPFIX plugin with the global registry
func init() {
	plugins.Register("ipfix", NewPlugin)
}
