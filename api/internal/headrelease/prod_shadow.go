package headrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// The shadow of "the platform releases PROD in batches".
//
// Why (2026-11-16): the platform went to PROD 19 times in one evening, each
// time by a seat that held the release window for about 129 seconds, often
// one or two commits after the release before it. The proposal is that the
// loop releases PROD the way it releases STG: the newest commit STG has run
// for a while, when the release policy covers it. Before anyone turns that
// on, this records what it would have done, beside what people did.
//
// Nothing here releases. The decision is a function of ProdFacts, which is
// data; ProdShadowLoop holds three functions: one gathers the facts (reads), one
// counts the commits of a batch (a read), one writes the record. There is no
// World in this file and so no Start; prod_shadow_test.go reads this source
// and fails if one appears.

// Modes of PROD_HEAD_RELEASE. There is no "on": that code is not written.
const (
	ProdOff    = "off"
	ProdShadow = "shadow"

	DefaultProdSoak = 600 * time.Second
	DefaultProdGap  = 1200 * time.Second
)

// ProdMode reads the switch: shadow, or off for everything else (unset, a
// typing mistake, and "on" alike).
func ProdMode(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), ProdShadow) {
		return ProdShadow
	}
	return ProdOff
}

// Seconds reads a whole number of seconds; anything else is the default.
func Seconds(value string, otherwise time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return otherwise
	}
	n := 0
	for _, c := range value {
		if c < '0' || c > '9' || n > 1_000_000 {
			return otherwise
		}
		n = n*10 + int(c-'0')
	}
	return time.Duration(n) * time.Second
}

// Deployed is what the release records say an environment runs.
type Deployed struct {
	// Commit is the deliverable's own repository's commit in the newest
	// record; Since is when that delivery finished; Run is its run.
	Commit string
	Since  time.Time
	Run    string
	// Unsettled says why the records do not tell what the environment runs
	// (a delivery is running or failed after the newest record, or nothing
	// was recorded). Err is a read that failed. Either leaves Commit empty.
	Unsettled string
	Err       string
}

func (d Deployed) known() bool { return d.Unsettled == "" && d.Err == "" && d.Commit != "" }

// ProdFacts is everything the decision reads, gathered once.
type ProdFacts struct {
	Now  time.Time
	Soak time.Duration
	Gap  time.Duration

	Prod Deployed
	Stg  Deployed

	// CI is the strict ledger verdict of the candidate (Succeeded, Failed or
	// ""), and the run it rests on.
	CI    string
	CIRun string
	CIErr string

	// PolicyCovered: the release policy covers releasing the candidate to
	// PROD, the window clause left out. PolicyReasons are its other reasons
	// when it does not.
	PolicyCovered bool
	PolicyID      string
	PolicyReasons []string
	PolicyErr     string

	// OpenCards names each approval of a PROD release of this deliverable
	// that is not finished ("#524 pending").
	OpenCards []string
	CardsErr  string

	// WindowHeld names the holder of an open release window; "" is free.
	WindowHeld string
	WindowErr  string

	Frozen       bool
	FreezeReason string
}

// The conditions, by the names the record uses.
const (
	CondProdSettled = "prod_settled"
	CondStgSettled  = "stg_settled"
	CondSoaked      = "soaked"
	CondDiffers     = "differs_from_prod"
	CondCIStrict    = "ci_strict"
	CondPolicy      = "policy_covers"
	CondNoOpenCard  = "no_open_release_card"
	CondWindowFree  = "window_free"
	CondSpaced      = "spaced"
	CondNotFrozen   = "not_frozen"

	// ProdRecordAction is the audit action of a shadow record.
	ProdRecordAction = "prod_batch_shadow"
)

// ProdConditions is every condition, in the order they are judged.
var ProdConditions = []string{CondProdSettled, CondStgSettled, CondSoaked, CondDiffers, CondCIStrict,
	CondPolicy, CondNoOpenCard, CondWindowFree, CondSpaced, CondNotFrozen}

// ProdDecision is what the shadow would do with these facts.
type ProdDecision struct {
	// WouldRelease: every condition holds. Never true when Unknown is not
	// empty.
	WouldRelease bool
	// Verdict is the decision in words: "would release <commit>", "wait: …"
	// or "unknown: …".
	Verdict string
	From    string
	To      string
	// Failing are the conditions that do not hold, Unknown those whose fact
	// could not be read (each is in Failing too), Skipped those that could
	// not be judged because the commit they are about is not known.
	Failing []string
	Unknown []string
	Skipped []string
	// Reasons says why, by condition.
	Reasons map[string]string
}

