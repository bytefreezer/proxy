// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package nats

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the NATS plugin with the global registry
func init() {
	plugins.Register("nats", NewPlugin)
}
