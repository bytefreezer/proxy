package http

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

func init() {
	plugins.GetRegistry().Register("http", NewPlugin)
}