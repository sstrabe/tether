package tether

import (
	"sync"
	"testing"
	"time"
)

// fakeTimers lets the test fire the settle timer by hand.
type fakeTimer struct {
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool { was := !t.stopped; t.stopped = true; return was }

type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

func (c *fakeClock) after(_ time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{f: f}
	c.timers = append(c.timers, t)
	return t
}

// fire runs the newest live timer, as if settle had passed.
func (c *fakeClock) fire(t *testing.T) {
	c.mu.Lock()
	var live *fakeTimer
	for i := len(c.timers) - 1; i >= 0; i-- {
		if !c.timers[i].stopped {
			live = c.timers[i]
			break
		}
	}
	c.mu.Unlock()
	if live == nil {
		t.Fatal("no live timer")
	}
	live.stopped = true
	live.f()
}

func (c *fakeClock) live() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped {
			n++
		}
	}
	return n
}

func newTestFilter() (*pathFilter, *fakeClock, *[]Path) {
	var got []Path
	c := &fakeClock{}
	f := newPathFilter(time.Second, func(p Path) { got = append(got, p) })
	f.after = c.after
	return f, c, &got
}

var (
	wifi = Path{Satisfied: true, Interface: "en0", IPv4: true, IPv6: true}
	cell = Path{Satisfied: true, Interface: "pdp_ip0", IPv4: true, IPv6: true, Expensive: true}
	none = Path{}
)

func TestFirstPathPassesAfterSettle(t *testing.T) {
	f, c, got := newTestFilter()
	f.update(wifi)
	if len(*got) != 0 {
		t.Fatalf("passed before settle: %v", *got)
	}
	c.fire(t)
	if len(*got) != 1 || (*got)[0] != wifi {
		t.Fatalf("got %v", *got)
	}
}

func TestSameKeyRepeatsAreDropped(t *testing.T) {
	// What cellular does all day: the same interface and families, again
	// and again (IPv6 privacy addresses rotating, radio state changes).
	f, c, got := newTestFilter()
	f.update(cell)
	c.fire(t)
	for i := 0; i < 50; i++ {
		f.update(cell)
	}
	if c.live() != 0 {
		t.Fatalf("repeats armed a timer")
	}
	if len(*got) != 1 {
		t.Fatalf("got %d changes, want 1", len(*got))
	}
	seen, passed, _ := f.stats()
	if seen != 51 || passed != 1 {
		t.Fatalf("stats seen=%d passed=%d", seen, passed)
	}
}

func TestExpensiveFlagAloneIsNoChange(t *testing.T) {
	f, c, got := newTestFilter()
	f.update(wifi)
	c.fire(t)
	w := wifi
	w.Expensive = true // personal hotspot over the same en0
	f.update(w)
	if c.live() != 0 || len(*got) != 1 {
		t.Fatalf("expensive flip counted as change: %v", *got)
	}
}

func TestHandoverBurstCoalesces(t *testing.T) {
	// Wi-Fi -> cellular as iOS reports it: several updates within a second.
	f, c, got := newTestFilter()
	f.update(wifi)
	c.fire(t)
	f.update(cell)
	v4only := cell
	v4only.IPv6 = false
	f.update(v4only)
	f.update(cell)
	c.fire(t)
	if len(*got) != 2 || (*got)[1] != cell {
		t.Fatalf("got %v", *got)
	}
}

func TestBounceBackCancelsPending(t *testing.T) {
	f, c, got := newTestFilter()
	f.update(wifi)
	c.fire(t)
	f.update(cell)
	f.update(wifi) // back before settle
	if c.live() != 0 {
		t.Fatal("pending change still armed")
	}
	if len(*got) != 1 {
		t.Fatalf("got %v", *got)
	}
}

func TestUnsatisfiedPassesAtOnceAndOnlyOnce(t *testing.T) {
	f, c, got := newTestFilter()
	f.update(wifi)
	c.fire(t)
	f.update(none)
	f.update(none)
	if len(*got) != 2 || (*got)[1].Satisfied {
		t.Fatalf("got %v", *got)
	}
	f.update(wifi) // same as before the outage, still a change
	c.fire(t)
	if len(*got) != 3 || (*got)[2] != wifi {
		t.Fatalf("got %v", *got)
	}
}
