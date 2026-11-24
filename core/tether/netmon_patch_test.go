package tether

import (
	"net"
	"net/netip"
	"testing"

	"tailscale.com/net/netmon"
)

func state(ips ...string) *netmon.State {
	st := &netmon.State{
		InterfaceIPs:          map[string][]netip.Prefix{},
		Interface:             map[string]netmon.Interface{},
		HaveV4:                true,
		HaveV6:                true,
		DefaultRouteInterface: "pdp_ip0",
	}
	st.Interface["pdp_ip0"] = netmon.Interface{Interface: &net.Interface{Name: "pdp_ip0", Flags: net.FlagUp}}
	for _, ip := range ips {
		st.InterfaceIPs["pdp_ip0"] = append(st.InterfaceIPs["pdp_ip0"], netip.MustParsePrefix(ip))
	}
	return st
}

// The patched netmon must not call a new temporary IPv6 address a major
// change, but must still call a lost address one.
func TestAddedIPv6IsMinor(t *testing.T) {
	tuneForMobile()
	defer func() { netmon.IgnoreAddedIPs = false }()

	old := state("10.20.30.40/32", "2001:db8:1:2:41f8:4a2e:cba8:856b/64")
	added := state("10.20.30.40/32", "2001:db8:1:2:41f8:4a2e:cba8:856b/64", "2001:db8:1:2:90b1:93ee:1a01:4645/64")
	cd, err := netmon.NewChangeDelta(old, added, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if cd.RebindLikelyRequired {
		t.Fatalf("added IPv6 address counted as major: %s", cd.StateDesc())
	}

	lost := state("2001:db8:1:2:90b1:93ee:1a01:4645/64")
	cd, err = netmon.NewChangeDelta(added, lost, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if !cd.RebindLikelyRequired {
		t.Fatal("lost addresses not counted as major")
	}

	// Unpatched behaviour stays the default for everyone else.
	netmon.IgnoreAddedIPs = false
	cd, _ = netmon.NewChangeDelta(old, added, 0, true)
	if !cd.RebindLikelyRequired {
		t.Fatal("stock behaviour changed")
	}
}
