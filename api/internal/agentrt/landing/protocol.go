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

// Version is a string because canonical event JSON forbids fractional numbers.
const Version = "1.1"

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
var reasons = []string{"stale", "conflict", "patch_changed", "overlap", "ci_failed", "ci_red", "floor", "limit", "denied", "bound", "declined", "rule", "main_red", "ci_stuck", "window"}

// ci_seen includes the evidence needed by the final gate. stg_snapshot keeps
// retry backoff across restarts. These fields make §2.4's shorthand explicit.
var fields = map[string]string{
	"enabled": "repo rule_version", "claimed": "task attempt A rule_version",
	"prepared": "R M0 committer_date dry_run", "branch_intent": "ref R", "branch_pushed": "ref R",
	"ci_kick": "try", "ci_seen": "run status R created_at", "ci_rerun": "try",
	"main_intent": "R M0", "main_result": "outcome", "landed": "R", "reverted": "R by",
	"parked": "why retry_at", "unparked": "", "failed": "why", "reserved": "root",
	"discarded": "why", "rehearsed": "R", "retry": "root after_op reason by",
	"stg_pending": "R", "stg_snapshot": "P U try not_before", "stg_started": "run try",
	"stg_result": "run outcome", "stg_verified": "P U digests", "stg_failed": "why",
}

// Parse accepts closed v1 and v1.1 objects, normalizing Go integers and JSON
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
	if r.String("v") != Version && slices.Contains([]string{"discarded", "rehearsed", "retry"}, r.String("kind")) {
		return nil, fmt.Errorf("landing kind requires v1.1")
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
		case "v":
			n, ok := v.(json.Number)
			valid = (ok && n.String() == "1") || r.String("v") == Version
		case "rule_version", "try":
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
		case "reason":
			valid = len(r.String(k)) > 0 && len(r.String(k)) <= 1000
		case "why":
			valid = slices.Contains(reasons, r.String(k))
			if r.String("kind") == "discarded" {
				valid = slices.Contains([]string{"main_changed", "branch_collision"}, r.String(k))
			}
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
	ClaimNumber                                          int
	Retried                                              bool
	NotBefore                                            time.Time
	Pending                                              []string
}
type Retry struct {
	At                  time.Time
	AfterOp, ConsumedBy string
}
type State struct {
	Retries        map[string]Retry
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
	if s.Retries == nil {
		s.Retries = map[string]Retry{}
	}
	k, op := r.String("kind"), r.String("op")
	if k == "retry" {
		root, after := r.String("root"), r.String("after_op")
		o, exists := s.Operations[after]
		if !exists || op != after || o.Task != root || s.LatestOperation(root) != after || o.Phase != "failed" || o.Retried || !slices.Contains([]string{"bound", "stale", "ci_red"}, o.Outcome) {
			return fmt.Errorf("retry requires latest retryable failure")
		}
		o.Retried = true
		s.Operations[after] = o
		s.Retries[root] = Retry{At: e.At, AfterOp: after}
		return nil
	}
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
		for _, existing := range s.Operations {
			if existing.Task == n.Task {
				n.ClaimNumber++
			}
		}
		n.ClaimNumber++
		if r.String("v") == Version {
			for _, existing := range s.Operations {
				if existing.Task == n.Task && (existing.BlocksClaim() || !existing.Terminal()) {
					return bad()
				}
			}
		}
		if retry, ok := s.Retries[n.Task]; ok && retry.ConsumedBy == "" {
			retry.ConsumedBy = op
			s.Retries[n.Task] = retry
		}
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
	case "discarded":
		if !slices.Contains([]string{"branch_intent", "branch_pushed", "parked"}, old.Phase) {
			return bad()
		}
		n.Outcome = r.String("why")
	case "rehearsed":
		if old.Phase != "branch_pushed" || !old.DryRun || r.String("R") != old.R || old.CI.String("status") != "Succeeded" || !rerunObserved(op, old, old.CI.String("run"), old.CI.Time("created_at")) {
			return bad()
		}
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
		if old.Phase == "" {
			// A crash may leave discarded as the last record until the root
			// budget expires. Its successor is born terminal in one event;
			// the discarded operation itself remains immutable.
			if r.String("v") != Version || r.String("why") != "bound" {
				return bad()
			}
			for id, previous := range s.Operations {
				first, count := RootBudget(*s, previous.Task)
				if s.LatestOperation(previous.Task) == id && s.AwaitsNext(previous.Task) &&
					op == fmt.Sprintf("land:%s:%d", previous.Task, count+1) && Bound(first, count, e.At) {
					n.Task, n.Attempt, n.A = previous.Task, previous.Attempt, previous.A
					n.RuleVersion, n.DryRun = previous.RuleVersion, previous.DryRun
					n.ClaimNumber, n.ClaimedAt = count+1, e.At
					// A retry this ends is used up, as a claim would use it.
					if retry, ok := s.Retries[n.Task]; ok && retry.ConsumedBy == "" {
						retry.ConsumedBy = op
						s.Retries[n.Task] = retry
					}
					break
				}
			}
			if n.Task == "" {
				return bad()
			}
			n.Phase, n.Outcome = "failed", "bound"
			s.Operations[op] = n
			return nil
		}
		allowed := old.EndsAtBound()
		// §2.4 recovery explicitly ends an unknown push not found on unchanged main.
		allowed = allowed || old.Phase == "main_result" && ((old.Outcome == "denied" && r.String("why") == "denied") || (old.Outcome == "unknown" && r.String("why") == "bound"))
		allowed = allowed || r.String("v") == Version && old.Phase == "failed" && old.Outcome != "declined" && r.String("why") == "declined" && s.LatestOperation(old.Task) == op
		if !allowed {
			return bad()
		}
		n.Retried = false
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
	return slices.Contains([]string{"landed", "reverted", "failed", "discarded", "rehearsed", "stg_verified", "stg_failed"}, o.Phase) || o.Phase == "main_result" && o.Outcome == "non_ff"
}

