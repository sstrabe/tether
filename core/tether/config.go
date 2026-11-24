// Package tether runs a Tailscale node inside an iOS packet tunnel extension
// (or, for tests, in userspace on any OS). It is the whole engine: Swift only
// hands it a TUN file descriptor, applies the network settings it asks for,
// and forwards network path changes.
//
// What differs from the official client, on purpose:
//   - Path changes are coalesced: a change only reaches the engine when the
//     default interface or the usable address families change, not on every
//     NWPathMonitor callback (IPv6 privacy address rotation on cellular fires
//     those constantly, and each one made the official client rebind and
//     drop its DERP connection).
//   - Network settings are only handed to iOS when they differ from what was
//     last applied: setTunnelNetworkSettings itself fires a path change, which
//     must not loop back into a rebind.
//   - No log upload (logtail) and a hard memory limit for the extension.
package tether

import (
	"errors"
	"net/url"
	"strings"
)

// Config is what the host passes to Start, as JSON.
type Config struct {
	// ControlURL is the coordination server. Never compiled in: the app
	// stores it in its settings.
	ControlURL string `json:"controlURL"`
	// Hostname is the node's name in the tailnet; empty means the OS name.
	Hostname string `json:"hostname,omitempty"`
	// StateDir holds the node key and prefs (tailscaled.state), the derp map
	// cache and the log file. In the app group container on iOS.
	StateDir string `json:"stateDir"`
	// TunFD is the utun file descriptor from the packet tunnel provider.
	// -1 runs userspace networking only (simulator, Linux harness).
	TunFD int `json:"tunFD"`
	// MemoryLimitMB is a soft limit for the Go heap (GOMEMLIMIT). The iOS
	// extension is killed when it passes its cap, so stay well below it.
	MemoryLimitMB int `json:"memoryLimitMB,omitempty"`
	// AcceptRoutes uses subnet routes advertised by other nodes.
	AcceptRoutes bool `json:"acceptRoutes"`
	// AcceptDNS uses the tailnet's DNS settings (MagicDNS, split DNS).
	AcceptDNS bool `json:"acceptDNS"`
	// ExitNode is the stable node ID or IP of the exit node; empty for none.
	ExitNode string `json:"exitNode,omitempty"`
}

func (c *Config) validate() error {
	if c.ControlURL == "" {
		return errors.New("controlURL is required")
	}
	u, err := url.Parse(c.ControlURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("controlURL must be an https URL")
	}
	c.ControlURL = strings.TrimRight(c.ControlURL, "/")
	if c.StateDir == "" {
		return errors.New("stateDir is required")
	}
	if c.MemoryLimitMB == 0 {
		c.MemoryLimitMB = 30
	}
	return nil
}
