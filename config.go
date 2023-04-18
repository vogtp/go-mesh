package mesh

import (
	"time"

	"golang.org/x/exp/slog"
)

// Setting is use for optional mesh settings
type Setting func(m *Mgr)

// SLog sets a slog logger
func SLog(log *slog.Logger) Setting {
	return func(m *Mgr) {
		m.slog = log.With("component", "mesh")
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

// Purge defines the intervall after which peers are deleted
func Purge(d time.Duration) Setting {
	return func(m *Mgr) {
		m.purgeIntervall = d
	}
}