// EndsAtBound: a live operation that has not reached main may end
// failed{bound} from the stream alone (§5 item 6). From main_intent on, the
// end depends on what main holds, which only a Git read can tell (§2.4).
func (o Operation) EndsAtBound() bool {
	return slices.Contains([]string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked"}, o.Phase)
}

// AtMain: the operation asked main to move and its outcome is not settled
// (main_intent, or a main_result other than non_ff). Neither a time limit nor
// failed{bound} ends it; reconciling with main does (TD-447 2).
func (o Operation) AtMain() bool {
	return o.Phase == "main_intent" || o.Phase == "main_result" && o.Outcome != "non_ff"
}

func (o Operation) Active() bool { return o.Phase != "" && o.Phase != "parked" && !o.Terminal() }

// Bound includes all elapsed time, including window and main-red waits.
func Bound(first time.Time, attempts int, now time.Time) bool {
	return !first.IsZero() && (!now.Before(first.Add(6*time.Hour)) || attempts >= 8)
}

// BlocksClaim is a root-task terminal result. A non-FF finishes only this
// operation: the next operation must still be able to replay on a new main.
func (o Operation) BlocksClaim() bool { return o.EverLanded || o.Phase == "failed" && !o.Retried }

// Reservable is checked against the repository state while reserving in the
// tenant/day stream. A rehearsal never consumes the production quota.
func (o Operation) Reservable(root string) bool {
	return o.Task == root && !o.DryRun && o.Phase == "branch_pushed"
}

// AwaitsNext: the root's latest operation ended without ending the root, so
// its next operation k+1 is due: a replay after discarded, a push main did
// not fast-forward (main_result{non_ff}), or a failure the Owner retried and
// no claim has used yet. Close, the drill scan and the bound settlement read
// it alike (TD-447 1, 6).
func (s State) AwaitsNext(root string) bool {
	o, ok := s.Operations[s.LatestOperation(root)]
	return ok && o.Terminal() && !o.BlocksClaim() && o.Phase != "rehearsed"
}

// RootPending: the root still has a step to take: its latest operation is
// live, or AwaitsNext. False for a root never claimed.
func (s State) RootPending(root string) bool {
	o, ok := s.Operations[s.LatestOperation(root)]
	return ok && (!o.Terminal() || s.AwaitsNext(root))
}

// LatestOperation uses stream claim order, including equal event timestamps.
func (s State) LatestOperation(root string) string {
	var latest string
	var number int
	for id, o := range s.Operations {
		if o.Task == root && o.ClaimNumber > number {
			latest, number = id, o.ClaimNumber
		}
	}
	return latest
}
