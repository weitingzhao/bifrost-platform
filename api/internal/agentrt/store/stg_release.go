package store

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// StgEntry includes the enclosing event so mixed historical landing records
// and current subprotocol records can be replayed in repository sequence order.
type StgEntry struct {
	Seq   int64
	At    time.Time
	Event Event
}
type StgEvent struct {
	Seq    int64
	At     time.Time
	Record StgRecord
}
type StgBudgetKey struct {
	R   string
	Gen int64
}
type StgCoverKey struct {
	Evidence string
	Member   StgMemberKey
}
type StgValidityKey struct {
	Member StgMemberKey
	P      string
}
type StgMemberState struct {
	Batch, To      string
	Gen            int64
	Ref            any
	FirstPendingAt time.Time
	Disposition    StgEvent
}
type StgDebit struct {
	Batch                 string
	Try, Total            int64
	TerminalAt, NotBefore time.Time
}
type StgTry struct {
	Snapshot, Started, Permit, Bound, RunEvidence, Effect, Verify StgEvent
	Unsent, Quarantined, QuarantineClosed, Retry                  StgEvent
}

func (t *StgTry) Occupied() bool {
	return t != nil && t.Started.Seq > 0 && t.RunEvidence.Seq == 0 && t.Unsent.Seq == 0 && t.QuarantineClosed.Seq == 0
}
func (t *StgTry) IsQuarantined() bool {
	return t != nil && t.Quarantined.Seq > 0 && t.RunEvidence.Seq == 0 && t.QuarantineClosed.Seq == 0
}

type StgBatch struct {
	Phase                     string
	Opened, Selected, Settled StgEvent
	Members                   map[StgMemberKey]int64
	CI                        map[string]StgEvent
	Try                       int64
	Tries                     map[int64]*StgTry
	Diagnostics               map[string]StgEvent
}
type StgBlock struct{ Opened, Closed StgEvent }

func (b StgBlock) IsOpen() bool { return b.Opened.Seq > 0 && b.Closed.Seq == 0 }

// StgState is a detached read model, never cached across transactions. All
// authoritative identities and times survive replay; diagnostics do not move
// the phase, and covers do not release an outstanding execution slot.
type StgState struct {
	Batches            map[string]*StgBatch
	Members            map[StgMemberKey]StgMemberState
	Budget             map[StgBudgetKey]int64
	Generations        map[string]int64
	Debits             map[string]StgDebit
	Grants             map[string]StgEvent
	Withheld           map[StgMemberKey]StgEvent
	Blocks             map[string]StgBlock
	BlockHistory       map[int64]StgBlock
	CIObservations     map[int64]map[string]StgEvent // opening seq -> UID -> successful terminal read
	BoundIntents       map[string]StgEvent           // UID -> first bound (batch/try never released)
	Evidence           map[string]StgEvent
	DeploymentChecks   map[string]StgEvent
	Covers             map[StgCoverKey]StgEvent
	Legacy             landing.State
	Validity           map[StgValidityKey]StgEvent
	Records            map[int64]StgEvent
	Answers            map[string]StgEvent
	InMain             map[string]string   // op -> H from failed{in_main}'s recorded history
	Disposals          map[string]StgEvent // op -> latest legacy Owner disposition
	LastBatch, LastSeq int64
}

func newStgState() StgState {
	return StgState{Batches: map[string]*StgBatch{}, Members: map[StgMemberKey]StgMemberState{}, Budget: map[StgBudgetKey]int64{},
		Generations: map[string]int64{}, Debits: map[string]StgDebit{}, Grants: map[string]StgEvent{}, Withheld: map[StgMemberKey]StgEvent{},
		Blocks: map[string]StgBlock{}, BlockHistory: map[int64]StgBlock{}, Evidence: map[string]StgEvent{}, DeploymentChecks: map[string]StgEvent{}, Covers: map[StgCoverKey]StgEvent{},
		CIObservations: map[int64]map[string]StgEvent{}, BoundIntents: map[string]StgEvent{},
		Validity: map[StgValidityKey]StgEvent{}, Records: map[int64]StgEvent{}, Answers: map[string]StgEvent{}, InMain: map[string]string{}, Disposals: map[string]StgEvent{}}
}

