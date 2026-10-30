// Package landing defines the no-I/O landing contract. External actions belong
// to the workers controller; these records authorize no action by themselves.
package landing

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Version is a string because canonical event JSON forbids fractional numbers.
const Version = "1.2"

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
var reasons = []string{"stale", "conflict", "patch_changed", "overlap", "ci_failed", "ci_red", "floor", "limit", "denied", "bound", "declined", "rule", "main_red", "ci_stuck", "window", "main_lost"}

// ci_seen includes the evidence needed by the final gate. stg_snapshot keeps
// retry backoff across restarts. These fields make §2.4's shorthand explicit.
var fields = map[string]string{
	"enabled": "repo rule_version", "stg_enabled": "repo rule_version", "mode": "repo", "claimed": "task attempt A rule_version",
	"prepared": "R M0 committer_date dry_run", "branch_intent": "ref R", "branch_pushed": "ref R",
	"ci_kick": "try", "ci_seen": "run status R created_at", "ci_rerun": "try",
	"awaiting_grant": "soft files lines material",
	"main_intent":    "R M0", "main_result": "outcome", "landed": "R", "reverted": "R by",
	"parked": "why retry_at", "unparked": "", "failed": "why", "reserved": "root",
	"discarded": "why", "rehearsed": "R", "retry": "root after_op reason by",
	"stg_pending": "R", "stg_snapshot": "P U try not_before", "stg_started": "run try",
	"stg_result": "run outcome", "stg_verified": "P U digests", "stg_failed": "why",
}

// Parse accepts closed v1, v1.1 and v1.2 objects, normalizing Go integers and JSON
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
	version := recordVersion(r)
	if version == 0 || version < minimumVersion[r.String("kind")] {
		return nil, fmt.Errorf("unsupported landing version for %s", r.String("kind"))
	}
	allowed := strings.Fields("v kind op " + spec)
	// S6 extends only infra preparation/evidence. The old shapes remain
	// byte-for-byte valid; repository ownership is enforced at append time.
	if version >= 3 && r.String("kind") == "prepared" && m["platform_revision"] != nil {
		allowed = append(allowed, "platform_revision", "generation")
	}
	if version >= 3 && r.String("kind") == "ci_seen" && m["results"] != nil {
		allowed = append(allowed, "uid", "results")
	}
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
			valid = (ok && n.String() == "1") || slices.Contains([]string{"1.1", "1.2"}, r.String("v"))
		case "rule_version", "try", "generation":
			n, ok := v.(json.Number)
			i, e := n.Int64()
			valid = ok && e == nil && i > 0 && i <= 1<<30
			if k == "v" {
				valid = valid && i == 1
			}
			if k == "try" {
				valid = valid && i <= 3
			}
		case "files", "lines":
			n, ok := v.(json.Number)
			i, e := n.Int64()
			valid = ok && e == nil && i >= 0
		case "soft":
			valid = validSoft(v)
		case "material":
			valid = digest.MatchString(r.String(k))
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
		case "A", "M0", "P", "U", "platform_revision":
			valid = sha.MatchString(r.String(k))
		case "uid":
			_, valid = v.(string)
			valid = valid && len(r.String(k)) <= 200 && (r.String("status") != "Succeeded" || r.String(k) != "")
		case "results":
			valid = validCIResults(v)
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
			valid = slices.Contains(reasons, r.String(k)) || (r.String(k) == "approval_expired" && version >= 3)
			if r.String("kind") == "discarded" {
				valid = slices.Contains([]string{"main_changed", "branch_collision"}, r.String(k))
			}
			// result and history end only a root the claim refused before
			// it was ever claimed (Apply checks that; TD-447 4).
			if r.String("kind") == "failed" && slices.Contains(ClaimRefusals, r.String(k)) {
				valid = true
			}
			// main_lost ends only a failed, v1.1 record (CUT §10.6 1A,
			// review 658 B3); Apply limits where it comes from.
			if r.String(k) == "main_lost" && (r.String("kind") != "failed" || version < 2) {
				valid = false
			}
			if r.String(k) == "approval_expired" && (r.String("kind") != "failed" || version < 3) {
				valid = false
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
			valid = RepoSupported(r.String(k))
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
	// Live: born after the repository's live mode (State.LiveAt).
	Live                                                 bool
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
	// Waiting is fixed by the stream. GrantPeriods is a projection of valid
	// grants supplied by the grant reader (C-5), never an approval status.
	AwaitingAt, WaitEndedAt time.Time
	Soft                    []string
	Files, Lines            int
	Material                string
	GrantPeriods            []Interval
	// Infra's sibling revision and generation are fixed with prepared.
	PlatformRevision string
	Generation       int
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
	STGEnabledAt   time.Time
	// LiveAt is when the live landing was first wired for this repository
	// (CUT §11.8 v3 item 1): only a PROD stream has it. Every operation
	// born after it is live.
	LiveAt       time.Time
	Reservations map[string]string // root -> first operation; quota stream is one UTC day
	// Grants is the grant projection (C-5): per operation, the intervals a
	// valid grant was available, from the stream's grant records. It is set
	// before the fold, so every record is checked against the same W that
	// was in force when it was appended (intervals are clipped to time).
	Grants map[string][]Interval
}

