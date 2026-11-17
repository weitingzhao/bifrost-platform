package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// The plan code's ports: what the plan files (plan*.go, cardkinds.go, the
// step facts and sign-off, progress*.go) need of another domain, declared
// here as small function types over the bases' and the plan's own types. The
// domain that provides one binds it at start (a Bind function per providing
// domain, called from an init of the provider's side); the plan files name
// the port and not the other domain's package or files, so the plan code can
// leave this package for one of the plan domain alone.
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
// the rules' files (envelope.go).
func BindPlanRules(p RulePorts) { rulePorts.Store(&p) }

func PlanRuleInForce(ctx context.Context, tx pgx.Tx) (*PlanRule, error) {
	return planRuleVia(ctx, rulePorts.Load(), tx)
}

func planRuleVia(ctx context.Context, p *RulePorts, tx pgx.Tx) (*PlanRule, error) {
	if p == nil || p.PlanRuleInForce == nil {
		return nil, unbound("the plan-standing rule in force", "BindPlanRules")
	}
	return p.PlanRuleInForce(ctx, tx)
}

func CardAutoKindsInForce(ctx context.Context, tx pgx.Tx) ([]string, error) {
	return cardAutoKindsVia(ctx, rulePorts.Load(), tx)
}

func cardAutoKindsVia(ctx context.Context, p *RulePorts, tx pgx.Tx) ([]string, error) {
	if p == nil || p.CardAutoKindsInForce == nil {
		return nil, unbound("the card-auto-answer rule in force", "BindPlanRules")
	}
	return p.CardAutoKindsInForce(ctx, tx)
}

// ExternalWorkPorts is what the plan code needs of registered external work
// (lineage): the check of its two documents where the plan code checks every
// note and every view artifact, and the read of a root's registered work for
// the plan view.
type ExternalWorkPorts struct {
	// CheckNote holds the external_work record of a note to its shape.
	CheckNote func(record any) error
	// CheckDescription holds an artifact that says it is an external work
	// description to being one.
	CheckDescription func(raw []byte) error
	// Snapshot reads the work registered under root, each with its
	// description, inside the caller's snapshot.
	Snapshot func(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error)
	// ActivityBuckets marks each work with its verification and gives the
	// ones working now and the ones finished today.
	ActivityBuckets func(work []map[string]any, now time.Time, verification func(id string) any) (working, finished []any)
}

var externalWorkPorts atomic.Pointer[ExternalWorkPorts]

// BindPlanExternalWork binds the external work's side. It is called once,
// from the init that wires the packages already outside this one
// (portsbind.go).
func BindPlanExternalWork(p ExternalWorkPorts) { externalWorkPorts.Store(&p) }

func CheckExternalNote(record any) error {
	return checkExternalNoteVia(externalWorkPorts.Load(), record)
}

func checkExternalNoteVia(p *ExternalWorkPorts, record any) error {
	if p == nil || p.CheckNote == nil {
		return unbound("the check of an external work note", "BindPlanExternalWork")
	}
	return p.CheckNote(record)
}

func CheckExternalDescription(raw []byte) error {
	return checkExternalDescriptionVia(externalWorkPorts.Load(), raw)
}

func checkExternalDescriptionVia(p *ExternalWorkPorts, raw []byte) error {
	if p == nil || p.CheckDescription == nil {
		return unbound("the check of an external work description", "BindPlanExternalWork")
	}
	return p.CheckDescription(raw)
}

func ExternalWorkSnapshot(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	return externalWorkSnapshotVia(ctx, externalWorkPorts.Load(), tx, root, now)
}

func externalWorkSnapshotVia(ctx context.Context, p *ExternalWorkPorts, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	if p == nil || p.Snapshot == nil {
		return nil, unbound("the read of a plan's registered external work", "BindPlanExternalWork")
	}
	return p.Snapshot(ctx, tx, root, now)
}

func ExternalActivityBuckets(work []map[string]any, now time.Time, verification func(string) any) (working, finished []any, err error) {
	return externalActivityBucketsVia(externalWorkPorts.Load(), work, now, verification)
}

func externalActivityBucketsVia(p *ExternalWorkPorts, work []map[string]any, now time.Time, verification func(string) any) (working, finished []any, err error) {
	if p == nil || p.ActivityBuckets == nil {
		return nil, nil, unbound("the buckets of a plan's external work", "BindPlanExternalWork")
	}
	working, finished = p.ActivityBuckets(work, now, verification)
	return working, finished, nil
}

// CollectorActorPrefix starts a host collector's actor, a thread that is
// never unregistered work (the plan view's coverage count). It is the
// external work's word (externalwork.CollectorActorPrefix); a test holds the
// two equal.
const CollectorActorPrefix = "thread:collector@"

