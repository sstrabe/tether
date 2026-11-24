//go:build ios || darwin

package tether

import (
	"os"

	"github.com/tailscale/wireguard-go/tun"
	"tailscale.com/net/netmon"
)

// tunFromFD wraps the utun descriptor the packet tunnel provider owns.
func tunFromFD(fd int) (tun.Device, error) {
	f := os.NewFile(uintptr(fd), "utun")
	return tun.CreateTUNFromFile(f, 1280)
}

// setDefaultInterface tells netmon which interface NWPathMonitor sees as
// the way out; sockets are bound to it (IP_BOUND_IF) so they don't loop
// back into the tunnel.
func setDefaultInterface(name string) {
	netmon.UpdateLastKnownDefaultRouteInterface(name)
}
