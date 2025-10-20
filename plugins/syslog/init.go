package syslog

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the Syslog plugin with the global registry
func init() {
	plugins.Register("syslog", NewPlugin)
}