// StgFold is deterministic and does no I/O. Legacy records still use the
// original parser and Terminal semantics. Unknown/malformed STG records fail
// the read instead of silently showing an empty or healthy release state.
func StgFold(entries []StgEntry) (StgState, error) {
	s := newStgState()
	for _, e := range entries {
		if e.Seq <= s.LastSeq || e.At.IsZero() {
			return StgState{}, stgError("invalid", "", "events require increasing sequences and times")
		}
		if raw, ok := e.Event.Body["stg_release"]; ok {
			if err := stgEnvelope(e.Event); err != nil {
				return StgState{}, err
			}
			r, err := ParseStgRelease(raw)
			if err != nil {
				return StgState{}, err
			}
			if _, err = s.apply(StgEvent{e.Seq, e.At, r}); err != nil {
				return StgState{}, err
			}
		} else if raw, ok := e.Event.Body["landing"]; ok {
			r, err := landing.Parse(raw)
			if err != nil {
				return StgState{}, err
			}
			if err = s.Legacy.Apply(landing.Entry{At: e.At, Record: r}); err != nil {
				return StgState{}, err
			}
			s.Records[e.Seq] = StgEvent{e.Seq, e.At, StgRecord(r)}
			if r.String("kind") == "failed" && r.String("why") == "in_main" {
				text, _ := e.Event.Body["text"].(string)
				s.InMain[r.String("op")] = stgInMainCommit(text)
			}
		} else if raw, ok := e.Event.Body["landing_disposed"]; ok {
			r, err := stgDetached(raw)
			if err != nil {
				return StgState{}, err
			}
			s.Disposals[r.String("op")] = StgEvent{e.Seq, e.At, r}
		} else if raw, ok := e.Event.Body["landing_main"]; ok {
			if why := badLandingMain(raw); why != "" {
				return StgState{}, stgError("invalid", "landing_main", why)
			}
			r, err := stgDetached(raw)
			if err != nil {
				return StgState{}, err
			}
			if r.String("stg_evidence") != "" {
				if err := s.checkCoverageProof(r); err != nil {
					return StgState{}, err
				}
			}
			s.Validity[StgValidityKey{stgMember(r), r.String("head")}] = StgEvent{e.Seq, e.At, r}
			s.Records[e.Seq] = StgEvent{e.Seq, e.At, r}
		} else if raw, ok := e.Event.Body["landing_card_outcome"].(map[string]any); ok && raw["family"] == "stg" {
			// The card protocol checks the storage-side question binding.
			// Replay retains the recorded outcome, never an RPC reply.
			r, err := stgDetached(raw)
			if err != nil {
				return StgState{}, err
			}
			if e.Event.Actor != "controller" || e.Event.Type != "note" || r.String("question_id") == "" {
				return StgState{}, stgError("permission", "answer", "invalid recorded card outcome")
			}
			if _, ok := s.Answers[r.String("question_id")]; ok {
				return StgState{}, stgError("duplicate", "answer", "question already handled")
			}
			s.Answers[r.String("question_id")] = StgEvent{e.Seq, e.At, r}
		}
		s.LastSeq = e.Seq
	}
	return s, nil
}

func stgDetached(raw any) (StgRecord, error) {
	b, err := canon.Encode(raw)
	if err != nil {
		return nil, err
	}
	v, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	return StgRecord(v.(map[string]any)), nil
}

