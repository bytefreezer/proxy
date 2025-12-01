package kinesis

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.Register("kinesis", NewKinesisPlugin)
}
