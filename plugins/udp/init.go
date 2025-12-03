// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package udp

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the UDP plugin with the global registry
func init() {
	plugins.Register("udp", NewPlugin)
}
