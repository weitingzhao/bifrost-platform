package seenfacts

import (
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The facts this package records are its own event types (registered under
// foundation in bus/registry/ledger/types.yaml): what the platform saw of an
// approval, a question, a pipeline run, a CI result and a deployment. It
// declares them here (moved from store/vocab.go on 2026-11-17), so every
// process that records a fact declares what it records.

// seenTypes are the facts this package records.
var seenTypes = []string{"approval_seen", "ci_seen", "deploy_seen", "pipeline_seen", "question_seen"}

func field(name string, kind eventlog.Kind, optional bool) eventlog.Field {
	return eventlog.Field{Name: name, Kind: kind, Optional: optional}
}

var seenBodies = map[string][]eventlog.Field{
	"approval_seen": {field("approval", eventlog.Int, false), field("status", eventlog.Str, false), field("action", eventlog.Str, false),
		field("tier", eventlog.Str, false), field("created_at", eventlog.Str, false), field("expired", eventlog.Bool, false),
		field("env", eventlog.Str, true), field("repo", eventlog.Str, true), field("task", eventlog.Str, true), field("op", eventlog.Str, true),
		field("decided_at", eventlog.Str, true)},
	"question_seen": {field("question", eventlog.ID, false), field("status", eventlog.Str, false), field("option", eventlog.Str, true),
		field("text", eventlog.Str, true), field("answered_by", eventlog.Str, true), field("answered_at", eventlog.Str, true),
		field("recommended", eventlog.Str, true), field("number", eventlog.Int, true), field("created_at", eventlog.Str, true),
		field("owner_duty", eventlog.Str, true), field("task", eventlog.Str, true), field("channel", eventlog.Str, true),
		field("withdrawn_at", eventlog.Str, true), field("withdrawn_by", eventlog.Str, true), field("expired_at", eventlog.Str, true)},
	"pipeline_seen": {field("run", eventlog.Str, false), field("uid", eventlog.Str, false), field("pipeline", eventlog.Str, false),
		field("kind", eventlog.Str, false), field("created_at", eventlog.Str, false), field("started_at", eventlog.Str, false),
		field("completed_at", eventlog.Str, false), field("result", eventlog.Str, false), field("repo", eventlog.Str, true),
		field("revision", eventlog.Str, true), field("reason", eventlog.Str, true), field("tree", eventlog.Str, true),
		field("tested_by", eventlog.Str, true), field("landed_commit", eventlog.Str, true), field("change_ids", eventlog.Str, true),
		field("outcome", eventlog.Str, true), field("refusal", eventlog.Str, true)},
	"ci_seen":     {field("repo", eventlog.Str, false), field("commit", eventlog.Str, false), field("status", eventlog.Str, false), field("run", eventlog.Str, true)},
	"deploy_seen": {field("env", eventlog.Str, false), field("commit", eventlog.Str, false), field("deploy_ref", eventlog.Any, false)},
}

var (
	seenCommit  = eventlog.Shaped("a commit (40 hex)", regexp.MustCompile(`^[0-9a-f]{40}$`))
	seenRunName = eventlog.Shaped("a PipelineRun name", regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`))
	seenInstant = eventlog.Value(eventlog.Instant)
)

var seenValues = map[string]map[string]eventlog.Value{
	"approval_seen": {"created_at": seenInstant, "decided_at": seenInstant},
	"question_seen": {"answered_at": seenInstant, "created_at": seenInstant, "withdrawn_at": seenInstant, "expired_at": seenInstant},
	"pipeline_seen": {"tree": seenCommit, "tested_by": seenRunName, "created_at": seenInstant, "started_at": seenInstant, "completed_at": seenInstant,
		"kind": eventlog.OneOf("ci", "delivery", "other"), "result": eventlog.OneOf("Succeeded", "Failed"),
		"reason": eventlog.Shaped("a reason", regexp.MustCompile(`^[A-Za-z]{1,64}$`))},
	"ci_seen":     {"commit": seenCommit},
	"deploy_seen": {"env": eventlog.OneOf("dev", "stg", "prod"), "commit": seenCommit},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{Types: seenTypes, Bodies: seenBodies, Values: seenValues})
}