func stgEnvelope(ev Event) error {
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != StgReleaseStream || ev.TaskID != "" || ev.MissionID != "" || ev.AttemptID != "" || ev.PlanNode != "" {
		return stgError("permission", "", "requires a controller note on the repository stream without row bindings")
	}
	for key := range ev.Body {
		if key != "text" && key != "stg_release" {
			return stgError("permission", "", "one subprotocol record per note")
		}
	}
	return nil
}

func (s StgState) generation(r string) int64 {
	if n := s.Generations[r]; n > 0 {
		return n
	}
	return 1
}
func (s StgState) covered(m StgMemberKey) bool {
	for k := range s.Covers {
		if k.Member == m {
			return true
		}
	}
	return false
}
func (s StgState) effective(m StgMemberKey, p string) bool {
	r := s.Validity[StgValidityKey{m, p}].Record
	return r.String("validity") == "effective" && r["has_r"] == true
}
func (s StgState) available(m StgMemberKey, gen int64) bool {
	_, withheld := s.Withheld[m]
	return !withheld && gen == s.generation(m.R) && s.Budget[StgBudgetKey{m.R, gen}] < 3
}
func stgSame(a, b any) bool {
	x, e := canon.Encode(a)
	y, f := canon.Encode(b)
	return e == nil && f == nil && string(x) == string(y)
}
func stgSameMembers(ms []StgMember, set map[StgMemberKey]int64) bool {
	if len(ms) != len(set) {
		return false
	}
	for _, m := range ms {
		if _, ok := set[m.StgMemberKey]; !ok {
			return false
		}
	}
	return true
}

// StgBlockID fixes the otherwise ambiguous hash concatenation to canonical
// JSON of the five named identity inputs; detail and scan time are excluded.
func StgBlockID(p, baseP, why, version string) string {
	h, _ := canon.Sum(map[string]any{"repo": "bifrost-platform", "P": p, "base.P": baseP, "why": why, "evidence_version": version})
	return h[:16]
}