// DecideProd judges every condition and lists all that fail, not the first.
func DecideProd(f ProdFacts) ProdDecision {
	d := ProdDecision{Reasons: map[string]string{}}
	fail := func(cond, why string) {
		d.Failing = append(d.Failing, cond)
		d.Reasons[cond] = why
	}
	unknown := func(cond, what string) {
		d.Unknown = append(d.Unknown, cond)
		fail(cond, "unknown: "+what)
	}
	skip := func(cond, why string) {
		d.Skipped = append(d.Skipped, cond)
		d.Reasons[cond] = "not judged: " + why
	}
	settled := func(cond, env string, e Deployed) bool {
		switch {
		case e.Err != "":
			unknown(cond, e.Err)
		case e.Unsettled != "":
			fail(cond, e.Unsettled)
		case e.Commit == "":
			fail(cond, "the newest "+env+" record names no commit of the deliverable")
		default:
			return true
		}
		return false
	}

	prod := settled(CondProdSettled, "prod", f.Prod)
	stg := settled(CondStgSettled, "stg", f.Stg)
	if prod {
		d.From = f.Prod.Commit
	}
	if stg {
		d.To = f.Stg.Commit
	}

	const noCandidate, noProd = "what stg runs is not known", "what prod runs is not known"
	if !stg {
		skip(CondSoaked, noCandidate)
	} else if age := f.Now.Sub(f.Stg.Since); age < f.Soak {
		fail(CondSoaked, fmt.Sprintf("stg has run %s for %s of %s", short(d.To), seconds(age), seconds(f.Soak)))
	}

	switch {
	case !stg:
		skip(CondDiffers, noCandidate)
	case !prod:
		skip(CondDiffers, noProd)
	case d.From == d.To:
		fail(CondDiffers, "prod runs "+short(d.To)+" already")
	}

	switch {
	case !stg:
		skip(CondCIStrict, noCandidate)
	case f.CIErr != "":
		unknown(CondCIStrict, f.CIErr)
	case f.CI != CISucceeded:
		verdict := f.CI
		if verdict == "" {
			verdict = "no verdict yet"
		}
		fail(CondCIStrict, fmt.Sprintf("the full CI of %s: %s", short(d.To), strings.TrimSpace(verdict+" "+f.CIRun)))
	}

	switch {
	case !stg:
		skip(CondPolicy, noCandidate)
	case f.PolicyErr != "":
		unknown(CondPolicy, f.PolicyErr)
	case !f.PolicyCovered:
		why := strings.Join(f.PolicyReasons, "; ")
		if why == "" {
			why = "the policy does not cover it and gives no reason"
		}
		fail(CondPolicy, why)
	}

	switch {
	case f.CardsErr != "":
		unknown(CondNoOpenCard, f.CardsErr)
	case len(f.OpenCards) > 0:
		fail(CondNoOpenCard, strings.Join(f.OpenCards, ", "))
	}

	switch {
	case f.WindowErr != "":
		unknown(CondWindowFree, f.WindowErr)
	case f.WindowHeld != "":
		fail(CondWindowFree, "held by "+f.WindowHeld)
	}

	if !prod {
		skip(CondSpaced, noProd)
	} else if since := f.Now.Sub(f.Prod.Since); since < f.Gap {
		fail(CondSpaced, fmt.Sprintf("the last prod delivery (%s) finished %s ago, of %s", f.Prod.Run, seconds(since), seconds(f.Gap)))
	}

	if f.Frozen {
		fail(CondNotFrozen, strings.TrimSpace("releases are frozen: "+f.FreezeReason))
	}

	switch {
	case len(d.Unknown) > 0:
		d.Verdict = "unknown: " + d.Unknown[0] + ": " + strings.TrimPrefix(d.Reasons[d.Unknown[0]], "unknown: ")
	case !prod:
		d.Verdict = "wait: prod is not settled"
	case !stg:
		d.Verdict = "wait: stg is not settled"
	case len(d.Failing) > 0:
		d.Verdict = "wait: " + strings.Join(d.Failing, ", ")
	default:
		d.WouldRelease = true
		d.Verdict = "would release " + short(d.To)
	}
	return d
}

