package tether

import (
	"sync"
	"time"
)

// Path is one NWPathMonitor update, reduced to what matters for the engine.
type Path struct {
	Satisfied bool   `json:"satisfied"`
	Interface string `json:"interface"` // first usable interface, e.g. en0, pdp_ip0
	IPv4      bool   `json:"ipv4"`
	IPv6      bool   `json:"ipv6"`
	Expensive bool   `json:"expensive"` // cellular or hotspot
}

// key is what decides whether an update is a real change. Address changes
// within the same interface and families (IPv6 privacy rotation, a new
// DHCP lease with the same route) are not: magicsock notices a changed
// endpoint on its own, through STUN or DERP, without tearing anything down.
func (p Path) key() Path { p.Expensive = false; return p }

// pathFilter coalesces path updates. An update reaches onChange only when
// its key differs from the last one passed on, and only after the path has
// held still for settle (iOS reports a handover as a burst: interface gone,
// both up, old one gone). Unsatisfied paths are passed on at once, so the
// engine stops trying to send into a dead link; the satisfied path that
// follows them then always counts as a change.
type pathFilter struct {
	settle   time.Duration
	onChange func(Path)
	now      func() time.Time
	after    func(time.Duration, func()) stopper

	mu       sync.Mutex
	last     *Path // last passed on
	pending  *Path
	timer    stopper
	seen     int
	passed   int
	lastSeen time.Time
}

type stopper interface{ Stop() bool }

func newPathFilter(settle time.Duration, onChange func(Path)) *pathFilter {
	return &pathFilter{
		settle:   settle,
		onChange: onChange,
		now:      time.Now,
		after: func(d time.Duration, f func()) stopper {
			return time.AfterFunc(d, f)
		},
	}
}

func (f *pathFilter) update(p Path) {
	f.mu.Lock()
	f.seen++
	f.lastSeen = f.now()
	if !p.Satisfied {
		if f.timer != nil {
			f.timer.Stop()
			f.timer = nil
		}
		f.pending = nil
		if f.last != nil && !f.last.Satisfied {
			f.mu.Unlock()
			return
		}
		pp := p
		f.last = &pp
		f.passed++
		f.mu.Unlock()
		f.onChange(p)
		return
	}
	if f.last != nil && f.last.key() == p.key() {
		// Back to what the engine already has: drop any pending change.
		if f.timer != nil {
			f.timer.Stop()
			f.timer = nil
		}
		f.pending = nil
		f.mu.Unlock()
		return
	}
	pp := p
	f.pending = &pp
	if f.timer != nil {
		f.timer.Stop()
	}
	f.timer = f.after(f.settle, f.fire)
	f.mu.Unlock()
}

func (f *pathFilter) fire() {
	f.mu.Lock()
	p := f.pending
	f.pending = nil
	f.timer = nil
	if p == nil {
		f.mu.Unlock()
		return
	}
	f.last = p
	f.passed++
	f.mu.Unlock()
	f.onChange(*p)
}

func (f *pathFilter) stats() (seen, passed int, last *Path) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.last != nil {
		l := *f.last
		last = &l
	}
	return f.seen, f.passed, last
}
