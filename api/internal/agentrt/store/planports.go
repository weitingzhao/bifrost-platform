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

func externalWorkSnapshot(ctx context.Context, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	return externalWorkSnapshotVia(ctx, externalWorkPorts.Load(), tx, root, now)
}

func externalWorkSnapshotVia(ctx context.Context, p *ExternalWorkPorts, tx pgx.Tx, root string, now time.Time) ([]map[string]any, error) {
	if p == nil || p.Snapshot == nil {
		return nil, unbound("the read of a plan's registered external work", "BindPlanExternalWork")
	}
	return p.Snapshot(ctx, tx, root, now)
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

// collectorActorPrefix starts a host collector's actor, a thread that is
// never unregistered work (the plan view's coverage count). It is the
// external work's word (externalwork.CollectorActorPrefix); a test holds the
// two equal.
const collectorActorPrefix = "thread:collector@"

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

func recorderImportLog(changes []PlanChange) (log string, is bool, err error) {
	return recorderImportLogVia(recorderImportPorts.Load(), changes)
}

func recorderImportLogVia(p *RecorderImportPorts, changes []PlanChange) (log string, is bool, err error) {
	if p == nil || p.LogOf == nil {
		return "", false, unbound("whether a proposal is a recorder import", "BindPlanRecorderImport")
	}
	log, is = p.LogOf(changes)
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
// (recorderimport.go: RecorderApply, RecorderRefuse, RecorderStream). A test
// holds each equal to the recorder import's own.
const (
	recorderApplyLabel   = "Import the log"
	recorderRefuseLabel  = "Do not import it"
	recorderStreamPrefix = "legacy:recorder:"
)
