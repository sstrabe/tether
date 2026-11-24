package tether

import (
	"net/netip"
	"testing"

	"tailscale.com/net/dns"
	"tailscale.com/util/dnsname"
	"tailscale.com/wgengine/router"
)

func TestSettingsAppliedOnlyWhenChanged(t *testing.T) {
	var applied []NetworkSettings
	m := &settingsMerger{apply: func(s NetworkSettings) error { applied = append(applied, s); return nil }}

	// DNS before any router config: nothing to apply yet.
	if err := m.setDNS(dns.OSConfig{Nameservers: []netip.Addr{netip.MustParseAddr("100.100.100.100")}}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied before router config")
	}
	rc := &router.Config{
		LocalAddrs: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
		Routes:     []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("10.1.0.0/16")},
	}
	if err := m.setRouter(rc); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(applied[0].DNSServers) != 1 {
		t.Fatalf("got %+v", applied)
	}
	// The engine re-sends the same config often (every netmap): no apply.
	same := *rc
	same.Routes = []netip.Prefix{rc.Routes[1], rc.Routes[0]} // order differs
	for i := 0; i < 5; i++ {
		if err := m.setRouter(&same); err != nil {
			t.Fatal(err)
		}
	}
	if len(applied) != 1 {
		t.Fatalf("re-applied identical settings %d times", len(applied)-1)
	}
	// A real change applies.
	if err := m.setDNS(dns.OSConfig{
		Nameservers:  []netip.Addr{netip.MustParseAddr("100.100.100.100")},
		MatchDomains: []dnsname.FQDN{"lab.example."},
	}); err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || applied[1].MatchDomains[0] != "lab.example" {
		t.Fatalf("got %+v", applied)
	}
	a, s := m.stats()
	if a != 2 || s != 5 {
		t.Fatalf("stats applied=%d skipped=%d", a, s)
	}
}
