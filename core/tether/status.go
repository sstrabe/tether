package tether

import (
	"runtime"
	"slices"
	"time"

	"tailscale.com/tailcfg"
)

func tailcfgStableNodeID(s string) tailcfg.StableNodeID { return tailcfg.StableNodeID(s) }

// Status is the snapshot the app shows. The counters are what the drops
// are debugged with: how often iOS reported a path change, how many of
// those reached the engine, how often settings were (re)applied.
type Status struct {
	State       string    `json:"state"`
	Self        *Node     `json:"self,omitempty"`
	Peers       []Node    `json:"peers"`
	Health      []string  `json:"health"`
	AuthURL     string    `json:"authURL,omitempty"`
	TailnetName string    `json:"tailnetName,omitempty"`
	Uptime      float64   `json:"uptimeSeconds"`
	Counters    Counters  `json:"counters"`
	LastPath    *Path     `json:"lastPath,omitempty"`
	Memory      MemStats  `json:"memory"`
	Time        time.Time `json:"time"`
}

type Node struct {
	Name      string   `json:"name"`
	DNSName   string   `json:"dnsName,omitempty"`
	OS        string   `json:"os,omitempty"`
	IPs       []string `json:"ips"`
	Online    bool     `json:"online"`
	Relay     string   `json:"relay,omitempty"`   // home DERP region
	CurAddr   string   `json:"curAddr,omitempty"` // direct endpoint, empty if relayed
	ExitNode  bool     `json:"exitNode,omitempty"`
	ExitOK    bool     `json:"exitNodeOption,omitempty"`
	LastSeen  string   `json:"lastSeen,omitempty"`
	RxBytes   int64    `json:"rxBytes"`
	TxBytes   int64    `json:"txBytes"`
	Handshake string   `json:"lastHandshake,omitempty"`
}

type Counters struct {
	PathUpdates     int `json:"pathUpdates"`     // from NWPathMonitor
	PathChanges     int `json:"pathChanges"`     // passed on to the engine
	SettingsApplied int `json:"settingsApplied"` // setTunnelNetworkSettings calls
	SettingsSkipped int `json:"settingsSkipped"` // identical, not re-applied
}

type MemStats struct {
	HeapMB  float64 `json:"heapMB"`
	SysMB   float64 `json:"sysMB"`
	LimitMB int     `json:"limitMB"`
	NumGC   uint32  `json:"numGC"`
}

// Status returns the current snapshot.
func (e *Engine) Status() Status {
	st := e.lb.Status()
	s := Status{
		State:  st.BackendState,
		Peers:  []Node{},
		Health: append([]string{}, st.Health...),
		Uptime: time.Since(e.started).Seconds(),
		Time:   time.Now(),
	}
	s.AuthURL = st.AuthURL
	if st.CurrentTailnet != nil {
		s.TailnetName = st.CurrentTailnet.Name
	}
	if st.Self != nil {
		n := nodeFrom(st.Self.HostName, st.Self.DNSName, st.Self.OS, st.Self.TailscaleIPs, st.Self.Online, st.Self.Relay, st.Self.CurAddr)
		s.Self = &n
	}
	for _, p := range st.Peer {
		n := nodeFrom(p.HostName, p.DNSName, p.OS, p.TailscaleIPs, p.Online, p.Relay, p.CurAddr)
		n.ExitNode = p.ExitNode
		n.ExitOK = p.ExitNodeOption
		n.RxBytes = p.RxBytes
		n.TxBytes = p.TxBytes
		if !p.LastSeen.IsZero() {
			n.LastSeen = p.LastSeen.UTC().Format(time.RFC3339)
		}
		if !p.LastHandshake.IsZero() {
			n.Handshake = p.LastHandshake.UTC().Format(time.RFC3339)
		}
		s.Peers = append(s.Peers, n)
	}
	slices.SortFunc(s.Peers, func(a, b Node) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	seen, passed, last := e.paths.stats()
	s.Counters.PathUpdates = seen
	s.Counters.PathChanges = passed
	s.LastPath = last
	if e.merger != nil {
		s.Counters.SettingsApplied, s.Counters.SettingsSkipped = e.merger.stats()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.Memory = MemStats{
		HeapMB:  float64(ms.HeapAlloc) / (1 << 20),
		SysMB:   float64(ms.Sys) / (1 << 20),
		LimitMB: e.cfg.MemoryLimitMB,
		NumGC:   ms.NumGC,
	}
	return s
}
