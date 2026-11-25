package planstore

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

// The plan code's ports: what this package needs of another domain, declared
// here as small function types over the bases' and the plan's own types. The
// domain that provides one binds it at start (a Bind function per providing
// domain, called from an init of the provider's side: today package store,
// which holds or wires every provider); this package names the port and not
// the other domain's package, which is what lets it be a package of the plan
// domain alone (it was the plan files of package store until 2026-11-17).
//
// A port nobody bound refuses: its caller gets ErrPortUnbound and the
// operation fails. It never answers as if there were no rule, no claim or
// no import.

// ErrPortUnbound: the plan code needed something of another domain and
// nothing was bound to provide it, so the operation is refused.
var ErrPortUnbound = errors.New("store: a port of the plan code is not bound")

func unbound(what, bind string) error {
	return fmt.Errorf("%w: %s (%s was not called); the operation is refused", ErrPortUnbound, what, bind)
}

// PlanRule is what the plan code reads of the plan-standing rule in force:
// which version it is, and whether it grants done rules.
type PlanRule struct {
	ID        string
	Version   int
	DoneRules bool
	// DoneRulesReadings: such a first rule may also name the reading atom.
	DoneRulesReadings bool
}

// RulePorts is what the plan code needs of the signed rules (verification).
type RulePorts struct {
	// PlanRuleInForce is the plan-standing rule in force at the database's
	// clock; nil when none is.
	PlanRuleInForce func(ctx context.Context, tx pgx.Tx) (*PlanRule, error)
	// CardAutoKindsInForce is the kinds of card the card-auto-answer rule in
	// force at the database's clock answers; none when no such rule is.
	CardAutoKindsInForce func(ctx context.Context, tx pgx.Tx) ([]string, error)
}

var rulePorts atomic.Pointer[RulePorts]

// BindPlanRules binds the rules' side. It is called once, from the init of
// the rules' files (store's rules.go).
func BindPlanRules(p RulePorts) { rulePorts.Store(&p) }

func planRuleInForce(ctx context.Context, tx pgx.Tx) (*PlanRule, error) {
	return planRuleVia(ctx, rulePorts.Load(), tx)
}

func planRuleVia(ctx context.Context, p *RulePorts, tx pgx.Tx) (*PlanRule, error) {
	if p == nil || p.PlanRuleInForce == nil {
		return nil, unbound("the plan-standing rule in force", "BindPlanRules")
	}
	return p.PlanRuleInForce(ctx, tx)
}

func cardAutoKindsInForce(ctx context.Context, tx pgx.Tx) ([]string, error) {
	return cardAutoKindsVia(ctx, rulePorts.Load(), tx)
}

func cardAutoKindsVia(ctx context.Context, p *RulePorts, tx pgx.Tx) ([]string, error) {
	if p == nil || p.CardAutoKindsInForce == nil {
		return nil, unbound("the card-auto-answer rule in force", "BindPlanRules")
	}
	return p.CardAutoKindsInForce(ctx, tx)
}

// RecentWorkPage says which of a root's listed events RootActivity reads:
// within Bounds (a JSON object of stream to highest sequence, a stream it does
// not name having none; nil: all of them, the first page), with an id below
// Before (0: any), at most Limit (0: none).
type RecentWorkPage struct {
	Bounds []byte
	Before int64
	Limit  int
}

// RootActivity is what the plan view takes from the records of a root's work,
// read together: one scan of the ledger answers all of it.
type RootActivity struct {
	// Work is the work registered under the root, each with its description.
	// The plan view shows it and takes the sign-off candidates from it:
	// external_work_id, plan_node_id, source_run_key, artifact_id, state and
	// report_digest.
	Work []map[string]any
	// Streams gives each stream holding events of the work that Recent
	// changes lists its highest sequence, whatever the page.
	Streams map[string]int64
	// Recent is the asked page of those events, newest first.
	Recent []planreadings.RecentEvent
	// Registrants is who registered work under the root; the coverage count
	// does not count them.
	Registrants []string
}

// ExternalWorkPorts is what the plan code needs of registered external work
// (lineage): the check of its two documents where the plan code checks every
// note and every view artifact, and the reads of a root's registered work. The
// reads run in the caller's transaction, so what they answer belongs to the
// caller's snapshot; the shape of the work's records stays the lineage's.
type ExternalWorkPorts struct {
	// CheckNote holds the external_work record of a note to its shape.
	CheckNote func(record any) error
	// CheckDescription holds an artifact that says it is an external work
	// description to being one.
	CheckDescription func(raw []byte) error
	// ActivityBuckets marks each work with its verification and gives the
	// ones working now and the ones finished today.
	ActivityBuckets func(work []map[string]any, now time.Time, verification func(id string) any) (working, finished []any)
	// RootActivity reads, in one scan of the ledger, what the plan view takes
	// from the records of root's work: see RootActivity and RecentWorkPage.
	RootActivity func(ctx context.Context, tx pgx.Tx, root string, now time.Time, page RecentWorkPage) (RootActivity, error)
	// HasWork says whether any work was ever recorded under root.
	HasWork func(ctx context.Context, tx pgx.Tx, root string) (bool, error)
	// CollectorActorPrefix starts a host collector's actor, a thread that is
	// never unregistered work (the plan view's coverage count). It is the
	// external work's word, passed in by the binding so the plan holds no
	// copy of it.
	CollectorActorPrefix string
}

