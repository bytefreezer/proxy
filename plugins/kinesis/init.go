// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package kinesis

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.Register("kinesis", NewKinesisPlugin)
}
