package runtime

import (
	"sync"
	"time"
)

// allowInvoke is the in-process cap on Invoke. It has no off switch.
// Preview does not use it. A full window returns before policy and HTTP.

const (
	invokePerWindow = 16
	invokeWindow    = time.Second
	invokeCallers   = 256
)

// InvokeGate is the per-runtime invoke cap. Zero Per, Window, and Max use 16 calls per second and 256 callers.
type InvokeGate struct {
	Per    int
	Window time.Duration
	Max    int

	mu sync.Mutex
	by map[string]*callerWindow
}

type callerWindow struct {
	start time.Time
	n     int
}

func (g *InvokeGate) allow(caller string, now time.Time) (bool, time.Duration) {
	if g == nil {
		return true, 0
	}
	per := g.Per
	if per <= 0 {
		per = invokePerWindow
	}
	window := g.Window
	if window <= 0 {
		window = invokeWindow
	}
	maxCallers := g.Max
	if maxCallers <= 0 {
		maxCallers = invokeCallers
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.by == nil {
		g.by = map[string]*callerWindow{}
	}
	w := g.by[caller]
	if w == nil {
		g.evict(now, window)
		if len(g.by) >= maxCallers {
			return false, window
		}
		w = &callerWindow{start: now}
		g.by[caller] = w
	}
	if w.start.IsZero() || now.Sub(w.start) >= window {
		w.start = now
		w.n = 0
	}
	if w.n >= per {
		return false, max(window-now.Sub(w.start), 0)
	}
	w.n++
	return true, 0
}

func (g *InvokeGate) evict(now time.Time, window time.Duration) {
	for caller, w := range g.by {
		if w == nil || w.start.IsZero() || now.Sub(w.start) >= window {
			delete(g.by, caller)
		}
	}
}
