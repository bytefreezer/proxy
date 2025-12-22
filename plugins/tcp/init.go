// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package tcp

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the TCP plugin with the global registry
func init() {
	plugins.Register("tcp", NewPlugin)
}
