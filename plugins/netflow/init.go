// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package netflow

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the NetFlow plugin with the global registry
func init() {
	plugins.Register("netflow", NewPlugin)
}
