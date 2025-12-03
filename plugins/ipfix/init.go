// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package ipfix

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the IPFIX plugin with the global registry
func init() {
	plugins.Register("ipfix", NewPlugin)
}
