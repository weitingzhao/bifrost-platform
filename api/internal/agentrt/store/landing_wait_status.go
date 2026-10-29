package store

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// LandingWaitStatus is read evidence for the API. OwnerTodo is true only for
// a pending request whose M0 a fresh main read still confirms. An approved or
// running approval alone never displays "authorized" (only a grant can).
type LandingWaitStatus struct {
	Phase            string    `json:"phase"`
	Shown            string    `json:"shown"`
	Since            time.Time `json:"since"`
	Soft             []string  `json:"soft_at_request"`
	Files            int       `json:"files"`
	Lines            int       `json:"lines"`
	Snapshot         string    `json:"waiting_snapshot"`
	R                string    `json:"R"`
	M0               string    `json:"M0"`
	ExecutionSeconds int64     `json:"execution_seconds"`
	WaitingSeconds   int64     `json:"waiting_seconds"`
	Operations       int       `json:"operations"`
	ApprovalID       string    `json:"approval_id,omitempty"`
	ApprovalNumber   int64     `json:"approval_number,omitempty"`
	ApprovalStatus   string    `json:"approval_status,omitempty"`
	ApprovalURL      string    `json:"approval_url,omitempty"`
	MainKnown        bool      `json:"main_known"`
	MainChanged      bool      `json:"main_changed"`
	OwnerTodo        bool      `json:"owner_todo"`
	// Grants are the operation's grants as the stream has them (C-5);
	// Shown is "authorized" while one is valid.
	Grants []LandingGrantStatus `json:"grants"`
	// GrantRefused is why the final gate spent the latest grant
	// (grant_insufficient: the rule changed, needs a new approval;
	// grant_stale: the candidate's evidence changed).
	GrantRefused string `json:"grant_refused,omitempty"`
}

func (s *LandingWaitStatus) ObserveMain(head string) {
	s.MainKnown = landing.ValidSHA(head)
	s.MainChanged = s.MainKnown && head != s.M0
	s.OwnerTodo = s.Phase == "awaiting_grant" && s.ApprovalStatus == "pending" && s.MainKnown && !s.MainChanged && s.Shown != "authorized"
	if s.Phase != "awaiting_grant" || s.ApprovalStatus != "pending" || s.Shown == "authorized" {
		return
	}
	s.Shown = "request_main_unknown"
	if s.MainKnown {
		s.Shown = "awaiting_owner"
	}
	if s.MainChanged {
		s.Shown = "request_stale"
	}
}

func readLandingWaitStatus(ctx context.Context, tx pgx.Tx, state landing.State, op string, at time.Time) (*LandingWaitStatus, error) {
	o := state.Operations[op]
	if o.AwaitingAt.IsZero() {
		return nil, nil
	}
	clock := landing.RootTiming(state, o.Task, at)
	w := &LandingWaitStatus{Phase: o.Phase, Since: o.AwaitingAt, Soft: slices.Clone(o.Soft), Files: o.Files, Lines: o.Lines,
		Snapshot: o.Material, R: o.R, M0: o.M0, ExecutionSeconds: int64(clock.Execution / time.Second), WaitingSeconds: int64(clock.Waiting / time.Second), Operations: clock.Operations}
	rows, err := tx.Query(ctx, `SELECT body_json->'landing_grant_request' FROM agentrt.event
	  WHERE event_stream_id='landing:bifrost-platform' AND body_json->'landing_grant_request'->>'op'=$1
	  ORDER BY seq DESC`, op)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Keep every distinct observation in the audit stream, but project each
	// approval's progress independently of arrival time (including seen_at,
	// which is sampled after reading the approval snapshot).
	var ids []string
	current := map[string]map[string]any{}
	for rows.Next() {
		var m map[string]any
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		id, _ := m["approval_id"].(string)
		if current[id] == nil {
			ids = append(ids, id)
		}
		if current[id] == nil || landingRequestProgress(m) > landingRequestProgress(current[id]) {
			current[id] = m
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	var picked map[string]any
	for _, id := range ids {
		m := current[id]
		if picked == nil {
			picked = m
		}
		if status, _ := m["status"].(string); slices.Contains([]string{"pending", "approved", "running"}, status) {
			picked = m
			break
		}
	}
	if picked != nil {
		w.ApprovalID, _ = picked["approval_id"].(string)
		w.ApprovalStatus, _ = picked["status"].(string)
		w.ApprovalNumber, _ = cardNumber(picked["approval_number"])
		w.ApprovalURL = "/api/v1/approvals/" + w.ApprovalID
	}
	_, grants, err := readLandingGrants(ctx, tx, landingRepoStream)
	if err != nil {
		return nil, err
	}
	authorized := false
	var refused *LandingGrantStatus
	w.Grants = []LandingGrantStatus{}
	for _, g := range grants {
		if g.Op == op {
			st := g.status(at, o, state.LatestOperation(o.Task) == op)
			w.Grants = append(w.Grants, st)
			authorized = authorized || st.Valid
			if st.Refused {
				refused = &w.Grants[len(w.Grants)-1]
			}
		}
	}
	switch {
	case o.AtMain():
		w.Shown = "handed_to_push"
	case o.Phase == "awaiting_grant" && authorized:
		w.Shown = "authorized"
	case o.Phase == "awaiting_grant" && refused != nil && w.ApprovalStatus != "pending":
		// review 694 R-05: the gate refused the grant for good (the rule
		// now fails more, or the candidate changed): a new request is due.
		w.Shown, w.GrantRefused = "grant_refused", refused.RefusedReason
	case o.Phase == "awaiting_grant":
		w.Shown = "awaiting_request"
		if slices.Contains([]string{"approved", "running"}, w.ApprovalStatus) {
			w.Shown = "approval_observed"
		}
		w.ObserveMain("")
	default:
		w.Shown = o.Phase
	}
	return w, nil
}

// Observed progress, not an execution grant: pending -> approved -> running
// -> unknown -> a definitive outcome. A not-started runner can requeue to
// approved; retaining running in that case is deliberately conservative and
// has the same open-request display. Unknown can only resolve to a late
// executed/failed result; it must never resurrect a pending Owner task.
func landingRequestProgress(m map[string]any) int {
	switch m["status"] {
	case "pending":
		return 1
	case "approved":
		return 2
	case "running":
		return 3
	case "unknown":
		return 4
	case "executed", "failed", "rejected", "expired":
		return 5
	}
	return 0
}

// ReadLandingWait reads one operation with no external I/O or writes. Main
// evidence is attached by the HTTP handler after this transaction closes.
func ReadLandingWait(ctx context.Context, db DB, op string) (*LandingWaitStatus, error) {
	var out *LandingWaitStatus
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		s, err := LandingState(ctx, tx, "landing:bifrost-platform")
		if err != nil {
			return err
		}
		out, err = readLandingWaitStatus(ctx, tx, s, op, DecisionAt())
		return err
	})
	return out, err
}
