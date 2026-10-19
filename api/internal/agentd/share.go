package agentd

import (
	"context"
	"sync"
)

// How attempts share the host when several run at once (W-31 2.4 slice ③,
// reviews 276 A2, 277 A2, 278 A2). They run as one user, with no wall
// between their workspaces: an attempt whose processes may write outside its
// own workspace (unconfined) could change another's checkout, or the path a
// sibling is cloned to with the Gitea credentials, while that one is being
// prepared. So an unconfined attempt has the host to itself, from before its
// checkout to its end, and only confined attempts run side by side.
//
// An attempt is confined when its executor runs inside agentd's write wall
// (confine.go): its whole process tree, setup and executor, may write only
// its own workspace, output and siblings, with a home of its own. That is
// Daemon.Wall, which holds an executor only when ProveWall proved the wall on
// this host for it; Run then wraps the attempt's command in the wall and
// refuses to start one whose spawn lacks a pin (checkPins). Without a wall
// (macOS, an old kernel, a probe that failed) no attempt is confined, and
// each has the host alone (TD-371).

// hostShare lets confined attempts hold the host together and an unconfined
// one alone. An unconfined attempt that waits holds back the confined that
// come after it, so it is not put off for ever.
type hostShare struct {
	mu       sync.Mutex
	shared   int
	alone    bool
	waiting  int           // unconfined attempts waiting for the host
	changed  chan struct{} // closed, and replaced, whenever the host is let go
	initDone bool
}

func (h *hostShare) init() {
	if !h.initDone {
		h.changed, h.initDone = make(chan struct{}), true
	}
}

func (h *hostShare) wake() {
	close(h.changed)
	h.changed = make(chan struct{})
}

// take holds the host, alone or shared, until release is called; it gives up
// when ctx ends.
func (h *hostShare) take(ctx context.Context, alone bool) (release func(), err error) {
	h.mu.Lock()
	h.init()
	if alone {
		h.waiting++
	}
	for {
		free := !h.alone && (alone && h.shared == 0 || !alone && h.waiting == 0)
		if free {
			if alone {
				h.waiting--
				h.alone = true
			} else {
				h.shared++
			}
			h.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					h.mu.Lock()
					if alone {
						h.alone = false
					} else {
						h.shared--
					}
					h.wake()
					h.mu.Unlock()
				})
			}, nil
		}
		ch := h.changed
		h.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			h.mu.Lock()
			if alone {
				h.waiting--
				h.wake() // the confined behind it may go now
			}
			h.mu.Unlock()
			return nil, ctx.Err()
		}
		h.mu.Lock()
	}
}