func (s *StgState) apply(e StgEvent) (int64, error) {
	r := e.Record
	k := r.String("kind")
	fail := func(code, why string) (int64, error) { return 0, stgError(code, k, why) }
	if e.Seq <= s.LastSeq || e.At.IsZero() {
		return fail("invalid", "invalid event sequence or time")
	}
	for _, key := range []string{"at", "first_at"} {
		if _, ok := r[key]; ok && r.Time(key).After(e.At) {
			return fail("invalid", key+" is in the future")
		}
	}
	if r.String("scope") != "batch" {
		if err := s.applyOther(e); err != nil {
			return 0, err
		}
		s.Records[e.Seq] = e
		return 0, nil
	}
	id := r.String("batch")
	b := s.Batches[id]
	if k == "opened" {
		if b != nil {
			return fail("duplicate", "batch already exists")
		}
		if stgBatchNumber(id) <= s.LastBatch {
			return fail("conflict", "batch numbers must increase")
		}
		for _, b := range s.Batches {
			if b.Phase != "CLOSED" {
				return fail("transition", "repository has an open batch")
			}
		}
		// The v1 block schema has no affected set. Until it does, an open
		// repository block holds every new batch rather than guessing members.
		for _, b := range s.Blocks {
			if b.IsOpen() {
				return fail("transition", "repository has an open blocking interval")
			}
		}
		members := map[StgMemberKey]int64{}
		for _, m := range r.members("members") {
			old := s.Members[m.StgMemberKey]
			if !s.available(m.StgMemberKey, m.Gen) {
				return fail("budget", "member has no current budget")
			}
			if old.To != "" && old.To != "released" || s.covered(m.StgMemberKey) {
				return fail("transition", "member already has a disposition")
			}
			pending := r.object("first_pending_at").Time(m.R)
			if pending.After(e.At) || !old.FirstPendingAt.IsZero() && !pending.Equal(old.FirstPendingAt) {
				return fail("conflict", "first_pending_at must survive batch changes")
			}
			members[m.StgMemberKey] = m.Gen
		}
		b = &StgBatch{Phase: "OPEN", Opened: e, Members: members, CI: map[string]StgEvent{}, Tries: map[int64]*StgTry{}, Diagnostics: map[string]StgEvent{}}
		for _, m := range r.members("members") {
			s.Members[m.StgMemberKey] = StgMemberState{Batch: id, Gen: m.Gen, FirstPendingAt: r.object("first_pending_at").Time(m.R)}
		}
		s.Batches[id] = b
		s.LastBatch = stgBatchNumber(id)
		s.Records[e.Seq] = e
		return 0, nil
	}
	if b == nil {
		return fail("transition", "batch has not opened")
	}
	// The one idempotent edge returns the original row, even after settlement.
	if k == "bound" {
		if t := b.Tries[r.Int("try")]; t != nil && t.Bound.Seq > 0 {
			if t.Bound.Record.String("uid") == r.String("uid") {
				return t.Bound.Seq, nil
			}
			return fail("conflict", "different run UID")
		}
	}
	if b.Phase == "CLOSED" && !(k == "diag" && r.String("what") == "finalizer_stuck") {
		return fail("transition", "batch is closed")
	}
	t := b.Tries[b.Try]
	if _, has := r["try"]; has && !slices.Contains([]string{"snapshot", "retry", "diag"}, k) && (t == nil || r.Int("try") != b.Try) {
		return fail("transition", "event is not for the current try")
	}
	switch k {
	case "p_selected":
		if b.Phase != "OPEN" || b.Selected.Seq > 0 {
			return fail("transition", "P is selected once while OPEN")
		}
		b.Selected = e
	case "ci_accepted":
		ci := r.object("ci")
		if b.Phase != "OPEN" && b.Phase != "SNAP" {
			return fail("transition", "CI requires OPEN or SNAP")
		}
		if b.Selected.Seq == 0 || r.String("P") != b.Selected.Record.String("P") || ci.String("platform_commit") != r.String("P") || ci.Time("read_at").After(e.At) {
			return fail("conflict", "CI must match selected P and its observation time")
		}
		if _, exists := b.CI[ci.String("uid")]; exists {
			return fail("duplicate", "CI UID already accepted")
		}
		for _, old := range b.CI {
			if old.Record.object("ci").String("ui_commit") != ci.String("ui_commit") {
				return fail("conflict", "CI changed U")
			}
		}
		b.CI[ci.String("uid")] = e
	case "snapshot":
		n := r.Int("try")
		if n != b.Try+1 || n == 1 && (b.Phase != "OPEN" || len(b.CI) == 0) || n > 1 && (t == nil || t.Retry.Seq == 0 || t.Retry.Record.Int("try") != n) {
			return fail("transition", "snapshot requires accepted CI or the next retry")
		}
		inputs := r.object("inputs")
		base := r.object("base")
		if inputs.String("revision") != b.Selected.Record.String("P") {
			return fail("conflict", "snapshot changed P")
		}
		matched := false
		for _, ci := range b.CI {
			c := ci.Record.object("ci")
			if inputs.String("uiRevision") == c.String("ui_commit") && inputs.String("infraRevision") == c.String("infra_commit") {
				matched = true
			}
		}
		if !matched {
			return fail("conflict", "inputs do not match accepted CI")
		}
		if !s.matchesBase(base) {
			return fail("transition", "base must name matching trusted evidence")
		}
		if n > 1 {
			first := b.Tries[1].Snapshot.Record
			if !stgSame(r["inputs"], first["inputs"]) || !stgSame(r["base"], first["base"]) || !stgSame(r["set"], first["set"]) || r.Time("not_before").Before(t.Retry.Record.Time("not_before")) {
				return fail("conflict", "retry cannot change frozen inputs, base, set or shorten backoff")
			}
		}
		for _, m := range r.members("set") {
			if _, ok := b.Members[m.StgMemberKey]; !ok {
				return fail("budget", "set is not a subset of batch members")
			}
			if r.Time("not_before").Before(s.Debits[m.R].NotBefore) {
				return fail("budget", "snapshot shortened inherited backoff")
			}
		}
		b.Try = n
		b.Tries[n] = &StgTry{Snapshot: e}
		b.Phase = "SNAP"
	case "started":
		if b.Phase != "SNAP" || t.Started.Seq > 0 || e.At.Before(t.Snapshot.Record.Time("not_before")) {
			return fail("transition", "started requires a due, unspent snapshot")
		}
		if err := s.checkReleaseBlocks(b, k); err != nil {
			return 0, err
		}
		if err := s.checkStart(b, e); err != nil {
			return 0, err
		}
		for _, m := range t.Snapshot.Record.members("set") {
			gen := b.Members[m.StgMemberKey]
			s.Budget[StgBudgetKey{m.R, gen}]++
			d := s.Debits[m.R]
			s.Debits[m.R] = StgDebit{Batch: id, Try: b.Try, Total: d.Total + 1}
		}
		t.Started = e
		t.Permit = e
		b.Phase = "STARTED"
	case "resend", "unsent":
		if !t.Occupied() || t.Bound.Seq > 0 || t.IsQuarantined() || e.At.Before(t.Permit.Record.Time("deadline")) {
			return fail("transition", "requires unbound execution after its latest permit expires")
		}
		if k == "resend" {
			if err := s.checkReleaseBlocks(b, k); err != nil {
				return 0, err
			}
			if r.Int("intent_seq") != t.Started.Seq || r.object("lease").String("who") != t.Started.Record.object("lease").String("who") || r.object("lease").Int("gen") < t.Permit.Record.object("lease").Int("gen") || !stgDeadline(e) {
				return fail("conflict", "resend requires the original intent, a nondecreasing lease generation and a new deadline")
			}
			t.Permit = e
		} else {
			t.Unsent = e
			b.Phase = "UNSENT"
			s.endTry(id, b.Try, e.At)
		}
	case "bound":
		if !t.Occupied() {
			return fail("transition", "bound requires an outstanding execution")
		}
		if s.BoundIntents[r.String("uid")].Seq > 0 {
			return fail("conflict", "run UID is already bound to another intent")
		}
		for id := range s.Evidence {
			if stgRunUID(id) == r.String("uid") {
				return fail("conflict", "run UID already has evidence without this binding")
			}
		}
		t.Bound = e
		s.BoundIntents[r.String("uid")] = e
	case "run_evidence":
		if t.Started.Seq == 0 || t.Bound.Seq == 0 || t.RunEvidence.Seq > 0 || t.Unsent.Seq > 0 || t.QuarantineClosed.Seq > 0 || r.String("uid") != t.Bound.Record.String("uid") || r.Time("at").Before(t.Started.At) {
			return fail("transition", "terminal evidence requires the bound UID and no earlier terminal decision")
		}
		t.RunEvidence = e
		b.Phase = "TERMINAL"
		s.endTry(id, b.Try, r.Time("at"))
	case "effect":
		if t.RunEvidence.Record.String("status") != "Failed" && t.Verify.Record.String("outcome") != "failed" {
			return fail("transition", "effect requires a failed run or verification")
		}
		if t.Effect.Seq > 0 && (t.Effect.Record.String("class") != "unknown" || r.String("class") == "unknown") {
			return fail("conflict", "only an unknown effect may become certain")
		}
		if r.Time("at").Before(t.RunEvidence.Record.Time("at")) {
			return fail("invalid", "effect predates the terminal run")
		}
		t.Effect = e
	case "verify":
		if t.RunEvidence.Seq == 0 || t.Verify.Seq > 0 {
			return fail("transition", "verification follows terminal evidence exactly once")
		}
		if err := s.checkVerify(b, e); err != nil {
			return 0, err
		}
		t.Verify = e
		b.Phase = "VERIFIED"
		if r.String("outcome") != "verified" {
			b.Phase = r.String("outcome")
		}
	case "member":
		if err := s.applyMember(b, e); err != nil {
			return 0, err
		}
	case "retry":
		if t == nil || r.Int("try") != b.Try+1 || t.Retry.Seq > 0 || t.IsQuarantined() {
			return fail("transition", "retry must name the next try once")
		}
		if t.Unsent.Seq == 0 && (t.Verify.Record.String("outcome") != "failed" || !slices.Contains([]string{"run_failed", "rollout", "health"}, t.Verify.Record.String("why")) || !slices.Contains([]string{"unchanged", "this_try"}, t.Effect.Record.String("class"))) {
			return fail("transition", "failure is not retryable or its deployment effect is unknown")
		}
		for m, gen := range b.Members {
			if !s.available(m, gen) || s.Members[m].To != "" || s.covered(m) || r.Time("not_before").Before(s.Debits[m.R].NotBefore) {
				return fail("budget", "retry needs every member available and inherited backoff")
			}
		}
		t.Retry = e
		b.Phase = "RETRY"
	case "quarantined":
		if !t.Occupied() || t.Quarantined.Seq > 0 || r.String("why") == "object_lost" && t.Bound.Seq == 0 || r.String("why") != "object_lost" && t.Bound.Seq > 0 {
			return fail("transition", "quarantine must match an outstanding execution's binding")
		}
		t.Quarantined = e
		b.Phase = "QUARANTINED"
	case "quarantine_closed":
		if !t.IsQuarantined() || t.QuarantineClosed.Seq > 0 {
			return fail("transition", "only an open quarantine can be closed")
		}
		if err := s.ownerAnswer(r.String("question_id"), t.Quarantined.Seq, "accept_unreconciled", nil); err != nil {
			return 0, err
		}
		t.QuarantineClosed = e
		b.Phase = "QUARANTINE_CLOSED"
		s.endTry(id, b.Try, e.At)
	case "settled":
		if !stgSameMembers(r.members("members"), b.Members) {
			return fail("conflict", "settlement must name exactly all members")
		}
		for m := range b.Members {
			if s.Members[m].To == "" {
				return fail("transition", "every member needs its own disposition")
			}
		}
		for _, t := range b.Tries {
			if t.Occupied() {
				return fail("transition", "execution is still outstanding")
			}
		}
		b.Settled = e
		b.Phase = "CLOSED"
	case "diag":
		if r.String("what") == "finalizer_stuck" {
			tr := b.Tries[r.Int("try")]
			if tr == nil || tr.RunEvidence.Seq == 0 || tr.Bound.Seq == 0 || e.At.Sub(tr.Bound.At) < 30*24*time.Hour {
				return fail("transition", "finalizer diagnostic requires an archived try bound for 30 days")
			}
		} else if r.Int("try") != b.Try {
			return fail("transition", "diagnostic must name the current try (zero before snapshot)")
		}
		key := fmt.Sprintf("%s/%d/%d", r.String("what"), r.Int("try"), r.Int("gen"))
		if _, ok := b.Diagnostics[key]; ok {
			return fail("duplicate", "diagnostic already recorded")
		}
		b.Diagnostics[key] = e
	default:
		return fail("invalid", "unknown batch kind")
	}
	s.Records[e.Seq] = e
	return 0, nil
}