// Fingerprint is what makes two decisions the same record: whether it would
// release, from what to what, and which conditions fail or are unknown. The
// words of a reason (an age in seconds, an error's text) are not in it.
func (d ProdDecision) Fingerprint() string {
	failing := append([]string(nil), d.Failing...)
	unknown := append([]string(nil), d.Unknown...)
	sort.Strings(failing)
	sort.Strings(unknown)
	return fmt.Sprintf("%t|%s|%s|%s|%s", d.WouldRelease, d.From, d.To, strings.Join(failing, ","), strings.Join(unknown, ","))
}

// ProdBatch is the commits prod lacks and the candidate has.
type ProdBatch struct {
	// Count is how many; Known is false when it could not be read (Note says
	// why). IDs are the newest of them, short; Work the Work trailers among
	// those, each once.
	Count int
	Known bool
	IDs   []string
	Work  []string
	Note  string
}

// ProdBatchMax caps the ids one record lists.
const ProdBatchMax = 50

// prodRecord is the audit entry's detail.
type prodRecord struct {
	WouldRelease bool              `json:"would_release"`
	Verdict      string            `json:"verdict"`
	From         string            `json:"from"`
	To           string            `json:"to"`
	Commits      any               `json:"commits"` // a number, or "unknown"
	CommitIDs    []string          `json:"commit_ids,omitempty"`
	Work         []string          `json:"work,omitempty"`
	CommitsNote  string            `json:"commits_note,omitempty"`
	Failing      []string          `json:"failing"`
	Skipped      []string          `json:"skipped,omitempty"`
	Reasons      map[string]string `json:"reasons"`
	At           string            `json:"at"`
}

// ProdShadowLoop evaluates the PROD candidate each pass and records a
// decision when it differs from the last one it recorded. It holds reads and
// one writer of records; it cannot start, hold or request anything.
type ProdShadowLoop struct {
	// Mode is ProdMode's answer; anything but ProdShadow makes Pass do
	// nothing at all.
	Mode string
	// Gather reads the facts of this moment.
	Gather func(ctx context.Context) ProdFacts
	// Batch reads the commits between two commits; nil records "unknown".
	Batch func(ctx context.Context, from, to string) ProdBatch
	// Record writes one record: result is would_release, wait or unknown.
	Record func(result, detail string)

	mu   sync.Mutex
	last string
}

// Pass judges once. recorded says whether a record was written.
func (s *ProdShadowLoop) Pass(ctx context.Context) (d ProdDecision, recorded bool) {
	if s == nil || s.Mode != ProdShadow || s.Gather == nil {
		return ProdDecision{}, false
	}
	f := s.Gather(ctx)
	d = DecideProd(f)
	mark := d.Fingerprint()
	s.mu.Lock()
	same := s.last == mark
	s.last = mark
	s.mu.Unlock()
	if same || s.Record == nil {
		return d, false
	}
	rec := prodRecord{WouldRelease: d.WouldRelease, Verdict: d.Verdict, From: d.From, To: d.To, Commits: "unknown",
		Failing: d.Failing, Skipped: d.Skipped, Reasons: d.Reasons, At: f.Now.UTC().Format(time.RFC3339)}
	if rec.Failing == nil {
		rec.Failing = []string{}
	}
	switch {
	case d.From == "" || d.To == "":
	case d.From == d.To:
		rec.Commits = 0
	case s.Batch != nil:
		b := s.Batch(ctx, d.From, d.To)
		if b.Known {
			rec.Commits = b.Count
		}
		rec.CommitIDs, rec.Work, rec.CommitsNote = b.IDs, b.Work, b.Note
		if len(rec.CommitIDs) > ProdBatchMax {
			rec.CommitIDs = rec.CommitIDs[:ProdBatchMax]
		}
	}
	detail, err := json.Marshal(rec)
	if err != nil {
		detail = []byte(d.Verdict)
	}
	result := "wait"
	switch {
	case d.WouldRelease:
		result = "would_release"
	case len(d.Unknown) > 0:
		result = "unknown"
	}
	s.Record(result, string(detail))
	return d, true
}

func seconds(d time.Duration) string { return fmt.Sprintf("%ds", int(d/time.Second)) }
