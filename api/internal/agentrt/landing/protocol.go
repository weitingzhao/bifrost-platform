// Package landing defines the no-I/O landing contract. External actions belong
// to the workers controller; these records authorize no action by themselves.
package landing

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

type Record map[string]any

func (r Record) String(k string) string { s, _ := r[k].(string); return s }
func (r Record) Int(k string) int {
	b, _ := json.Marshal(r[k])
	var n int
	_ = json.Unmarshal(b, &n)
	return n
}
func (r Record) Time(k string) time.Time { t, _ := time.Parse(time.RFC3339Nano, r.String(k)); return t }

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)
var ident = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_:./-]{0,199}$`)
var task = regexp.MustCompile(`^t_[0-9A-HJKMNP-TV-Z]{26}$`)
var attempt = regexp.MustCompile(`^a_[0-9A-HJKMNP-TV-Z]{26}$`)
var digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var reasons = []string{"stale", "conflict", "patch_changed", "overlap", "ci_failed", "floor", "limit", "denied", "bound", "declined", "rule", "main_red", "ci_stuck", "window"}

// ci_seen includes the evidence needed by the final gate. stg_snapshot keeps
// retry backoff across restarts. These fields make §2.4's shorthand explicit.
var fields = map[string]string{
	"enabled": "repo rule_version", "claimed": "task attempt A rule_version",
	"prepared": "R M0 committer_date dry_run", "branch_intent": "ref R", "branch_pushed": "ref R",
	"ci_kick": "try", "ci_seen": "run status R created_at", "ci_rerun": "try",
	"main_intent": "R M0", "main_result": "outcome", "landed": "R", "reverted": "R by",
	"parked": "why retry_at", "unparked": "", "failed": "why", "reserved": "root",
	"stg_pending": "R", "stg_snapshot": "P U try not_before", "stg_started": "run try",
	"stg_result": "run outcome", "stg_verified": "P U digests", "stg_failed": "why",
}

// Parse accepts only a closed v1 object, normalizing Go integers and JSON
// numbers alike through the same canonical decoder used by event storage.
func Parse(v any) (Record, error) {
	if r, ok := v.(Record); ok {
		v = map[string]any(r)
	}
	b, err := canon.Encode(v)
	if err != nil {
		return nil, err
	}
	x, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	m, ok := x.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("landing must be an object")
	}
	r := Record(m)
	spec, ok := fields[r.String("kind")]
	if !ok {
		return nil, fmt.Errorf("unknown landing kind")
	}
	allowed := strings.Fields("v kind op " + spec)
	if len(m) != len(allowed) {
		return nil, fmt.Errorf("landing fields do not match %s", r.String("kind"))
	}
	for _, k := range allowed {
		v, ok := m[k]
		if !ok {
			return nil, fmt.Errorf("landing needs %s", k)
		}
		valid := false
		switch k {
		case "v", "rule_version", "try":
			n, ok := v.(json.Number)
			i, e := n.Int64()
			valid = ok && e == nil && i > 0 && i <= 1<<30
			if k == "v" {
				valid = valid && i == 1
			}
			if k == "try" {
				valid = valid && i <= 3
			}
		case "dry_run":
			_, valid = v.(bool)
		case "committer_date", "created_at", "retry_at", "not_before":
			s, ok := v.(string)
			t, e := time.Parse(time.RFC3339Nano, s)
			valid = ok && e == nil && !t.IsZero()
		case "task", "root":
			valid = task.MatchString(r.String(k))
		case "attempt":
			valid = attempt.MatchString(r.String(k))
		case "R":
			if r.String("kind") == "stg_pending" {
				a, ok := v.([]any)
				valid = ok && len(a) > 0
				seen := map[string]bool{}
				for _, x := range a {
					s, ok := x.(string)
					valid = valid && ok && sha.MatchString(s) && !seen[s]
					seen[s] = true
				}
			} else {
				valid = sha.MatchString(r.String(k))
			}
		case "A", "M0", "P", "U":
			valid = sha.MatchString(r.String(k))
		case "digests":
			a, ok := v.(map[string]any)
			valid = ok && len(a) == 3
			for _, key := range []string{"platform-api", "platform-workers", "platform-console"} {
				s, ok := a[key].(string)
				valid = valid && ok && digest.MatchString(s)
			}
		case "why":
			valid = slices.Contains(reasons, r.String(k))
		case "status":
			valid = slices.Contains([]string{"Missing", "Unknown", "Running", "Succeeded", "Failed"}, r.String(k))
		case "outcome":
			vals := []string{"ok", "non_ff", "denied", "unknown"}
			if r.String("kind") == "stg_result" {
				vals = []string{"ok", "failed"}
			}
			valid = slices.Contains(vals, r.String(k))
		case "ref":
			valid = regexp.MustCompile(`^refs/heads/land/(prod|stg)/t_[0-9A-HJKMNP-TV-Z]{26}/[1-9][0-9]*$`).MatchString(r.String(k))
		case "repo":
			valid = r.String(k) == "bifrost-platform"
		default:
			s, ok := v.(string)
			valid = ok && ident.MatchString(s)
		}
		if !valid {
			return nil, fmt.Errorf("invalid landing %s", k)
		}
	}
	return r, nil
}

type Entry struct {
	At     time.Time
	Record Record
}
type Operation struct {
	Phase, Resume, Outcome, Task, Attempt, A, R, M0, Ref string
	RuleVersion                                          int
	CommitterDate, RetryAt, LastAt                       time.Time
	Why                                                  string
	Digests                                              map[string]string
	KickedAt, RerunAt                                    time.Time
	ClaimedAt, PreparedAt                                time.Time
	DryRun, Kicked, Rerun, EverLanded, Reverted          bool
	CI                                                   Record
	P, U, Run                                            string
	Try                                                  int
	NotBefore                                            time.Time
	Pending                                              []string
}
type State struct {
	Operations     map[string]Operation
	EnabledAt      time.Time
	EnabledVersion int
	Reservations   map[string]string // root -> first operation; quota stream is one UTC day
}

func Fold(entries []Entry) (State, error) {
	s := State{Operations: map[string]Operation{}, Reservations: map[string]string{}}
	for _, e := range entries {
		if err := s.Apply(e); err != nil {
			return State{}, err
		}
	}
	return s, nil
}
func (s *State) Apply(e Entry) error {
	r, err := Parse(e.Record)
	if err != nil {
		return err
	}
	if e.At.IsZero() {
		return fmt.Errorf("landing event needs time")
	}
	if s.Operations == nil {
		s.Operations = map[string]Operation{}
	}
	if s.Reservations == nil {
		s.Reservations = map[string]string{}
	}
	k, op := r.String("kind"), r.String("op")
	if k == "enabled" {
		if !s.EnabledAt.IsZero() {
			return fmt.Errorf("landing already enabled")
		}
		s.EnabledAt = e.At
		s.EnabledVersion = r.Int("rule_version")
		return nil
	}
	if k == "reserved" {
		if _, ok := s.Reservations[r.String("root")]; ok {
			return fmt.Errorf("root already reserved")
		}
		s.Reservations[r.String("root")] = op
		return nil
	}
	old := s.Operations[op]
	n := old
	n.LastAt = e.At
	bad := func() error { return fmt.Errorf("landing %s: %s -> %s is illegal", op, old.Phase, k) }
	switch k {
	case "claimed":
		if old.Phase != "" {
			return bad()
		}
		n.Task = r.String("task")
		n.Attempt = r.String("attempt")
		n.A = r.String("A")
		n.RuleVersion = r.Int("rule_version")
		n.ClaimedAt = e.At
	case "prepared":
		if old.Phase != "claimed" {
			return bad()
		}
		n.R = r.String("R")
		n.M0 = r.String("M0")
		n.PreparedAt = e.At
		n.CommitterDate = r.Time("committer_date")
		n.DryRun = r["dry_run"].(bool)
	case "branch_intent", "branch_pushed":
		want := "prepared"
		if k == "branch_pushed" {
			want = "branch_intent"
		}
		if old.Phase != want || r.String("R") != old.R {
			return bad()
		}
		prefix := "refs/heads/land/prod/"
		if old.DryRun {
			prefix = "refs/heads/land/stg/"
		}
		if !strings.HasPrefix(r.String("ref"), prefix+old.Task+"/") || (k == "branch_pushed" && r.String("ref") != old.Ref) {
			return bad()
		}
		n.Ref = r.String("ref")
	case "ci_kick", "ci_rerun", "ci_seen":
		if old.Phase != "branch_pushed" {
			return bad()
		}
		if k == "ci_kick" {
			if old.Kicked || r.Int("try") != 1 {
				return bad()
			}
			n.Kicked = true
			n.KickedAt = e.At
		}
		if k == "ci_rerun" {
			if old.Rerun || r.Int("try") != 2 {
				return bad()
			}
			n.Rerun = true
			n.RerunAt = e.At
			n.CI = nil
		}
		if k == "ci_seen" {
			if r.String("R") != old.R || !r.Time("created_at").After(old.PreparedAt) || r.Time("created_at").After(e.At) {
				return bad()
			}
			if old.CI != nil && r.Time("created_at").Before(old.CI.Time("created_at")) {
				return bad()
			}
			n.CI = r
		}
		n.Phase = old.Phase
		s.Operations[op] = n
		return nil
	case "main_intent":
		if old.Phase != "branch_pushed" || old.DryRun || old.CI.String("status") != "Succeeded" || r.String("R") != old.R || r.String("M0") != old.M0 {
			return bad()
		}
	case "main_result":
		if old.Phase != "main_intent" && !(old.Phase == "main_result" && old.Outcome == "unknown" && r.String("outcome") == "non_ff") {
			return bad()
		}
		n.Outcome = r.String("outcome")
	case "landed":
		if old.DryRun || old.Phase != "main_result" || !slices.Contains([]string{"ok", "unknown"}, old.Outcome) || r.String("R") != old.R {
			return bad()
		}
		n.EverLanded = true
	case "reverted":
		if old.Phase != "landed" || r.String("R") != old.R {
			return bad()
		}
		n.Reverted = true
	case "parked":
		if !slices.Contains([]string{"claimed", "prepared", "branch_pushed"}, old.Phase) {
			return bad()
		}
		n.Resume = old.Phase
		n.Why = r.String("why")
		n.RetryAt = r.Time("retry_at")
	case "unparked":
		if old.Phase != "parked" {
			return bad()
		}
		n.Phase = old.Resume
		n.Resume = ""
		n.Why = ""
		n.RetryAt = time.Time{}
		s.Operations[op] = n
		return nil
	case "failed":
		allowed := slices.Contains([]string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked"}, old.Phase)
		// §2.4 recovery explicitly ends an unknown push not found on unchanged main.
		allowed = allowed || old.Phase == "main_result" && ((old.Outcome == "denied" && r.String("why") == "denied") || (old.Outcome == "unknown" && r.String("why") == "bound"))
		if !allowed {
			return bad()
		}
		n.Outcome = r.String("why")
	case "stg_pending":
		if old.Phase != "" {
			return bad()
		}
		for _, v := range r["R"].([]any) {
			n.Pending = append(n.Pending, v.(string))
		}
	case "stg_snapshot":
		if old.Phase != "" && old.Phase != "stg_pending" && !(old.Phase == "stg_result" && old.Outcome == "failed") {
			return bad()
		}
		if r.Int("try") != old.Try+1 || r.Int("try") > 3 {
			return bad()
		}
		if old.Try > 0 && (r.String("P") != old.P || r.String("U") != old.U || r.Time("not_before").Before(e.At.Add(stgBackoff(old.Try)))) {
			return bad()
		}
		n.P = r.String("P")
		n.U = r.String("U")
		n.Try = r.Int("try")
		n.NotBefore = r.Time("not_before")
		n.Run = ""
		n.Outcome = ""
	case "stg_started":
		if old.Phase != "stg_snapshot" || r.Int("try") != old.Try || e.At.Before(old.NotBefore) {
			return bad()
		}
		n.Run = r.String("run")
	case "stg_result":
		if old.Phase != "stg_started" || r.String("run") != old.Run {
			return bad()
		}
		n.Outcome = r.String("outcome")
	case "stg_verified":
		if old.Phase != "stg_result" || old.Outcome != "ok" || r.String("P") != old.P || r.String("U") != old.U {
			return bad()
		}
		n.Digests = map[string]string{}
		for key, value := range r["digests"].(map[string]any) {
			n.Digests[key] = value.(string)
		}
	case "stg_failed":
		if old.Phase != "stg_result" {
			return bad()
		}
		n.Outcome = r.String("why")
	default:
		return bad()
	}
	n.Phase = k
	s.Operations[op] = n
	return nil
}
func stgBackoff(previousTry int) time.Duration {
	if previousTry == 1 {
		return 10 * time.Minute
	}
	return 30 * time.Minute
}
func (o Operation) Terminal() bool {
	return slices.Contains([]string{"landed", "reverted", "failed", "stg_verified", "stg_failed"}, o.Phase) || o.Phase == "main_result" && o.Outcome == "non_ff"
}
func (o Operation) Active() bool { return o.Phase != "" && o.Phase != "parked" && !o.Terminal() }

// Bound includes all elapsed time, including window and main-red waits.
func Bound(first time.Time, attempts int, now time.Time) bool {
	return !first.IsZero() && (!now.Before(first.Add(6*time.Hour)) || attempts >= 8)
}

// BlocksClaim is a root-task terminal result. A non-FF finishes only this
// operation: the next operation must still be able to replay on a new main.
func (o Operation) BlocksClaim() bool { return o.EverLanded || o.Phase == "failed" }

// Reservable is checked against the repository state while reserving in the
// tenant/day stream. A rehearsal never consumes the production quota.
func (o Operation) Reservable(root string) bool {
	return o.Task == root && !o.DryRun && o.Phase == "branch_pushed"
}
