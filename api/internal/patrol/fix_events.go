package patrol

import (
	"context"
	"sync"
)

// FixFunc hears how an automatic fix of a checklist item ended.
type FixFunc func(ctx context.Context, item string, ok bool, detail string)

var (
	fixMu       sync.Mutex
	fixListener FixFunc
	lastFixOK   = map[string]bool{}
)

// OnFix sets who hears fixes. A fix is said when its outcome differs from that
// item's last one, so a fix failing on every run wakes nobody twice. Report-only
// runs and skipped items act on nothing and say nothing.
func OnFix(f FixFunc) {
	fixMu.Lock()
	defer fixMu.Unlock()
	fixListener = f
	lastFixOK = map[string]bool{}
}

func sayFix(ctx context.Context, r fixResult) {
	if r.Skipped {
		return
	}
	fixMu.Lock()
	f := fixListener
	was, seen := lastFixOK[r.ItemID]
	lastFixOK[r.ItemID] = r.Ok
	fixMu.Unlock()
	if f != nil && (!seen || was != r.Ok) {
		f(ctx, r.ItemID, r.Ok, r.Action+": "+r.Detail)
	}
}
