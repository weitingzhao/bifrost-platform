package agentthreads

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Work is one external work as the runtime last recorded it, with the
// session identity a collector registered it under (R5:5.5-12 §1.4): what a
// session's binding is judged against (§3.1). Host is empty for a record
// without one.
type Work struct {
	ID, Root, Node, State string
	Host, Vendor, Session string
	Epoch                 int
}

// WorksFunc reads the runtime's external work; an error is a runtime that
// could not be read.
type WorksFunc func(ctx context.Context) ([]Work, error)

// worksTTL is how long the runtime's works are reused between reads (§3.1:
// the same query shape as the plan view's, cached 10 seconds).
const worksTTL = 10 * time.Second

// Binding is the server's judgement of a session (§3.1): linked to the
// latest work of its key, with how many earlier epochs of the session had a
// work; unlinked; refused (the client said so); unsupported (a Codex hook
// thread, §2.5); unknown (no host on the record, or the runtime unread).
type Binding struct {
	State          string `json:"state"`
	ExternalWorkID string `json:"external_work_id,omitempty"`
	Root           string `json:"root,omitempty"`
	Node           string `json:"plan_node_id,omitempty"`
	WorkState      string `json:"work_state,omitempty"`
	History        int    `json:"history,omitempty"`
}

const (
	BindLinked      = "linked"
	BindUnlinked    = "unlinked"
	BindRefused     = "refused"
	BindUnsupported = "unsupported"
	BindUnknown     = "unknown"
)

// The server's notes on a row (§3.1).
const (
	NoteCodexBinding = "codex_binding_after_s1_0"
	NoteUnreadable   = "runtime_unreadable"
	NoteLegacyNoHost = "legacy_no_host"
)

type workKey struct {
	host, vendor, session string
	epoch                 int
}

// works indexes the runtime's works by key, or holds why they were not read.
type works struct {
	byKey   map[workKey]Work
	history map[workKey][]int // epochs with a work, by host, vendor and session (epoch 0)
	noHost  map[workKey]bool  // keys of records without a host (host "")
	err     error
}

func indexWorks(list []Work, err error) works {
	w := works{byKey: map[workKey]Work{}, history: map[workKey][]int{}, noHost: map[workKey]bool{}, err: err}
	for _, x := range list {
		if x.Vendor == "" || x.Session == "" {
			continue
		}
		k := workKey{x.Host, x.Vendor, x.Session, x.Epoch}
		if x.Host == "" {
			w.noHost[k] = true
			continue
		}
		w.byKey[k] = x
		s := workKey{x.Host, x.Vendor, x.Session, 0}
		w.history[s] = append(w.history[s], x.Epoch)
	}
	return w
}

// threadKey is a row's key: its host, vendor and session, with its epoch.
func threadKey(t Thread) workKey {
	session, epoch := sessionKey(t.Thread, t.Session, t.Epoch)
	return workKey{t.Host, t.Vendor, session, epoch}
}

// bind judges a row (§3.1). The client's own word decides nothing but
// refused: a client saying linked with no work of its key is unlinked.
func (w works) bind(t Thread) (Binding, string) {
	k := threadKey(t)
	switch {
	case t.Vendor == "codex":
		return Binding{State: BindUnsupported}, NoteCodexBinding
	case w.err != nil:
		return Binding{State: BindUnknown}, NoteUnreadable
	}
	if x, ok := w.byKey[k]; ok {
		earlier := 0
		for _, e := range w.history[workKey{k.host, k.vendor, k.session, 0}] {
			if e < k.epoch {
				earlier++
			}
		}
		return Binding{State: BindLinked, ExternalWorkID: x.ID, Root: x.Root, Node: x.Node, WorkState: x.State, History: earlier}, ""
	}
	if w.noHost[workKey{"", k.vendor, k.session, k.epoch}] {
		return Binding{State: BindUnknown}, NoteLegacyNoHost
	}
	if t.ClientBind != nil && t.ClientBind.State == BindRefused {
		return Binding{State: BindRefused}, ""
	}
	return Binding{State: BindUnlinked}, ""
}

// workCache reuses one read of the runtime for worksTTL.
type workCache struct {
	mu   sync.Mutex
	read WorksFunc
	at   time.Time
	w    works
}

func (c *workCache) get(ctx context.Context, now time.Time) works {
	if c == nil || c.read == nil {
		return works{err: fmt.Errorf("the agent runtime is not wired to the thread list")}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if age := now.Sub(c.at); !c.at.IsZero() && age >= 0 && age < worksTTL {
		return c.w
	}
	list, err := c.read(ctx)
	c.w, c.at = indexWorks(list, err), now
	return c.w
}

// SessionCounts are the retained sessions by binding (§3.2): one per key,
// judged on its latest row; the five counts sum to the sessions retained.
type SessionCounts struct {
	Linked      int    `json:"linked"`
	Unlinked    int    `json:"unlinked"`
	Refused     int    `json:"refused"`
	Unsupported int    `json:"unsupported"`
	Unknown     int    `json:"unknown"`
	Scope       string `json:"scope"`
}

// sessionCounts counts st's sessions by key, a key's latest row judging it
// (a key recovered under a new thread id, <session>:n, is the same key).
func (c Config) sessionCounts(st State, w works) SessionCounts {
	latest := map[workKey]Thread{}
	for _, t := range st.Threads {
		k := threadKey(t)
		if cur, ok := latest[k]; !ok || t.At.After(cur.At) || t.At.Equal(cur.At) && Key(t.Vendor, t.Thread) > Key(cur.Vendor, cur.Thread) {
			latest[k] = t
		}
	}
	out := SessionCounts{Scope: fmt.Sprintf("Sessions heard from in the last %s, one per host, vendor, session and epoch, across every root", retainText(c.Retain))}
	for _, t := range latest {
		b, _ := w.bind(t)
		switch b.State {
		case BindLinked:
			out.Linked++
		case BindUnlinked:
			out.Unlinked++
		case BindRefused:
			out.Refused++
		case BindUnsupported:
			out.Unsupported++
		default:
			out.Unknown++
		}
	}
	return out
}

func retainText(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	return d.String()
}
