package syslog

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the Syslog plugin with the global registry
func init() {
	plugins.Register("syslog", NewPlugin)
}
