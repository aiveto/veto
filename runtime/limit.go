package runtime

import (
	"sync"
	"time"

	"github.com/aiveto/veto/policy"
)

// allowInvoke is the in-process cap on Invoke. It has no off switch.
// Preview does not use it. A full window returns before policy and HTTP.

const (
	invokePerWindow = 16
	invokeWindow    = time.Second
)

type gateKey struct {
	state  *policy.State
	caller string
}

type window struct {
	mu    sync.Mutex
	start time.Time
	n     int
}

var gates sync.Map

func allowInvoke(state *policy.State, caller string, now time.Time) bool {
	v, _ := gates.LoadOrStore(gateKey{state: state, caller: caller}, &window{})
	return v.(*window).allow(now)
}

func (w *window) allow(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.start.IsZero() || now.Sub(w.start) >= invokeWindow {
		w.start = now
		w.n = 0
	}
	if w.n >= invokePerWindow {
		return false
	}
	w.n++
	return true
}
