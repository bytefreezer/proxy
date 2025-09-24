package kinesis

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

func init() {
	plugins.Register("kinesis", NewKinesisPlugin)
}