//go:build !ios && !darwin

package tether

import (
	"errors"

	"github.com/tailscale/wireguard-go/tun"
)

func tunFromFD(fd int) (tun.Device, error) {
	return nil, errors.New("TUN from a file descriptor is only supported on Apple platforms here; use tunFD -1")
}

func setDefaultInterface(string) {}
