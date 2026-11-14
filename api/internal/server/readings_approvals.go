package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
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
// (store.BriefZone). "Approved" is decided and not rejected. A request
// approved and then failed, or left with a result nobody could read
// (unknown), is an approval that bought no change: failed and unknown list
// those. Whether an approval should not have been asked for at all is not in
// the records and is not counted here.

type approvalReadingGroup struct {
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

type approvalReadingDay struct {
	Day    string                 `json:"day"`
	Groups []approvalReadingGroup `json:"groups"`
}

type approvalReadings struct {
	AsOf string `json:"as_of"`
	Zone string `json:"zone"`
	// Retained is how many records the store still holds and Oldest the
	// creation time of the oldest: earlier days are not "no requests", they
	// are not kept.
	Retained int                  `json:"retained"`
	Oldest   string               `json:"oldest,omitempty"`
	Days     []approvalReadingDay `json:"days"` // newest first
}

func readApprovals(items []approvals.Approval, loc *time.Location, now time.Time) approvalReadings {
	out := approvalReadings{AsOf: now.UTC().Format(time.RFC3339), Zone: loc.String(), Retained: len(items), Days: []approvalReadingDay{}}
	type key struct{ day, requester string }
	groups := map[key]*approvalReadingGroup{}
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
			g = &approvalReadingGroup{Requester: a.Requester, WorkIDs: []string{}, Requests: []int{}, Pending: []int{}, Rejected: []int{}, Expired: []int{},
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
	days := map[string][]approvalReadingGroup{}
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
		out.Days = append(out.Days, approvalReadingDay{Day: day, Groups: list})
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].Day > out.Days[j].Day })
	return out
}

func (s *Server) handleApprovalReadings(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform keeps no approval records"})
		return
	}
	loc, err := time.LoadLocation(store.BriefZone)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.approvals.Observations()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the approval records could not be read: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, readApprovals(items, loc, time.Now()))
}