func stgDeadline(e StgEvent) bool {
	return e.Record.Time("deadline").After(e.At) && !e.Record.Time("deadline").After(e.At.Add(2*time.Minute))
}
func (s StgState) matchesBase(base StgRecord) bool {
	e := s.Evidence[base.String("evidence")].Record
	return e != nil && e.String("P") == base.String("P") && e.object("content").String("api_digest") == base.String("api_digest") && e.object("content").String("console_digest") == base.String("console_digest")
}
func (s *StgState) endTry(batch string, try int64, at time.Time) {
	for r, d := range s.Debits {
		if d.Batch == batch && d.Try == try {
			wait := 10 * time.Minute
			if d.Total >= 2 {
				wait = 30 * time.Minute
			}
			d.TerminalAt = at
			d.NotBefore = at.Add(wait)
			s.Debits[r] = d
		}
	}
}
func (s StgState) expectedSpend(b *StgBatch) ([]any, error) {
	spend := []any{}
	for _, m := range b.Tries[b.Try].Snapshot.Record.members("set") {
		gen := b.Members[m.StgMemberKey]
		if !s.available(m.StgMemberKey, gen) || s.Members[m.StgMemberKey].To != "" || s.covered(m.StgMemberKey) {
			return nil, stgError("budget", "started", "set member is unavailable")
		}
		spend = append(spend, map[string]any{"R": m.R, "gen": gen, "n": s.Budget[StgBudgetKey{m.R, gen}] + 1})
	}
	return spend, nil
}
func stgFingerprint(inputs, labels, annotations StgRecord) string {
	h, _ := canon.Sum(map[string]any{"namespace": inputs["namespace"], "pipeline": inputs["pipeline"], "params_canonical": inputs["params_canonical"], "labels": map[string]any(labels), "annotations": map[string]any(annotations)})
	return h
}
func (s StgState) checkStart(b *StgBatch, e StgEvent) error {
	r := e.Record
	n := stgBatchNumber(r.String("batch"))
	inputs := b.Tries[b.Try].Snapshot.Record.object("inputs")
	wantRun := fmt.Sprintf("stgrel-%d-%d-%s", n, b.Try, inputs.String("revision")[:8])
	labels := map[string]any{"bifrost.io/stg-release": "true", "bifrost.io/stg-release-n": strconv.FormatInt(n, 10), "bifrost.io/stg-release-try": strconv.FormatInt(b.Try, 10)}
	annotations := map[string]any{"bifrost.io/stg-release-batch": r.String("batch"), "bifrost.io/stg-release-intent": strconv.FormatInt(e.Seq, 10)}
	if r.Int("intent_seq") != e.Seq || r.String("run") != wantRun || len(wantRun) > 40 || !stgSame(r["labels"], labels) || !stgSame(r["annotations"], annotations) || r.String("fingerprint") != stgFingerprint(inputs, StgRecord(labels), StgRecord(annotations)) || r.object("lease").String("who") != fmt.Sprintf("stgrel-bifrost-platform-%d-%d", n, b.Try) || !stgDeadline(e) {
		return stgError("conflict", "started", "intent, deterministic identity, fingerprint or permit does not match")
	}
	for _, m := range b.Tries[b.Try].Snapshot.Record.members("set") {
		if e.At.Before(s.Debits[m.R].NotBefore) {
			return stgError("budget", "started", "member backoff has not elapsed")
		}
	}
	want, err := s.expectedSpend(b)
	if err != nil {
		return err
	}
	if !stgSame(r["spend"], want) {
		return stgError("budget", "started", "spend is not the recomputed debit of all S")
	}
	return nil
}
func (s StgState) checkVerify(b *StgBatch, e StgEvent) error {
	r := e.Record
	t := b.Tries[b.Try]
	outcome := r.String("outcome")
	if t.RunEvidence.Record.String("status") == "Failed" && (outcome != "failed" || !slices.Contains([]string{"run_failed", "wrong_input"}, r.String("why"))) {
		return stgError("conflict", "verify", "a failed run verifies as run_failed or wrong_input")
	}
	if outcome == "failed" {
		return nil
	}
	ev, ok := s.Evidence[r.String("evidence")]
	if !ok {
		return stgError("transition", "verify", "trusted deployment evidence is missing")
	}
	inputs := t.Snapshot.Record.object("inputs")
	if outcome == "verified" && (ev.Record.String("source") != "automatic" || ev.Record.String("batch") != b.Opened.Record.String("batch") || ev.Record.String("run") != t.Started.Record.String("run") || ev.Record.String("P") != inputs.String("revision") || ev.Record.String("U") != inputs.String("uiRevision") || ev.Record.String("infra") != inputs.String("infraRevision")) {
		return stgError("conflict", "verify", "verified evidence is not this execution")
	}
	if outcome == "superseded" {
		observed := s.deploymentObservation(r.String("evidence"))
		if stgRunUID(r.String("evidence")) == t.Bound.Record.String("uid") || observed.Seq <= t.RunEvidence.Seq || observed.Record.object("deployed").Time("at").Before(t.RunEvidence.At) || observed.Record.object("deployed").Time("at").Before(t.RunEvidence.Record.Time("at")) {
			return stgError("conflict", "verify", "superseding deployment must be rechecked after this run's terminal evidence")
		}
	}
	return nil
}
func (s *StgState) applyMember(b *StgBatch, e StgEvent) error {
	r := e.Record
	m := stgMember(r)
	old := s.Members[m]
	to := r.String("to")
	if _, ok := b.Members[m]; !ok || old.Batch != r.String("batch") || old.To != "" {
		return stgError("transition", "member", "member is absent or already settled")
	}
	t := b.Tries[b.Try]
	if t.Occupied() {
		return stgError("transition", "member", "member settlement waits for the outstanding execution")
	}
	refSeq, _ := cardNumber(r["ref"])
	switch to {
	case "delivered":
		if t == nil || t.Verify.Record.String("outcome") != "verified" || refSeq != t.Verify.Seq || !s.effective(m, t.Snapshot.Record.object("inputs").String("revision")) {
			return stgError("transition", "member", "delivery needs verified execution and effective R at P")
		}
	case "covered":
		if _, ok := s.Covers[StgCoverKey{r.String("ref"), m}]; !ok {
			return stgError("transition", "member", "coverage is missing")
		}
	case "disposed":
		proof := s.Records[refSeq].Record
		// Only the recorded landing reversal of this operation disposes it.
		if proof.String("kind") != "reverted" || proof.String("op") != m.Op || proof.String("R") != m.R || s.Legacy.Operations[m.Op].Task != m.Root {
			return stgError("transition", "member", "disposal needs this member's landing reversal")
		}
		authorized := false
		for _, op := range s.Legacy.Operations {
			if op.EverLanded && op.R == proof.String("by") {
				authorized = true
			}
		}
		if !authorized {
			return stgError("transition", "member", "reversal itself must be a landed commit")
		}
	case "exhausted":
		if s.Budget[StgBudgetKey{m.R, old.Gen}] < 3 {
			return stgError("budget", "member", "member has remaining budget")
		}
		fallthrough
	case "released":
		if to == "released" && t != nil && t.Verify.Record.String("why") == "wrong_input" {
			blocked := false
			for _, interval := range s.Blocks {
				p := interval.Opened.Record
				if interval.IsOpen() && p.String("why") == "wrong_input" && p.String("P") == t.Snapshot.Record.object("inputs").String("revision") && stgSame(p["base"], t.Snapshot.Record["base"]) {
					blocked = true
				}
			}
			if !blocked {
				return stgError("transition", "member", "wrong_input requires a blocking interval before release")
			}
		}
		proof := s.Records[refSeq]
		if refSeq <= 0 || proof.Seq == 0 || (proof.Record.String("batch") != r.String("batch") && (proof.Record.String("kind") != "blocked" || b.Selected.Record.String("P") != proof.Record.String("P"))) {
			return stgError("transition", "member", "disposition must reference a recorded decision")
		}
	}
	old.To = to
	old.Ref = r["ref"]
	old.Disposition = e
	s.Members[m] = old
	return nil
}
