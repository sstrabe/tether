package tether

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"tailscale.com/control/controlclient"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnlocal"
	"tailscale.com/ipn/store"
	"tailscale.com/net/netmon"
	"tailscale.com/net/tsdial"
	"tailscale.com/tsd"
	"tailscale.com/types/logid"
	"tailscale.com/wgengine"
	"tailscale.com/wgengine/magicsock"
	"tailscale.com/wgengine/netstack"
)

// tuneForMobile sets the knobs our Tailscale patches add (patches/).
func tuneForMobile() {
	netmon.IgnoreAddedIPs = true
	magicsock.DERPRebindPingTimeout = 8 * time.Second
	magicsock.DERPRebindPingTries = 2
}

// Host is the side that embeds the engine (the extension, through the C
// bridge, or the harness).
type Host interface {
	// ApplySettings puts the tunnel's network settings in place and returns
	// when that is done. Only called in TUN mode.
	ApplySettings(NetworkSettings) error
	// Event reports a state change; see Event.
	Event(Event)
}

// Event is sent to the host as JSON.
type Event struct {
	Kind string `json:"kind"` // state, browseToURL, error, settings, path
	// State is the backend state (NoState, NeedsLogin, NeedsMachineAuth,
	// Stopped, Starting, Running) for kind=state.
	State string `json:"state,omitempty"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
	Path  *Path  `json:"path,omitempty"`
}

// Engine is one running node.
type Engine struct {
	cfg     Config
	host    Host
	logs    *logRing
	started time.Time

	sys    *tsd.System
	netMon *netmon.Monitor
	lb     *ipnlocal.LocalBackend
	merger *settingsMerger
	paths  *pathFilter

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	stopped  bool
	rebinds  int
	lastWake time.Time
}

// Start brings the node up. It returns once the backend is running; login
// and connection happen afterwards and are reported through host.Event.
func Start(cfg Config, host Host) (_ *Engine, retErr error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	tuneForMobile()
	debug.SetMemoryLimit(int64(cfg.MemoryLimitMB) << 20)
	debug.SetGCPercent(50)

	logs, err := newLogRing(filepath.Join(cfg.StateDir, "tether.log"), 1<<20)
	if err != nil {
		return nil, err
	}
	logf := logs.logf
	logf("tether: starting, control %s, tun fd %d, %s/%s", cfg.ControlURL, cfg.TunFD, runtime.GOOS, runtime.GOARCH)

	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{cfg: cfg, host: host, logs: logs, started: time.Now(), ctx: ctx, cancel: cancel}
	var closers []func()
	defer func() {
		if retErr != nil {
			cancel()
			for i := len(closers) - 1; i >= 0; i-- {
				closers[i]()
			}
			logs.close()
		}
	}()

	sys := tsd.NewSystem()
	e.sys = sys
	netMon, err := netmon.New(sys.Bus.Get(), logf)
	if err != nil {
		return nil, fmt.Errorf("netmon: %w", err)
	}
	closers = append(closers, func() { netMon.Close() })
	e.netMon = netMon

	dialer := &tsdial.Dialer{Logf: logf}
	dialer.SetBus(sys.Bus.Get())
	sys.Set(dialer)

	conf := wgengine.Config{
		EventBus:      sys.Bus.Get(),
		NetMon:        netMon,
		Dialer:        dialer,
		SetSubsystem:  sys.Set,
		ControlKnobs:  sys.ControlKnobs(),
		HealthTracker: sys.HealthTracker.Get(),
		Metrics:       sys.UserMetricsRegistry(),
	}
	tunMode := cfg.TunFD >= 0
	if tunMode {
		dev, err := tunFromFD(cfg.TunFD)
		if err != nil {
			return nil, fmt.Errorf("tun from fd %d: %w", cfg.TunFD, err)
		}
		e.merger = &settingsMerger{apply: func(s NetworkSettings) error {
			logf("tether: applying network settings: %d addresses, %d routes, %d dns servers", len(s.Addresses), len(s.Routes), len(s.DNSServers))
			return host.ApplySettings(s)
		}}
		conf.Tun = dev
		conf.Router = callbackRouter{e.merger}
		conf.DNS = callbackDNS{e.merger}
		sys.Set(conf.Router)
	}
	eng, err := wgengine.NewUserspaceEngine(logf, conf)
	if err != nil {
		return nil, fmt.Errorf("engine: %w", err)
	}
	closers = append(closers, eng.Close)
	sys.Set(eng)
	sys.HealthTracker.Get().SetMetricsRegistry(sys.UserMetricsRegistry())

	ns, err := netstack.Create(logf, sys.Tun.Get(), eng, sys.MagicSock.Get(), dialer, sys.DNSManager.Get(), sys.ProxyMapper())
	if err != nil {
		return nil, fmt.Errorf("netstack: %w", err)
	}
	if !tunMode {
		// No OS stack behind us: netstack answers for our own addresses.
		ns.ProcessLocalIPs = true
		ns.ProcessSubnets = true
	}
	sys.Tun.Get().Start()
	sys.Set(ns)
	sys.NetstackRouter.Set(false)

	st, err := store.New(logf, filepath.Join(cfg.StateDir, "tailscaled.state"))
	if err != nil {
		return nil, fmt.Errorf("state store: %w", err)
	}
	sys.Set(st)

	// Zero log ID and no uploader: nothing goes to log.tailscale.com.
	lb, err := ipnlocal.NewLocalBackend(logf, logid.PublicID{}, sys, controlclient.LoginDefault)
	if err != nil {
		return nil, fmt.Errorf("backend: %w", err)
	}
	closers = append(closers, lb.Shutdown)
	lb.SetVarRoot(cfg.StateDir)
	e.lb = lb
	if !tunMode {
		dialer.UseNetstackForIP = func(ip netip.Addr) bool {
			_, ok := lb.PeerForIP(ip)
			return ok
		}
	}
	if err := ns.Start(lb); err != nil {
		return nil, fmt.Errorf("netstack start: %w", err)
	}

	e.paths = newPathFilter(2*time.Second, e.applyPath)
	go e.watch()

	if err := lb.Start(ipn.Options{UpdatePrefs: e.prefs()}); err != nil {
		return nil, fmt.Errorf("backend start: %w", err)
	}
	logf("tether: backend started, state %v", lb.State())
	return e, nil
}

func (e *Engine) prefs() *ipn.Prefs {
	p := ipn.NewPrefs()
	p.ControlURL = e.cfg.ControlURL
	p.Hostname = e.cfg.Hostname
	p.WantRunning = true
	p.RouteAll = e.cfg.AcceptRoutes
	p.CorpDNS = e.cfg.AcceptDNS
	if e.cfg.ExitNode != "" {
		if ip, err := netip.ParseAddr(e.cfg.ExitNode); err == nil {
			p.ExitNodeIP = ip
		} else {
			p.ExitNodeID = tailcfgStableNodeID(e.cfg.ExitNode)
		}
	}
	return p
}

func (e *Engine) watch() {
	e.lb.WatchNotifications(e.ctx, ipn.NotifyInitialState, nil, func(n *ipn.Notify) bool {
		if n.State != nil {
			e.host.Event(Event{Kind: "state", State: n.State.String()})
		}
		if n.BrowseToURL != nil && *n.BrowseToURL != "" {
			e.host.Event(Event{Kind: "browseToURL", URL: *n.BrowseToURL})
		}
		if n.ErrMessage != nil && *n.ErrMessage != "" {
			e.host.Event(Event{Kind: "error", Error: *n.ErrMessage})
		}
		return true
	})
}

// Login starts an interactive login; the URL to open arrives as a
// browseToURL event.
func (e *Engine) Login() error {
	return e.lb.StartLoginInteractive(e.ctx)
}

// Logout drops the node key on the server and locally.
func (e *Engine) Logout() error {
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	return e.lb.Logout(ctx, nil)
}

// PathChanged takes every NWPathMonitor update; the filter decides which
// ones reach the engine.
func (e *Engine) PathChanged(p Path) {
	e.paths.update(p)
}

func (e *Engine) applyPath(p Path) {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.rebinds++
	e.mu.Unlock()
	e.logs.logf("tether: path change passed on: %+v", p)
	if p.Satisfied {
		setDefaultInterface(p.Interface)
	}
	e.netMon.InjectEvent()
	pp := p
	e.host.Event(Event{Kind: "path", Path: &pp})
}

// Wake is called when the device wakes. After a long sleep the NAT
// mappings and the DERP connection are likely gone, so re-check the network
// at once instead of waiting for a read to time out.
func (e *Engine) Wake(slept time.Duration) {
	e.mu.Lock()
	e.lastWake = time.Now()
	e.mu.Unlock()
	if slept > 30*time.Second {
		e.logs.logf("tether: woke after %v, re-checking network", slept.Round(time.Second))
		e.netMon.InjectEvent()
	}
}

// Stop shuts the node down. The node key stays, so the next Start
// reconnects without a login.
func (e *Engine) Stop() {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.stopped = true
	e.mu.Unlock()
	e.logs.logf("tether: stopping")
	e.cancel()
	e.lb.Shutdown()
	e.netMon.Close()
	e.logs.close()
}

// Logs returns the most recent log lines.
func (e *Engine) Logs(n int) []string { return e.logs.tail(n) }

var errStopped = errors.New("engine stopped")
