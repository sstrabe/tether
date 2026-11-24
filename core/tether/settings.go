package tether

import (
	"encoding/json"
	"net/netip"
	"slices"
	"sync"

	"tailscale.com/net/dns"
	"tailscale.com/wgengine/router"
)

// NetworkSettings is what the extension turns into
// NEPacketTunnelNetworkSettings. Router and DNS updates arrive separately
// from the engine; both are merged here and handed over as one.
type NetworkSettings struct {
	Addresses     []string `json:"addresses"`     // CIDR, ours (100.x/32, fd7a::/128)
	Routes        []string `json:"routes"`        // CIDR, into the tunnel
	ExcludeRoutes []string `json:"excludeRoutes"` // CIDR, kept off the tunnel
	MTU           int      `json:"mtu,omitempty"`
	DNSServers    []string `json:"dnsServers"`
	MatchDomains  []string `json:"matchDomains"`  // empty with servers set = all queries
	SearchDomains []string `json:"searchDomains"`
}

func (s NetworkSettings) equal(o NetworkSettings) bool {
	a, _ := json.Marshal(s)
	b, _ := json.Marshal(o)
	return string(a) == string(b)
}

// ApplyFunc applies settings on the OS and returns once they are in place
// (or failed). Called without the merger's lock held only from one goroutine
// at a time.
type ApplyFunc func(NetworkSettings) error

// settingsMerger collects router and DNS configs and applies the merged
// result, skipping it when nothing changed since the last successful apply.
type settingsMerger struct {
	apply ApplyFunc

	mu      sync.Mutex // serialises apply too
	rcfg    *router.Config
	dcfg    *dns.OSConfig
	last    *NetworkSettings
	applied int // successful applies
	skipped int // identical, not applied
}

func (m *settingsMerger) setRouter(c *router.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c == nil {
		c = &router.Config{}
	}
	cc := *c
	m.rcfg = &cc
	return m.flushLocked()
}

func (m *settingsMerger) setDNS(c dns.OSConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dcfg = &c
	return m.flushLocked()
}

func (m *settingsMerger) stats() (applied, skipped int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applied, m.skipped
}

func (m *settingsMerger) flushLocked() error {
	if m.rcfg == nil {
		// Nothing to apply before the engine has given us addresses.
		return nil
	}
	s := merge(m.rcfg, m.dcfg)
	if m.last != nil && m.last.equal(s) {
		m.skipped++
		return nil
	}
	if err := m.apply(s); err != nil {
		return err
	}
	m.last = &s
	m.applied++
	return nil
}

func prefixes(ps []netip.Prefix) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func merge(r *router.Config, d *dns.OSConfig) NetworkSettings {
	s := NetworkSettings{
		Addresses:     prefixes(r.LocalAddrs),
		Routes:        prefixes(r.Routes),
		ExcludeRoutes: prefixes(r.LocalRoutes),
		MTU:           r.NewMTU,
		DNSServers:    []string{},
		MatchDomains:  []string{},
		SearchDomains: []string{},
	}
	if d != nil {
		for _, a := range d.Nameservers {
			s.DNSServers = append(s.DNSServers, a.String())
		}
		for _, m := range d.MatchDomains {
			s.MatchDomains = append(s.MatchDomains, m.WithoutTrailingDot())
		}
		for _, sd := range d.SearchDomains {
			s.SearchDomains = append(s.SearchDomains, sd.WithoutTrailingDot())
		}
		slices.Sort(s.MatchDomains)
		slices.Sort(s.SearchDomains)
	}
	return s
}

// callbackRouter is the router.Router the engine sees in TUN mode.
type callbackRouter struct{ m *settingsMerger }

func (r callbackRouter) Up() error                  { return nil }
func (r callbackRouter) Set(c *router.Config) error { return r.m.setRouter(c) }
func (r callbackRouter) Close() error               { return nil }

// callbackDNS is the dns.OSConfigurator the engine sees in TUN mode. iOS
// supports split DNS through matchDomains.
type callbackDNS struct{ m *settingsMerger }

func (d callbackDNS) SetDNS(c dns.OSConfig) error { return d.m.setDNS(c) }
func (d callbackDNS) SupportsSplitDNS() bool      { return true }
func (d callbackDNS) GetBaseConfig() (dns.OSConfig, error) {
	return dns.OSConfig{}, dns.ErrGetBaseConfigNotSupported
}
func (d callbackDNS) Close() error { return nil }
