package planapi

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

type ownerIssuesBody struct {
	Issues []planstore.OwnerIssue `json:"issues"`
}

// HandlePostOwnerIssues is POST /owner-issues {issues}: the legislation line
// records the issues it drew from what the Owner said (Owner 2026-11-25: his
// complaints, pains and needs go here); an issue that did not change records
// nothing, so the whole list may be posted again. Answers {recorded}.
func (rt Routes) HandlePostOwnerIssues(w http.ResponseWriter, r *http.Request) {
	var b ownerIssuesBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	who, err := rt.Host.Writer(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	n, err := planstore.RecordOwnerIssues(r.Context(), rt.Host.DB(), who, b.Issues)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"recorded": n})
}

// HandleGetOwnerIssues is GET /owner-issues[?to=<line or domain>]: every
// issue as it stands, the most often said first, with how many go each way and
// how many a law answers whose pain came back; with to, only the issues routed
// to that line or domain (Owner 2026-11-25: every agent reads the complaints in
// its own business). Read-only.
func (rt Routes) HandleGetOwnerIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := planstore.OwnerIssues(r.Context(), rt.Host.DB())
	if err != nil {
		rt.Host.Logf("owner-issues: %v", err)
		rt.Host.Error(w, http.StatusServiceUnavailable, "unavailable", "the Owner's issues are unavailable")
		return
	}
	if to := r.URL.Query().Get("to"); to != "" {
		mine := issues[:0]
		for _, m := range issues {
			if m.To == to {
				mine = append(mine, m)
			}
		}
		issues = mine
	}
	if issues == nil {
		issues = []planstore.OwnerIssueRead{}
	}
	byAction, byStatus, cameBack := map[string]int{}, map[string]int{}, 0
	for _, m := range issues {
		byAction[m.Action]++
		byStatus[m.Status]++
		if m.SaidAfterLaw != nil && *m.SaidAfterLaw > 0 {
			cameBack++
		}
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"issues": issues, "total": len(issues),
		"by_action": byAction, "by_status": byStatus, "came_back_after_law": cameBack})
}