var externalWorkPorts atomic.Pointer[ExternalWorkPorts]

// BindPlanExternalWork binds the external work's side. It is called once,
// from the init that wires the providers outside package store (store's
// portsbind.go).
func BindPlanExternalWork(p ExternalWorkPorts) { externalWorkPorts.Store(&p) }

func checkExternalNote(record any) error {
	return checkExternalNoteVia(externalWorkPorts.Load(), record)
}

func checkExternalNoteVia(p *ExternalWorkPorts, record any) error {
	if p == nil || p.CheckNote == nil {
		return unbound("the check of an external work note", "BindPlanExternalWork")
	}
	return p.CheckNote(record)
}

func checkExternalDescription(raw []byte) error {
	return checkExternalDescriptionVia(externalWorkPorts.Load(), raw)
}

func checkExternalDescriptionVia(p *ExternalWorkPorts, raw []byte) error {
	if p == nil || p.CheckDescription == nil {
		return unbound("the check of an external work description", "BindPlanExternalWork")
	}
	return p.CheckDescription(raw)
}

func externalActivityBuckets(work []map[string]any, now time.Time, verification func(string) any) (working, finished []any, err error) {
	return externalActivityBucketsVia(externalWorkPorts.Load(), work, now, verification)
}

func externalActivityBucketsVia(p *ExternalWorkPorts, work []map[string]any, now time.Time, verification func(string) any) (working, finished []any, err error) {
	if p == nil || p.ActivityBuckets == nil {
		return nil, nil, unbound("the buckets of a plan's external work", "BindPlanExternalWork")
	}
	working, finished = p.ActivityBuckets(work, now, verification)
	return working, finished, nil
}

func externalRootActivity(ctx context.Context, tx pgx.Tx, root string, now time.Time, page RecentWorkPage) (RootActivity, error) {
	return externalRootActivityVia(ctx, externalWorkPorts.Load(), tx, root, now, page)
}

func externalRootActivityVia(ctx context.Context, p *ExternalWorkPorts, tx pgx.Tx, root string, now time.Time, page RecentWorkPage) (RootActivity, error) {
	if p == nil || p.RootActivity == nil {
		return RootActivity{}, unbound("the activity of a plan's external work", "BindPlanExternalWork")
	}
	return p.RootActivity(ctx, tx, root, now, page)
}

func externalHasWork(ctx context.Context, tx pgx.Tx, root string) (bool, error) {
	return externalHasWorkVia(ctx, externalWorkPorts.Load(), tx, root)
}

func externalHasWorkVia(ctx context.Context, p *ExternalWorkPorts, tx pgx.Tx, root string) (bool, error) {
	if p == nil || p.HasWork == nil {
		return false, unbound("whether a plan has external work", "BindPlanExternalWork")
	}
	return p.HasWork(ctx, tx, root)
}

// collectorActorPrefix is the bound prefix of a host collector's actor, or
// the refusal of the plan view when it is not bound.
func collectorActorPrefix() (string, error) {
	return collectorActorPrefixVia(externalWorkPorts.Load())
}

func collectorActorPrefixVia(p *ExternalWorkPorts) (string, error) {
	if p == nil || p.CollectorActorPrefix == "" {
		return "", unbound("the prefix of a collector's actor", "BindPlanExternalWork")
	}
	return p.CollectorActorPrefix, nil
}

// RecorderImportPorts is what the plan code needs of the recorder import
// (lineage), whose proposals the plan's decision pass decides: the landing of
// its log under the decision. Whether a proposal is one is the plan's own
// (recorderpropose.go, opRecorderImport).
type RecorderImportPorts struct {
	// Decide lands the log in tx under the decision decided.
	Decide func(ctx context.Context, tx pgx.Tx, log string, decided int64) error
}

var recorderImportPorts atomic.Pointer[RecorderImportPorts]

// BindPlanRecorderImport binds the recorder import's side. It is called
// once, from the store's wiring (portsbind.go).
func BindPlanRecorderImport(p RecorderImportPorts) { recorderImportPorts.Store(&p) }

// recorderImportLog is the kept log of a proposal that is a recorder import.
// It never fails now (the plan answers it itself); the error stays for its
// callers.
func recorderImportLog(changes []PlanChange) (log string, is bool, err error) {
	log, is = recorderImportOf(changes)
	return log, is, nil
}