// RecorderImportPorts is what the plan code needs of the recorder import
// (lineage), whose proposals the plan's decision pass decides: whether a
// proposal is one, and the landing of its log under the decision.
type RecorderImportPorts struct {
	// LogOf is the kept log of a proposal that is a recorder import.
	LogOf func(changes []PlanChange) (log string, is bool)
	// Decide lands the log in tx under the decision decided.
	Decide func(ctx context.Context, tx pgx.Tx, log string, decided int64) error
}

var recorderImportPorts atomic.Pointer[RecorderImportPorts]

// BindPlanRecorderImport binds the recorder import's side. It is called
// once, from the init of its file (recorderimport.go).
func BindPlanRecorderImport(p RecorderImportPorts) { recorderImportPorts.Store(&p) }

func RecorderImportLog(changes []PlanChange) (log string, is bool, err error) {
	return recorderImportLogVia(recorderImportPorts.Load(), changes)
}

func recorderImportLogVia(p *RecorderImportPorts, changes []PlanChange) (log string, is bool, err error) {
	if p == nil || p.LogOf == nil {
		return "", false, unbound("whether a proposal is a recorder import", "BindPlanRecorderImport")
	}
	log, is = p.LogOf(changes)
	return log, is, nil
}

func DecideRecorderImportLog(ctx context.Context, tx pgx.Tx, log string, decided int64) error {
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
// (recorderimport.go: RecorderApply, RecorderRefuse, RecorderStream). A test
// holds each equal to the recorder import's own.
const (
	RecorderApplyLabel   = "Import the log"
	RecorderRefuseLabel  = "Do not import it"
	RecorderStreamPrefix = "legacy:recorder:"
)

// CoordinationPorts is what the plan code needs of coordination: the record
// of a text that claims the Owner's approval.
type CoordinationPorts struct {
	// AlertClaim records a policy_alert on ev's stream, caused by cause,
	// when body claims the Owner's approval, and nothing when it does not.
	// source is where the text came in; artifact is the text's artifact, ""
	// for a text that is in the event the alert is caused by.
	AlertClaim func(ctx context.Context, tx pgx.Tx, ev Event, cause int64, source, artifact string, body []byte) error
}

var coordinationPorts atomic.Pointer[CoordinationPorts]

// BindPlanCoordination binds coordination's side. It is called once, from
// the init that wires the packages already outside this one (portsbind.go).
func BindPlanCoordination(p CoordinationPorts) { coordinationPorts.Store(&p) }

func AlertClaim(ctx context.Context, tx pgx.Tx, ev Event, cause int64, source, artifact string, body []byte) error {
	return alertClaimVia(ctx, coordinationPorts.Load(), tx, ev, cause, source, artifact, body)
}

func alertClaimVia(ctx context.Context, p *CoordinationPorts, tx pgx.Tx, ev Event, cause int64, source, artifact string, body []byte) error {
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
}

var questionPorts atomic.Pointer[QuestionPorts]

// BindPlanQuestions binds the question store's side. It is called once, from
// the init that wires the packages already outside this one (portsbind.go).
func BindPlanQuestions(p QuestionPorts) { questionPorts.Store(&p) }

func CardBodyBytes(body string) (int, error) { return cardBodyBytesVia(questionPorts.Load(), body) }

func cardBodyBytesVia(p *QuestionPorts, body string) (int, error) {
	if p == nil || p.BodyBytes == nil {
		return 0, unbound("the size the question store counts a card's body as", "BindPlanQuestions")
	}
	return p.BodyBytes(body), nil
}

// The question store's two words the plan code reads, as the plan's own
// constants: the most a question's body holds, in the bytes BodyBytes counts
// (questions.BodyLimit), and the channel of an answer a rule gave
// (questions.RuleChannel). A test holds each equal to the question store's.
const (
	QuestionBodyLimit = 3000
	RuleAnswerChannel = "rule"
)

// LoopPorts is what the plan code needs of the controller's loop alerts (the
// store's loopalert.go, over foundation's loopreadings): the loops failing
// now, which Progress says of the platform itself.
type LoopPorts struct {
	// Failing lists the controller loops whose latest record is an alert no
	// note has answered, oldest alert first.
	Failing func(ctx context.Context, tx pgx.Tx) ([]map[string]any, error)
}

var loopPorts atomic.Pointer[LoopPorts]

// BindPlanLoops binds the loop alerts' side. It is called once, from the
// init of their file (loopalert.go).
func BindPlanLoops(p LoopPorts) { loopPorts.Store(&p) }

func ControllerLoopsFailing(ctx context.Context, tx pgx.Tx) ([]map[string]any, error) {
	return controllerLoopsFailingVia(ctx, loopPorts.Load(), tx)
}

func controllerLoopsFailingVia(ctx context.Context, p *LoopPorts, tx pgx.Tx) ([]map[string]any, error) {
	if p == nil || p.Failing == nil {
		return nil, unbound("the controller loops failing now", "BindPlanLoops")
	}
	return p.Failing(ctx, tx)
}
