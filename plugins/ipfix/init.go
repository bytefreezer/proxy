package ipfix

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the IPFIX plugin with the global registry
func init() {
	plugins.Register("ipfix", NewPlugin)
}
