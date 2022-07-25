package mesh

import (
	"time"

	"github.com/vogtp/go-hcl"
)

// Setting is use for optional mesh settings
type Setting func(m *Mgr)

// Hcl sets a HCL logger
func Hcl(hcl hcl.Logger) Setting {
	return func(m *Mgr) {
		m.hcl = hcl.Named("mesh")
	}
}

// ConnectPeers enables/disabled connection estabilshment to peers
func ConnectPeers(b bool) Setting {
	return func(m *Mgr) {
		m.connectToNew = b
	}
}

// BroadcastIntervall defines the intervall broadcasts will be send
func BroadcastIntervall(d time.Duration) Setting {
	return func(m *Mgr) {
		m.checkIntervall = d
	}
}
