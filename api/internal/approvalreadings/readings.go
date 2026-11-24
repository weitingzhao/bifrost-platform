// Package approvalreadings computes read-only observations of retained approval records.
// It neither approves nor executes requests and does not read storage or HTTP state.
package approvalreadings

import (
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// GET /api/v1/readings/approvals is what the approval records say, counted:
// a day, a requester, how many requests were made, how long approval took,
// and what became of the approved ones. The Owner, 2026-11-14: the Ops page
// is a window on the system, and what it shows is what the system measured,
// with no drift from a ledger kept by hand. Every count comes with the
// numbers of the requests it counts, so each can be opened. It reads the
// approval store's retained records and nothing else; it computes on every
// request and stores nothing. Read-only; viewer and above.
//
// A day is the day the request was made, in the Owner's zone
// (planpace.BriefZone). "Approved" is decided and not rejected. A request
// approved and then failed, or left with a result nobody could read
// (unknown), is an approval that bought no change: failed and unknown list
// those. Whether an approval should not have been asked for at all is not in
// the records and is not counted here.

type Group struct {
	Requester string `json:"requester"`
	// Thread is the requester's own one line about itself, from its newest
	// request that day that gave one; WorkIDs the work items its requests name.
	Thread  string   `json:"thread,omitempty"`
	WorkIDs []string `json:"work_ids"`
	// Each list is request numbers (#n), ascending.
	Requests []int `json:"requests"` // made that day, drills left out
	Pending  []int `json:"pending"`  // not decided yet
	Rejected []int `json:"rejected"`
	Expired  []int `json:"expired"` // never decided, or approved and never run
	Approved []int `json:"approved"`
	// ApproveSecondsMedian is the median of creation to decision over
	// Approved; absent when none was approved.
	ApproveSecondsMedian *int  `json:"approve_seconds_median,omitempty"`
	Executed             []int `json:"executed"` // approved and done
	Failed               []int `json:"failed"`   // approved, ran, failed
	Unknown              []int `json:"unknown"`  // approved, result unreadable
	Waiting              []int `json:"waiting"`  // approved, not finished yet
	Drills               []int `json:"drills"`   // made to exercise the pages; never approved
}

type Day struct {
	Day    string  `json:"day"`
	Groups []Group `json:"groups"`
	// ToOwner is every card of the day that reached the Owner, split by
	// whether it should have (Owner 2026-11-18/19, laws 31 and 46: only a
	// change of law and the six classes that stay his). Daily is the gap
	// the approvals line closes to 0. Six is the requester's own claim
	// (owner_class); drills are left out.
	ToOwner ToOwner `json:"to_owner"`
}

// ToOwner lists request numbers, ascending.
type ToOwner struct {
	Law   []int `json:"law"`
	Six   []int `json:"six"`
	Daily []int `json:"daily"`
	// SixByClass is Six split by the class the requester named (H1-H6);
	// every class is present, empty when none.
	SixByClass map[string][]int `json:"six_by_class"`
}

// newToOwner has every list and every class present.
func newToOwner() *ToOwner {
	t := &ToOwner{Law: []int{}, Six: []int{}, Daily: []int{}, SixByClass: map[string][]int{}}
	for c := range approvals.SixClasses {
		t.SixByClass[c] = []int{}
	}
	return t
}

type Readings struct {
	AsOf string `json:"as_of"`
	Zone string `json:"zone"`
	// Retained is how many records the store still holds and Oldest the
	// creation time of the oldest: earlier days are not "no requests", they
	// are not kept.
	Retained int    `json:"retained"`
	Oldest   string `json:"oldest,omitempty"`
	Days     []Day  `json:"days"` // newest first
	// Open is every card not finished, with whose move it is and since when
	// (the Owner, 2026-11-21: what the system cannot see; Approvals list #4,
	// #6, #7). Oldest first. Drills are left out.
	Open []OpenCard `json:"open"`
	// WaitTimeouts are cards that were approved and failed waiting for a free
	// slot (exit 75 past its 30 minutes, "等空档超时"): approvals that bought
	// nothing because something else held the way (list #13).
	WaitTimeouts []int `json:"wait_timeouts"`
	// SixClasses is the definition the six counts use (code -> what it is),
	// so a page reads it instead of copying it.
	SixClasses map[string]string `json:"six_classes"`
}

// OpenCard is one unfinished card. Holder is whose move it is: "owner" (to
// decide), "executor" (approved, an executor is to claim it), "waiting" (an
// executor tried and was told "not now": Refusal says why, NextAttempt when
// it tries again), "running" (an executor holds it). Since is when it came to
// that holder; Minutes is how long ago that was.
type OpenCard struct {
	Number      int    `json:"number"`
	Action      string `json:"action"`
	Requester   string `json:"requester"`
	Runner      string `json:"runner,omitempty"`
	Holder      string `json:"holder"`
	Since       string `json:"since"`
	Minutes     int    `json:"minutes"`
	Refusal     string `json:"refusal,omitempty"`
	NextAttempt string `json:"next_attempt,omitempty"`
	Executor    string `json:"executor,omitempty"`
}

// openCard is the holder of an unfinished card, or false for a finished one.
func openCard(a approvals.Approval, now time.Time) (OpenCard, bool) {
	c := OpenCard{Number: a.Number, Action: a.Action, Requester: a.Requester, Runner: a.Runner}
	var since time.Time
	switch a.Status {
	case approvals.StatusPending:
		c.Holder, since = "owner", a.CreatedAt
	case approvals.StatusApproved:
		c.Holder, since = "executor", a.DecidedAt
		if e := a.Execution; e != nil && e.LastRefusal != "" && e.NextAttemptAt.After(now) {
			c.Holder, c.Refusal = "waiting", e.LastRefusal
			c.NextAttempt = e.NextAttemptAt.UTC().Format(time.RFC3339)
		}
	case approvals.StatusRunning:
		c.Holder, since = "running", a.DecidedAt
		if e := a.Execution; e != nil {
			c.Executor = e.ExecutorID
			if !e.StartedAt.IsZero() {
				since = e.StartedAt
			}
		}
	default:
		return OpenCard{}, false
	}
	if since.IsZero() {
		since = a.CreatedAt
	}
	c.Since = since.UTC().Format(time.RFC3339)
	if d := now.Sub(since); d > 0 {
		c.Minutes = int(d / time.Minute)
	}
	return c, true
}

func Compute(items []approvals.Approval, loc *time.Location, now time.Time) Readings {
	out := Readings{AsOf: now.UTC().Format(time.RFC3339), Zone: loc.String(), Retained: len(items), Days: []Day{},
		Open: []OpenCard{}, WaitTimeouts: []int{}, SixClasses: approvals.SixClasses}
	for _, a := range items {
		if a.Drill {
			continue
		}
		if c, ok := openCard(a, now); ok {
			out.Open = append(out.Open, c)
		}
		if a.Status == approvals.StatusFailed && strings.Contains(a.Error, "等空档超时") {
			out.WaitTimeouts = append(out.WaitTimeouts, a.Number)
		}
	}
	sort.Slice(out.Open, func(i, j int) bool {
		if out.Open[i].Since != out.Open[j].Since {
			return out.Open[i].Since < out.Open[j].Since
		}
		return out.Open[i].Number < out.Open[j].Number
	})
	sort.Ints(out.WaitTimeouts)
	type key struct{ day, requester string }
	groups := map[key]*Group{}
	seconds := map[key][]int{}
	newest := map[key]time.Time{}
	works := map[key]map[string]bool{}
	var oldest time.Time
	for _, a := range items {
		if a.CreatedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || a.CreatedAt.Before(oldest) {
			oldest = a.CreatedAt
		}
		k := key{a.CreatedAt.In(loc).Format("2006-01-02"), a.Requester}
		g := groups[k]
		if g == nil {
			g = &Group{Requester: a.Requester, WorkIDs: []string{}, Requests: []int{}, Pending: []int{}, Rejected: []int{}, Expired: []int{},
				Approved: []int{}, Executed: []int{}, Failed: []int{}, Unknown: []int{}, Waiting: []int{}, Drills: []int{}}
			groups[k], works[k] = g, map[string]bool{}
		}
		if a.RequesterThread != "" && !a.CreatedAt.Before(newest[k]) {
			g.Thread, newest[k] = a.RequesterThread, a.CreatedAt
		}
		if a.WorkID != "" {
			works[k][a.WorkID] = true
		}
		if a.Drill {
			g.Drills = append(g.Drills, a.Number)
			continue
		}
		g.Requests = append(g.Requests, a.Number)
		approved := func() {
			g.Approved = append(g.Approved, a.Number)
			if !a.DecidedAt.IsZero() && !a.DecidedAt.Before(a.CreatedAt) {
				seconds[k] = append(seconds[k], int(a.DecidedAt.Sub(a.CreatedAt).Seconds()))
			}
		}
		switch a.Status {
		case approvals.StatusPending:
			g.Pending = append(g.Pending, a.Number)
		case approvals.StatusRejected:
			g.Rejected = append(g.Rejected, a.Number)
		case approvals.StatusExpired:
			g.Expired = append(g.Expired, a.Number)
			// Approved and never run before its deadline is an expiry too, and still an approval.
			if a.ApprovedLine != "" {
				approved()
			}
		case approvals.StatusExecuted:
			approved()
			g.Executed = append(g.Executed, a.Number)
		case approvals.StatusFailed:
			approved()
			g.Failed = append(g.Failed, a.Number)
		case approvals.StatusUnknown:
			approved()
			g.Unknown = append(g.Unknown, a.Number)
		case approvals.StatusApproved, approvals.StatusRunning:
			approved()
			g.Waiting = append(g.Waiting, a.Number)
		}
	}
	if !oldest.IsZero() {
		out.Oldest = oldest.UTC().Format(time.RFC3339)
	}
	toOwner := map[string]*ToOwner{}
	for _, a := range items {
		if a.Drill || a.CreatedAt.IsZero() {
			continue
		}
		d := a.CreatedAt.In(loc).Format("2006-01-02")
		t := toOwner[d]
		if t == nil {
			t = newToOwner()
			toOwner[d] = t
		}
		switch {
		case approvals.TrackOf(a.Action) == approvals.TrackLaw:
			t.Law = append(t.Law, a.Number)
		case a.OwnerClass != "":
			t.Six = append(t.Six, a.Number)
			t.SixByClass[a.OwnerClass] = append(t.SixByClass[a.OwnerClass], a.Number)
		default:
			t.Daily = append(t.Daily, a.Number)
		}
	}
	days := map[string][]Group{}
	for k, g := range groups {
		for _, list := range []*[]int{&g.Requests, &g.Pending, &g.Rejected, &g.Expired, &g.Approved, &g.Executed, &g.Failed, &g.Unknown, &g.Waiting, &g.Drills} {
			sort.Ints(*list)
		}
		for w := range works[k] {
			g.WorkIDs = append(g.WorkIDs, w)
		}
		sort.Strings(g.WorkIDs)
		if s := seconds[k]; len(s) > 0 {
			sort.Ints(s)
			m := s[len(s)/2]
			if len(s)%2 == 0 {
				m = (s[len(s)/2-1] + s[len(s)/2]) / 2
			}
			g.ApproveSecondsMedian = &m
		}
		days[k.day] = append(days[k.day], *g)
	}
	for day, list := range days {
		sort.Slice(list, func(i, j int) bool {
			if len(list[i].Requests) != len(list[j].Requests) {
				return len(list[i].Requests) > len(list[j].Requests)
			}
			return list[i].Requester < list[j].Requester
		})
		t := toOwner[day]
		if t == nil {
			t = newToOwner()
		}
		for _, l := range t.SixByClass {
			sort.Ints(l)
		}
		sort.Ints(t.Law)
		sort.Ints(t.Six)
		sort.Ints(t.Daily)
		out.Days = append(out.Days, Day{Day: day, Groups: list, ToOwner: *t})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Day > out.Days[j].Day })
	return out
}