func Fold(entries []Entry) (State, error) { return FoldWithGrants(entries, nil) }

// FoldWithGrants folds entries with the grant projection in place (C-5).
func FoldWithGrants(entries []Entry, grants map[string][]Interval) (State, error) {
	s := State{Operations: map[string]Operation{}, Reservations: map[string]string{}, Grants: grants}
	for _, e := range entries {
		if err := s.Apply(e); err != nil {
			return State{}, err
		}
	}
	return s, nil
}

// Check reports whether e would apply to s, without changing s: an answer
// decides before it appends whether the protocol takes its record, so a
// refusal is recorded instead of rolling the answer back (CUT §8).
func (s State) Check(e Entry) error {
	c := State{EnabledAt: s.EnabledAt, EnabledVersion: s.EnabledVersion, STGEnabledAt: s.STGEnabledAt, LiveAt: s.LiveAt, Grants: s.Grants,
		Retries: maps.Clone(s.Retries), Reservations: maps.Clone(s.Reservations), Operations: make(map[string]Operation, len(s.Operations))}
	for k, o := range s.Operations {
		o.Digests = maps.Clone(o.Digests)
		c.Operations[k] = o
	}
	return c.Apply(e)
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
		if !exists || op != after || o.Task != root || s.LatestOperation(root) != after || o.Phase != "failed" || o.Retried || !slices.Contains(RetryableReasons, o.Outcome) {
			return fmt.Errorf("retry requires latest retryable failure")
		}
		o.Retried = true
		s.Operations[after] = o
		s.Retries[root] = Retry{At: e.At, AfterOp: after}
		return nil
	}
	if k == "mode" {
		if recordVersion(r) < 2 || !s.LiveAt.IsZero() {
			return fmt.Errorf("landing mode recorded once, at v%s", Version)
		}
		s.LiveAt = e.At
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
	if k == "stg_enabled" {
		if !s.STGEnabledAt.IsZero() || r.String("repo") != "bifrost-platform" {
			return fmt.Errorf("STG release already enabled or unsupported repository")
		}
		s.STGEnabledAt = e.At
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
	if old.Phase == "" {
		n.Live = !s.LiveAt.IsZero() // born after the live mode: a live operation
	}
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
		if recordVersion(r) >= 2 {
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
		n.PlatformRevision, n.Generation = r.String("platform_revision"), r.Int("generation")
		if n.Generation != 0 && n.Generation != n.ClaimNumber {
			return bad()
		}
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
		if old.Phase != "branch_pushed" && !(old.Phase == "awaiting_grant" && k == "ci_seen") {
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
		if !slices.Contains([]string{"branch_intent", "branch_pushed", "parked", "awaiting_grant"}, old.Phase) {
			return bad()
		}
		if old.Phase == "awaiting_grant" && r.String("why") != "main_changed" {
			return bad()
		}
		n.Outcome = r.String("why")
	case "awaiting_grant":
		if old.Phase != "branch_pushed" || old.DryRun || old.R == "" || old.M0 == "" || old.CI.String("status") != "Succeeded" {
			return bad()
		}
		n.AwaitingAt = e.At
		n.Soft = nil
		for _, x := range r["soft"].([]any) {
			n.Soft = append(n.Soft, x.(string))
		}
		n.Files, n.Lines, n.Material = r.Int("files"), r.Int("lines"), r.String("material")
	case "rehearsed":
		if old.Phase != "branch_pushed" || !old.DryRun || r.String("R") != old.R || old.CI.String("status") != "Succeeded" || !rerunObserved(op, old, old.CI.String("run"), old.CI.Time("created_at")) {
			return bad()
		}
	case "main_intent":
		if (old.Phase != "branch_pushed" && old.Phase != "awaiting_grant") || old.DryRun || old.CI.String("status") != "Succeeded" || r.String("R") != old.R || r.String("M0") != old.M0 {
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
			if recordVersion(r) < 2 {
				return bad()
			}
			// A root never claimed that the claim refused for good, or whose
			// claim window ran out, ends in one event: land:<root>:1, born
			// failed (TD-447 4). Close then releases its mission on the
			// record instead of a timer.
			if root, ok := neverClaimed(*s, op); ok {
				if !slices.Contains(append([]string{"stale", "bound", "floor"}, ClaimRefusals...), r.String("why")) {
					return bad()
				}
				n.Task, n.ClaimNumber, n.ClaimedAt = root, 1, e.At
				n.Phase, n.Outcome = "failed", r.String("why")
				s.Operations[op] = n
				return nil
			}
			// A crash may leave discarded as the last record until the root
			// budget expires. Its successor is born terminal in one event;
			// the discarded operation itself remains immutable.
			if !slices.Contains([]string{"bound", "approval_expired"}, r.String("why")) {
				return bad()
			}
			for id, previous := range s.Operations {
				_, count := RootBudget(*s, previous.Task)
				if s.LatestOperation(previous.Task) == id && s.AwaitsNext(previous.Task) &&
					op == fmt.Sprintf("land:%s:%d", previous.Task, count+1) && RootBound(*s, previous.Task, e.At, true) == r.String("why") {
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
			n.Phase, n.Outcome = "failed", r.String("why")
			s.Operations[op] = n
			return nil
		}
		if old.Phase == "awaiting_grant" && !slices.Contains([]string{"declined", "approval_expired", "bound", "stale"}, r.String("why")) {
			return bad()
		}
		if r.String("why") == "approval_expired" && RootTiming(*s, old.Task, e.At).Waiting < WaitWindow {
			return bad()
		}
		if slices.Contains(ClaimRefusals, r.String("why")) {
			return bad()
		}
		// main_lost comes from main_result{ok} alone, never from an
		// operation that a general failure may end (review 658 B3).
		allowed := old.EndsAtBound() && r.String("why") != "main_lost"
		// §2.4 recovery explicitly ends an unknown push not found on unchanged main.
		allowed = allowed || old.Phase == "main_result" && ((old.Outcome == "denied" && r.String("why") == "denied") || (old.Outcome == "unknown" && r.String("why") == "bound"))
		allowed = allowed || recordVersion(r) >= 2 && old.Phase == "failed" && old.Outcome != "declined" && r.String("why") == "declined" && s.LatestOperation(old.Task) == op
		// CUT §10.6 (1A): a push reported ok whose R main no longer shows
		// ends only when the Owner confirms it is lost.
		allowed = allowed || recordVersion(r) >= 2 && old.Phase == "main_result" && old.Outcome == "ok" && r.String("why") == "main_lost" && s.LatestOperation(old.Task) == op
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
	if old.Phase == "awaiting_grant" && k != "ci_seen" {
		n.WaitEndedAt = e.At
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
// end depends on what main holds (§2.4): main_result{unknown} may take
// failed{bound} only after a main read, denied ends failed{denied}.
func (o Operation) EndsAtBound() bool {
	return slices.Contains([]string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked", "awaiting_grant"}, o.Phase)
}

// AtMain: the operation asked main to move and its outcome is not settled
// (main_intent, or a main_result other than non_ff). No time limit ends it:
// reconciling with main does, or failed{denied} after a recorded denial
// (TD-447 2). A recoverer must exist before S3 writes these phases.
func (o Operation) AtMain() bool {
	return o.Phase == "main_intent" || o.Phase == "main_result" && o.Outcome != "non_ff"
}

func (o Operation) Active() bool {
	return o.Phase != "" && o.Phase != "parked" && o.Phase != "awaiting_grant" && !o.Terminal()
}

// ClaimWindow is a root's wall-clock budget: from its first claim (or the
// Owner's retry), and for a root never claimed from its acceptance (TD-447 4).
const ClaimWindow = 6 * time.Hour

// Bound subtracts authorization waiting only. Window and main-red waits
// remain execution time. Callers with a stream use RootBound.
func Bound(first time.Time, attempts int, now time.Time, waiting ...time.Duration) bool {
	var w time.Duration
	for _, d := range waiting {
		w += d
	}
	return !first.IsZero() && (now.Sub(first)-w >= ClaimWindow || attempts >= 8)
}

// RetryableReasons are the failures the Owner's retry may answer (CUT §8):
// the only ones a root_failed card offers try_again for.
var RetryableReasons = []string{"bound", "stale", "ci_red", "approval_expired"}

// ClaimRefusals are the failed reasons only the first record of a root never
// claimed may carry (TD-447 4, CUT §7.7): its result cannot be claimed
// (result), A is in main already (in_main: merged by hand, not "undelivered"),
// or was and was reverted (reverted). stale and bound end such a root too.
var ClaimRefusals = []string{"result", "in_main", "reverted"}

// ValidSHA: a full lowercase commit id, as the protocol stores them.
func ValidSHA(s string) bool { return sha.MatchString(s) }

// neverClaimed: op is land:<root>:1 for a root with no operation yet.
func neverClaimed(s State, op string) (string, bool) {
	root, ok := strings.CutPrefix(op, "land:")
	if !ok {
		return "", false
	}
	root, ok = strings.CutSuffix(root, ":1")
	if !ok || !task.MatchString(root) {
		return "", false
	}
	for _, o := range s.Operations {
		if o.Task == root {
			return "", false
		}
	}
	return root, true
}

// BlocksClaim is a root-task terminal result. A non-FF finishes only this
// operation: the next operation must still be able to replay on a new main.
func (o Operation) BlocksClaim() bool { return o.EverLanded || o.Phase == "failed" && !o.Retried }

// Reservable is checked against the repository state while reserving in the
// tenant/day stream. A rehearsal never consumes the production quota.
func (o Operation) Reservable(root string) bool {
	return o.Task == root && !o.DryRun && (o.Phase == "branch_pushed" || o.Phase == "awaiting_grant")
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

// Fence is the landing fence of the stream (CUT §11.8 v3 item 1): the later
// of enabled and the live mode, when there is a live mode; enabled alone on a
// stream that never had one (the STG drill). Zero while enabled is not
// recorded: a mode alone puts no root in scope (review 672 B-02). PROD
// records enabled only after its mode (ObserveLandingEnabled), so enabled
// alone is the STG drill's fence. A root with a live operation stays in
// scope whatever the fence (its history, not the time, decides).
func (s State) Fence() time.Time {
	if s.EnabledAt.IsZero() {
		return time.Time{}
	}
	if s.LiveAt.After(s.EnabledAt) {
		return s.LiveAt
	}
	return s.EnabledAt
}