func decideRecorderImportLog(ctx context.Context, tx pgx.Tx, log string, decided int64) error {
	return decideRecorderImportLogVia(ctx, recorderImportPorts.Load(), tx, log, decided)
}

func decideRecorderImportLogVia(ctx context.Context, p *RecorderImportPorts, tx pgx.Tx, log string, decided int64) error {
	if p == nil || p.Decide == nil {
		return unbound("the landing of a recorder import", "BindPlanRecorderImport")
	}
	return p.Decide(ctx, tx, log, decided)
}

// The recorder import's words as the progress projection shows them: its
// card's two options and what starts the stream a log is imported into
// (store's recorderimport.go: RecorderApply, RecorderRefuse; externalwork's
// recorder.go: RecorderStream). A test holds each equal to the recorder
// import's own.
const (
	recorderApplyLabel   = "Import the log"
	recorderRefuseLabel  = "Do not import it"
	recorderStreamPrefix = "legacy:recorder:"
)

// CoordinationPorts is what the plan code needs of coordination: the record
// of a text that claims the Owner's approval.
type CoordinationPorts struct {
	// AlertClaim records a policy_alert on ev's stream, caused by cause,
	// when body claims the Owner's approval, and nothing when it does not.
	// source is where the text came in; artifact is the text's artifact, ""
	// for a text that is in the event the alert is caused by.
	AlertClaim func(ctx context.Context, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error
}

var coordinationPorts atomic.Pointer[CoordinationPorts]

// BindPlanCoordination binds coordination's side. It is called once, from
// the init that wires the providers outside package store (store's
// portsbind.go).
func BindPlanCoordination(p CoordinationPorts) { coordinationPorts.Store(&p) }

func alertClaim(ctx context.Context, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error {
	return alertClaimVia(ctx, coordinationPorts.Load(), tx, ev, cause, source, artifact, body)
}

func alertClaimVia(ctx context.Context, p *CoordinationPorts, tx pgx.Tx, ev eventlog.Event, cause int64, source, artifact string, body []byte) error {
	if p == nil || p.AlertClaim == nil {
		return unbound("the record of a text that claims an approval", "BindPlanCoordination")
	}
	return p.AlertClaim(ctx, tx, ev, cause, source, artifact, body)
}

// QuestionPorts is what the plan code needs of the Owner's question store:
// the size it will count a card's body as, asked before the card is.
type QuestionPorts struct {
	// BodyBytes is the size of a body as the question store checks it.
	BodyBytes func(body string) int
	// BodyLimit is the most a question's body holds, in the bytes BodyBytes
	// counts (questions.BodyLimit).
	BodyLimit int
}

var questionPorts atomic.Pointer[QuestionPorts]

// BindPlanQuestions binds the question store's side. It is called once, from
// the init that wires the providers outside package store (store's
// portsbind.go).
func BindPlanQuestions(p QuestionPorts) { questionPorts.Store(&p) }

func cardBodyBytesVia(p *QuestionPorts, body string) (int, error) {
	if p == nil || p.BodyBytes == nil {
		return 0, unbound("the size the question store counts a card's body as", "BindPlanQuestions")
	}
	return p.BodyBytes(body), nil
}

// cardBodyFits checks a card's body against the question store's limit, as
// that store counts it: the size and the limit, an error when either is not
// bound.
func cardBodyFits(body string) (n, limit int, err error) {
	p := questionPorts.Load()
	if n, err = cardBodyBytesVia(p, body); err != nil {
		return 0, 0, err
	}
	if p.BodyLimit <= 0 {
		return 0, 0, unbound("the most a question's body holds", "BindPlanQuestions")
	}
	return n, p.BodyLimit, nil
}

// The question store's word the plan code reads as its own constant: the
// channel of an answer a rule gave (questions.RuleChannel). A test holds it
// equal to the question store's. (The body limit comes through QuestionPorts
// since 2026-11-17.)
const ruleAnswerChannel = "rule"

// LoopPorts is what the plan code needs of the controller's loop alerts (the
// store's loopalert.go, now in foundation's agentrt/loopalert): the loops failing
// now, which Progress says of the platform itself.
type LoopPorts struct {
	// Failing lists the controller loops whose latest record is an alert no
	// note has answered, oldest alert first.
	Failing func(ctx context.Context, tx pgx.Tx) ([]map[string]any, error)
}

var loopPorts atomic.Pointer[LoopPorts]

// BindPlanLoops binds the loop alerts' side. It is called once, from the
// init of their file (store's loopalert.go).
func BindPlanLoops(p LoopPorts) { loopPorts.Store(&p) }

func controllerLoopsFailing(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	return controllerLoopsFailingVia(ctx, loopPorts.Load(), tx)
}

func controllerLoopsFailingVia(ctx context.Context, p *LoopPorts, tx pgx.Tx) ([]map[string]any, error) {
	if p == nil || p.Failing == nil {
		return nil, unbound("the controller loops failing now", "BindPlanLoops")
	}
	return p.Failing(ctx, tx)
}
