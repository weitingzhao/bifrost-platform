package agentthreads

// A session key (host, vendor, session, epoch) and the state every row of
// it shares: first sighting, compactions, the client's word, MetaAt; the
// side beats that change it; and the clock retention reads (P-c2).

import (
	"time"
)

// heldKeyState is a session key's state another row holds (review M2).
type heldKeyState struct {
	FirstSeenAt time.Time
	Compactions int
	ClientBind  *ClientBind
	MetaAt      time.Time
}

// beatKey is the key b writes on t (seen or not): an old client's beat keeps
// the row's.
func beatKey(t Thread, seen bool, b Beat) (string, int) {
	if seen && b.Session == "" {
		return sessionKey(t.Thread, t.Session, t.Epoch)
	}
	return sessionKey(b.Thread, b.Session, b.Epoch)
}

// sessionKey is the session a beat or a thread is bound by, with its host
// and vendor (§3.1): an old client's is its thread id, epoch 0.
func sessionKey(thread, session string, epoch int) (string, int) {
	if session == "" {
		return thread, 0
	}
	return session, epoch
}

// applySide folds a side beat (§4.1): heard from, shown as the last event,
// counted when it is a precompact; the turn, its tool window and a wait are
// as they were. A thread it opens is in no turn.
//
// The turn's clock is the turn's: on a row in a turn or a wait, a side beat
// moves neither the time silence is measured from nor a claim on it, so a
// tool's deadline and a silent page stay where they were (review M1). On a
// row in no turn it is the last thing heard, and that row is stored as
// turn_end, the one event every binary reads as idle, the side beat in Mark
// alone: a binary from before P-c2 never times it as a turn (review M4).
func (st *State) applySide(k string, t Thread, seen bool, b Beat, at time.Time, shared bool) bool {
	if !seen {
		t.Thread, t.Vendor, t.Host, t.Event, t.TurnStartedAt = b.Thread, b.Vendor, b.Host, TurnEnd, at
	}
	idle := t.idle()
	if b.Work != "" {
		t.Work = b.Work
	}
	if b.Title != "" {
		t.Title = b.Title
	}
	if b.keyHash != "" {
		t.KeyHash = b.keyHash
	}
	if b.boundHost != "" {
		t.HostBound = true
	}
	if b.Sidebar != "" {
		t.Sidebar = b.Sidebar
	}
	if b.Event == PreCompact {
		t.Compactions++
		t.MetaAt = at
		shared = true
	}
	t.Mark, t.MarkReason, t.Seq = b.Event, b.Reason, b.Seq
	if idle {
		t.At = at
	}
	t.HeardAt = at
	st.Threads[k] = t
	if shared {
		st.shareKeyMeta(k, t)
	}
	return true
}

// lastHeard is when a row was last heard from: the turn's clock or, later,
// a side beat that left that clock alone.
func lastHeard(t Thread) time.Time {
	if t.HeardAt.After(t.At) {
		return t.HeardAt
	}
	return t.At
}

// shareKeyMeta gives every other row of t's key (host, vendor, session and
// epoch) the copy of the key's state t now holds. A session recovered under
// other thread ids (<session>:n) is one key: its rows carry one copy, so a
// row's own clock never decides which state is current, and a precompact or
// a diagnosis on any row lands on all of them (P-c2 landing review rounds 1
// and 2).
func (st *State) shareKeyMeta(skip string, t Thread) {
	session, epoch := sessionKey(t.Thread, t.Session, t.Epoch)
	for k, o := range st.Threads {
		if k == skip || o.Host != t.Host || o.Vendor != t.Vendor {
			continue
		}
		if os, oe := sessionKey(o.Thread, o.Session, o.Epoch); os != session || oe != epoch {
			continue
		}
		o.FirstSeenAt, o.Compactions, o.MetaAt, o.ClientBind = t.FirstSeenAt, t.Compactions, t.MetaAt, nil
		if t.ClientBind != nil {
			cb := *t.ClientBind
			o.ClientBind = &cb
		}
		st.Threads[k] = o
	}
}

// keyRows copies the rows of b's key other than b's own, so a caller that
// may yet refuse b's new row can put them back (foldBatch).
func (st *State) keyRows(b Beat) map[string]Thread {
	session, epoch := sessionKey(b.Thread, b.Session, b.Epoch)
	out := map[string]Thread{}
	for k, o := range st.Threads {
		if k == Key(b.Vendor, b.Thread) || o.Host != b.Host || o.Vendor != b.Vendor {
			continue
		}
		if os, oe := sessionKey(o.Thread, o.Session, o.Epoch); os == session && oe == epoch {
			out[k] = o
		}
	}
	return out
}

// olderEpoch says whether b names the row's own session at an earlier epoch
// than the row's: a late beat of a life of the session that has ended.
func olderEpoch(t Thread, b Beat) bool {
	if b.Session == "" {
		return false
	}
	session, epoch := sessionKey(t.Thread, t.Session, t.Epoch)
	return b.Session == session && b.Epoch < epoch
}

// heldKey is what the platform holds for a session's key on a row other
// than skip: the copy whose metadata changed last (metaTime), with the key's
// earliest first sighting. nil: no other row holds the key.
func (st *State) heldKey(skip, host, vendor, session string, epoch int) *heldKeyState {
	var held Thread
	var first time.Time
	found := false
	for k, o := range st.Threads {
		os, oe := sessionKey(o.Thread, o.Session, o.Epoch)
		if k == skip || o.Host != host || o.Vendor != vendor || os != session || oe != epoch {
			continue
		}
		if !found || metaTime(o).After(metaTime(held)) {
			held = o
		}
		if !o.FirstSeenAt.IsZero() && (first.IsZero() || o.FirstSeenAt.Before(first)) {
			first = o.FirstSeenAt
		}
		found = true
	}
	if !found {
		return nil
	}
	if !first.IsZero() {
		held.FirstSeenAt = first
	}
	return &heldKeyState{FirstSeenAt: held.FirstSeenAt, Compactions: held.Compactions, ClientBind: held.ClientBind, MetaAt: metaTime(held)}
}

// metaTime orders rows' copies of a key's state. A row stored before
// MetaAt existed has none; its last receive time stands in.
func metaTime(t Thread) time.Time {
	if t.MetaAt.IsZero() {
		return t.At
	}
	return t.MetaAt
}
