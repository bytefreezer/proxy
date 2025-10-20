package ebpf

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

// Register the eBPF plugin with the global registry
func init() {
	plugins.Register("ebpf", NewPlugin)
}
