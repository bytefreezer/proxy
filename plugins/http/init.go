package http

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.GetRegistry().Register("http", NewPlugin)
}
