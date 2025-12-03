// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package http

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.GetRegistry().Register("http", NewPlugin)
}
