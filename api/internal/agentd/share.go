package agentd

import (
	"context"
	"sync"
)

// How attempts share the host when several run at once (W-31 2.4 slice ③,
// review 276 A2). They run as one user, with no wall between their
// workspaces: an attempt whose processes may write outside its own workspace
// (unconfined) could change another's checkout, or the path a sibling is
// cloned to with the Gitea credentials, while that one is being prepared. So
// an unconfined attempt has the host to itself, from before its checkout to
// its end, and confined attempts run side by side. Confined: claude, whose
// tools only read; cursor in ask mode (anything but a branch task: it writes
// no file and runs only read-only commands); codex, in its workspace-write
// sandbox. Unconfined: a task with setup commands (they run unsandboxed),
// the deterministic executor (it runs the repository's own checks), cursor
// on a branch task, and any executor this does not know.
func confined(cl *Claimed, prep TaskSpec) bool {
	if len(prep.Setup) > 0 {
		return false
	}
	switch cl.Tool.Executor {
	case "exec:claude", "exec:codex":
		return true
	case "exec:cursor":
		return cl.EffectClass != "branch"
	}
	return false
}

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
