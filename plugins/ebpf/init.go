package ebpf

import (
	"github.com/bytefreezer/proxy/plugins"
)

// Register the eBPF plugin with the global registry
func init() {
	plugins.Register("ebpf", NewPlugin)
}
