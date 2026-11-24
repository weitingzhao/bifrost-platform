package unitplan

import (
	"fmt"
	"sync"
)

// advanceManual and advanceSTG are the values a pin's advance may take.
//
// manual: whoever needs a newer commit changes the line. stg: the pin follows
// what STG ships, the commit of that repository in the platform's current STG
// release, and the line's commit is only the fallback for a process that
// cannot read the release (TD-588: on 2026-11-21 and 2026-11-23 the console's
// ui pin lagged ui main, and every daily unit run between a console change and
// the pin's move reported a regression that was not there).
const (
	advanceManual = "manual"
	advanceSTG    = "stg"
)

// Follower gives the commit a pin with advance stg follows now, and where it
// came from ("stg release <run>"); "" when it cannot say, and the pin keeps
// the commit its line names.
type Follower func(repo string) (commit, from string)

var follow struct {
	sync.RWMutex
	f Follower
}

// SetFollower sets the follower of every plan this process builds, so the five
// places that build one (BuildFrom, ScopeOf, the unit watch, ciconfig, the
// unit dispatch) see the same commit for the same pin. nil clears it. The
// platform's assembly sets it once; a test sets it and clears it after.
func SetFollower(f Follower) {
	follow.Lock()
	follow.f = f
	follow.Unlock()
}

// pinCommit is the commit a pin is planned at: its line's commit, or for
// advance stg the follower's, with a note saying which and why.
func pinCommit(repo string, p Pin) (commit, note string) {
	if p.Advance != advanceSTG {
		return p.Commit, ""
	}
	follow.RLock()
	f := follow.f
	follow.RUnlock()
	if f == nil {
		return p.Commit, "advance stg: no follower in this process; the line's commit"
	}
	got, from := f(repo)
	if !fullID.MatchString(got) {
		return p.Commit, "advance stg: the STG release's commit could not be read; the line's commit"
	}
	return got, fmt.Sprintf("advance stg: %s", from)
}
