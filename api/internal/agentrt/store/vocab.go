package store

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/loopalert"
	"regexp"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
)

// The kinds, fields and value checks of the vocabulary are the event log's
// (eventlog/vocabulary.go); these are its names as this package writes them.
type (
	kind  = eventlog.Kind
	value = eventlog.Value
)

// field is one field of a body as this package's tables write it; the
// declaration hands them to the event log as its Fields (declaredBodies).
type field struct {
	name     string
	kind     kind
	optional bool
}

func declaredBodies(in map[string][]field) map[string][]eventlog.Field {
	out := make(map[string][]eventlog.Field, len(in))
	for t, spec := range in {
		fields := make([]eventlog.Field, 0, len(spec))
		for _, f := range spec {
			fields = append(fields, eventlog.Field{Name: f.name, Kind: f.kind, Optional: f.optional})
		}
		out[t] = fields
	}
	return out
}

const (
	kStr  = eventlog.Str
	kID   = eventlog.ID
	kInt  = eventlog.Int
	kIDs  = eventlog.IDs
	kAny  = eventlog.Any
	kBool = eventlog.Bool
)

// eventTypes is the closed vocabulary, as the database's event_type_closed
// constraint has it (migrations 0009, 0013); TestEventTypesAreTheDatabases holds
// the two together. bodies below covers only the types the store writes.
var eventTypes = map[string]bool{"attempt_leased": true,
	"attempt_started": true, "attempt_progress": true, "attempt_checkpointed": true, "attempt_stale": true,
	"attempt_renewed": true, "attempt_computed": true, "attempt_persisted": true, "attempt_submitted": true,
	"attempt_lost": true, "attempt_ended": true, "attempt_salvaged": true, "attempt_cancelled": true,
	"artifact_put": true, "verification_recorded": true, "owner_wait_opened": true, "owner_wait_closed": true,
	"approval_seen": true, "question_seen": true, "ci_seen": true, "pipeline_seen": true, "deploy_seen": true, "actor_registered": true, "host_registered": true, "host_state_changed": true,
	"host_retired": true, "rule_proposed": true, "rule_signed": true,
	"rule_tightened": true, "rule_expired": true, "rule_approved": true, "capability_scored": true, "candidate_status_changed": true,
	"budget_charged": true, "pool_cooldown_set": true, "pool_measured": true,
	"policy_alert": true, "note": true, "wait_limit_passed": true,
	"legacy_imported":  true,
	"host_slots_set":   true,
	"metrics_computed": true, "metric_label_set": true, "ci_reused": true, "ci_red_classified": true,
}

// IsEventType says whether t is an event type of the closed vocabulary.
func IsEventType(t string) bool { return eventlog.IsEventType(t) }

// bodies is the closed body of each event type the store writes so far
// (design v5 §5.1.3): every required field present with a value of its kind,
// no other field. The database checks the type against the full vocabulary;
// a type with no body here is refused rather than written with a guessed shape.
var bodies = map[string][]field{
	// Delivery measurement records (0016); CI fact bodies arrive in wave M-b.
	"metrics_computed": {{"source", kStr, false}, {"rows", kInt, false}, {"definitions", kAny, false}, {"windows", kAny, true}, {"label", kStr, true}, {"artifact_id", kID, true}, {"xmin", kInt, true}},

	"actor_registered": {{"actors", kIDs, false}, {"capability_facts", kIDs, true}, {"note", kStr, true}, {"token", kStr, true}},
	"artifact_put":     {{"artifact_id", kID, false}, {"artifact_kind", kStr, false}, {"size_bytes", kInt, false}, {"uploaded_by", kStr, false}},
	// policy_alert: a text claiming the Owner's approval (approval_claim, from
	// a task's spec or a message: the closed phrase it holds), or a line the
	// chief of staff ended an answer or a plan with (seat_alert, the seat's
	// protocol v2: its phrase is POLICY_ALERT and its words stay in the report
	// the artifact names; line_digest tells one line from another), or the
	// platform's refusal of a report agentd uploaded, on the host's stream
	// (local_path, workspace_ref: the rule that refused it, source the
	// report's kind, artifact_id the refused body's id, which is not
	// stored), or an executor
	// that failed a task failuresToStop times in a row (repeated_failure,
	// source attempt, no artifact: the event's attempt is the last failure;
	// TD-381, TD-382), or a controller loop that failed the same way
	// FailuresToAlert times in a row (controller_failing, source controller,
	// on the controller stream: line_digest names the loop, the artifact
	// holds its error; TD-411, loopalert.go), or a host whose claim reported
	// free disk below the floor (disk_low, source controller, on the host's
	// stream: the artifact says how much and the floor; TD-415, hostdisk.go),
	// or a host that stopped polling (host_offline, source controller, on the
	// host's presence stream) or an executor that cannot take work now
	// (executor_unavailable, source controller, on the executor's presence
	// stream), or host presence that cannot be judged (presence_unjudged, on
	// the presence:judging stream) or whose judgment is deferred
	// (presence_deferred, on presence:deferred); the artifact says when and
	// why (W-31 3.5, presencepass.go, presencejudge.go).
	// Every rule but repeated_failure
	// names its artifact (checkBody), but an approval claim in a plan note
	// (source plan_note, review 489 B3), whose text is in the note it is
	// caused by.
	"policy_alert": {{"rule", kStr, false}, {"source", kStr, false}, {"phrase", kStr, false}, {"artifact_id", kID, true}, {"message_id", kID, true},
		{"line_digest", kStr, true}},
	// legacy_imported: the head of an import (source names what it came from,
	// count how many it holds) or, on a recorder log's stream, one line of
	// the log (source recorder; the line's seq, its original digest, the
	// body as kept, masked when its local paths were; recorderimport.go).
	"legacy_imported": {{"source", kStr, false}, {"count", kInt, true}, {"work_id", kStr, true}, {"seq", kInt, true},
		{"digest", kStr, true}, {"record", kStr, true}, {"masked", kBool, true}},
	"rule_signed":    {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"job_ids", kIDs, true}, {"job_candidate_ids", kIDs, true}, {"capability_facts", kIDs, true}},
	"rule_tightened": {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"capability_facts", kIDs, true}},
	// rule_approved: a capacity change the Owner approved on a card
	// (ruleapprove.go): the approval, the draft's artifact and each field it
	// changes with its old and new value.
	"rule_approved": {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"approval_id", kID, false}, {"text_artifact_id", kID, false}, {"changes", kAny, false}},
	"rule_proposed": {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"direction", kStr, false}, {"text_artifact_id", kID, false}, {"reason", kStr, false}, {"token", kStr, true}},
	"pipeline_seen": {{"run", kStr, false}, {"uid", kStr, false}, {"pipeline", kStr, false}, {"kind", kStr, false}, {"created_at", kStr, false}, {"started_at", kStr, false}, {"completed_at", kStr, false}, {"result", kStr, false}, {"repo", kStr, true}, {"revision", kStr, true}, {"reason", kStr, true}, {"tree", kStr, true}, {"tested_by", kStr, true}, {"landed_commit", kStr, true}, {"change_ids", kStr, true}, {"outcome", kStr, true}},
	"approval_seen": {{"approval", kInt, false}, {"status", kStr, false}, {"action", kStr, false}, {"tier", kStr, false}, {"created_at", kStr, false}, {"expired", kBool, false}, {"env", kStr, true}, {"repo", kStr, true}, {"task", kStr, true}, {"op", kStr, true}, {"decided_at", kStr, true}},
	"question_seen": {{"question", kID, false}, {"status", kStr, false}, {"option", kStr, true}, {"text", kStr, true}, {"answered_by", kStr, true}, {"answered_at", kStr, true}, {"recommended", kStr, true}, {"number", kInt, true}, {"created_at", kStr, true}, {"owner_duty", kStr, true}, {"task", kStr, true}, {"channel", kStr, true}, {"withdrawn_at", kStr, true}, {"withdrawn_by", kStr, true}, {"expired_at", kStr, true}},
	"ci_seen":       {{"repo", kStr, false}, {"commit", kStr, false}, {"status", kStr, false}, {"run", kStr, true}},
	"deploy_seen":   {{"env", kStr, false}, {"commit", kStr, false}, {"deploy_ref", kAny, false}},
	// note: landed, the commit an implementation's change first stood on main
	// as, when the controller first saw it there, with landed_via (its
	// route), matched_by (how main names it), landed_result (the pushed
	// commit matched; none for a trailer), landed_repo and landed_tip (main's head);
	// pruned, an attempt branch its host deleted; prune_refused, one Gitea
	// would not let it delete; prune_failed, one it could not reach Gitea to
	// delete (prune.go).
	// On an item's assessment stream (assess.go, TD-391): assessment_requested,
	// the Owner's opt-in naming the kind and id (TD-463); ask, the option
	// keys and refs its spec gave; assessment, what the seat handed in, its
	// refs resolved; unavailable, why there is none. On the exams stream,
	// caused by a paper's issue (examvoid.go, TD-407): voided, the approval
	// that voided the paper, which no run was handed in for.
	"note": {{"text", kStr, false}, {"plan_observation", kAny, true}, {"plan_observation_pass", kAny, true}, {"manual", kStr, true}, {"evidence", kIDs, true}, {"landed", kStr, true},
		{"landed_via", kStr, true}, {"matched_by", kStr, true}, {"landed_result", kStr, true}, {"landed_repo", kStr, true}, {"landed_tip", kStr, true},
		{"pruned", kStr, true}, {"prune_refused", kStr, true}, {"prune_failed", kStr, true},
		{"assessment_requested", kAny, true}, {"ask", kAny, true}, {"assessment", kAny, true}, {"unavailable", kStr, true}, {"voided", kStr, true},
		// card: kept for the notes already written; business issues, which
		// wrote it, were removed (0022).
		{"card", kAny, true}, {"plan_view", kAny, true}, {"external_work", kAny, true},
		{"leak_value", kAny, true},
		// full_ci: on the controller's stream, the daily full CI runs started
		// and how a red one was handled (agentrt/fullci).
		{"full_ci", kAny, true},
		// check_value: on the controller's stream, the goal proposed to
		// delete a check or retry the check value table marked (checkvalue.go).
		{"check_value", kAny, true},
		// offered, disk_below: on a host's offers stream, what its claim
		// offers (executor, provider, model each) and whether the free disk
		// it reports is below the floor, when either changed (presence.go,
		// W-31 3.5).
		{"offered", kAny, true}, {"disk_below", kBool, true},
		// agentd_build: on a host's build stream, the agentd build its
		// claims report (commit, sha256, modified), when it changed
		// (presence.go).
		{"agentd_build", kAny, true},
		// headroom: on a host's headroom stream, the load and memory its
		// claim reports, integers only, when it moved or got old
		// (slotsignals.go, R5 phase 2 step 3b).
		{"headroom", kAny, true},
		// prune_round: on a host's prune stream, a round of deleting the
		// attempt branches it was handed, opened or closed (presencelive.go,
		// review 526 A1).
		{"prune_round", kStr, true}},
	// owner_wait_opened, owner_wait_closed: their bodies and values are
	// declared by package questioncards (blockedcard.go), which took them
	// with the outcomes they name; the two type names stay in eventTypes.
}

func checkBody(eventType string, body map[string]any) error {
	return eventlog.CheckBody(eventType, body)
}

// The store's declaration: its types, their bodies and values, and the
// checks a type has of its own. The coordination types are declared by
// package coord, which took them with it; each domain's will go the same way.
func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  keysOf(eventTypes),
		Bodies: declaredBodies(bodies),
		Values: fieldValues,
		WholeValues: map[string]map[string]func(any) string{
			"note": {"assessment_requested": assessmentreadings.BadAssessmentRequest, "agentd_build": badAgentdBuild},
		},
		Before: map[string]func(map[string]any) error{
			"metrics_computed": checkMetricDefinitions,
			"note":             planstore.CheckViewNote,
		},
		After: map[string]func(map[string]any) error{
			"policy_alert": checkPolicyAlert,
		},
		Events: []func(Event) error{retiredRecord},
	})
}

func checkMetricDefinitions(body map[string]any) error {
	var defs []MetricDefinition
	if err := strictViewValue(body["definitions"], &defs); err != nil {
		return invalid("metrics_computed definitions: %v", err)
	}
	if defs == nil {
		return invalid("metrics_computed definitions must be an array")
	}
	for _, d := range defs {
		if !metricIDRE.MatchString(d.ID) || d.Version < 1 || hex64(d.SHA256) != "" {
			return invalid("invalid metric definition")
		}
	}
	return nil
}

func checkPolicyAlert(body map[string]any) error {
	if _, named := body["artifact_id"]; !named && body["rule"] != "repeated_failure" &&
		(body["rule"] != "approval_claim" || body["source"] != "plan_note") {
		return fmt.Errorf("store: policy_alert needs \"artifact_id\"")
	}
	return nil
}

// Values (review 145 B6): what a field may hold beyond its kind. An enum's
// values are the states and kinds the schema's checks allow (0001_core.sql),
// taken from the state machines here where there is one; an id has the shape
// ids.go mints; a commit is 40 lower-case hex digits; an instant is RFC 3339.
// A value refused here is the request's (ErrInvalid). A field not named here
// takes any value of its kind: free text; the states of what is outside the
// runtime (an approval's, a question's, a CI run's), which another system
// names; a control's id, which fixtures name freely and no table checks; and
// a value its row checks in the same transaction (a task's base commit, an
// attempt's pushed commit and branch), where the row's constraint, which the
// routes report as such, decides.

// oneOf is an enum (eventlog.OneOf).
func oneOf(vals ...string) value { return eventlog.OneOf(vals...) }

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// shaped is an id or a reference of a fixed shape (eventlog.Shaped).
func shaped(name string, re *regexp.Regexp) value { return eventlog.Shaped(name, re) }

func instant(s string) string { return eventlog.Instant(s) }

var (
	ulid        = `[0-9A-HJKMNP-TV-Z]{26}`
	taskID      = shaped("a task id", regexp.MustCompile(`^t_`+ulid+`$`))
	attemptID   = shaped("an attempt id", regexp.MustCompile(`^a_`+ulid+`$`))
	messageID   = shaped("a message id", regexp.MustCompile(`^m_`+ulid+`$`))
	verifyID    = shaped("a verification id", regexp.MustCompile(`^v_`+ulid+`$`))
	artifactID  = shaped("an artifact id (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	commit      = shaped("a commit (40 hex)", regexp.MustCompile(`^[0-9a-f]{40}$`))
	agentBranch = shaped("an attempt branch (refs/heads/agent/<task>/a<n>)", regexp.MustCompile(`^refs/heads/agent/(t_[0-9A-HJKMNP-TV-Z]{26})/a([1-9][0-9]{0,5})$`))
	fingerprint = shaped("a fingerprint (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	executorID  = shaped("an executor (exec:<tool>)", regexp.MustCompile(`^exec:[a-z]+$`))
	approvalRef = shaped("an approval (appr_<16 hex>)", approvalIDRE)
)

// fieldValues are the checked fields, by event type and field.
var fieldValues = map[string]map[string]value{
	"metrics_computed": {"source": oneOf("computed", "imported"), "label": metricLabel, "artifact_id": artifactID},
	"artifact_put":     {"artifact_id": artifactID, "artifact_kind": oneOf(keysOf(artifact.ArtifactKinds)...)},
	"policy_alert": {"rule": oneOf("approval_claim", "seat_alert", "local_path", "workspace_ref", "repeated_failure", "controller_failing", "disk_low",
		"host_offline", "executor_unavailable", "presence_unjudged", "presence_deferred"),
		"source": oneOf("task_spec", "message", "seat_answer", "seat_plan", "report", "verify", "review", "attempt", "controller",
			"plan_note", "assessment"),
		"phrase": oneOf(append(slices.Clone(coord.ApprovalClaims), seatAlertPhrase, localPathPhrase, workspaceRefPhrase, repeatedFailurePhrase,
			loopalert.FailingPhrase, diskLowPhrase, hostOfflinePhrase, executorUnavailablePhrase, presenceUnjudgedPhrase, presenceDeferredPhrase)...),
		"artifact_id": artifactID, "message_id": messageID,
		"line_digest": fingerprint},
	"rule_proposed": {"direction": oneOf("initial", "loosen", "tighten"), "text_artifact_id": artifactID},
	"rule_approved": {"approval_id": approvalRef, "text_artifact_id": artifactID},
	"approval_seen": {"created_at": instant, "decided_at": instant},
	"pipeline_seen": {"tree": commit, "tested_by": ciRunName, "created_at": instant, "started_at": instant, "completed_at": instant, "kind": oneOf("ci", "delivery", "other"), "result": oneOf("Succeeded", "Failed"), "reason": shaped("a reason", regexp.MustCompile(`^[A-Za-z]{1,64}$`))},
	"question_seen": {"answered_at": instant, "created_at": instant, "withdrawn_at": instant, "expired_at": instant},
	"ci_seen":       {"commit": commit},
	"deploy_seen":   {"env": oneOf("dev", "stg", "prod"), "commit": commit},
	"note": {"landed": commit, "landed_tip": commit, "landed_via": oneOf(LandedViaLanding, LandedViaHarvest, LandedViaOwnerMerge),
		"matched_by": oneOf(MatchedResult, MatchedCherryPick, MatchedTrailer), "landed_result": commit, "pruned": agentBranch, "prune_refused": agentBranch, "prune_failed": agentBranch, "voided": approvalRef,
		"prune_round": oneOf("opened", "closed")},
	"legacy_imported": {"digest": fingerprint},
}

// waitRef is what a wait waits on: an attempt, a task or a message
// (checkBody holds it to its wait).
var waitRef = shaped("an attempt, task or message id", regexp.MustCompile(`^[atm]_`+ulid+`$`))

// badValue is why v, of its field's kind already, is refused, or ""
// (eventlog.BadValue).
func badValue(eventType, name string, v any) string { return eventlog.BadValue(eventType, name, v) }

// hex64 is a sha256 as 64 hex characters.
var hex64 = shaped("a sha256 (64 hex)", regexp.MustCompile(`^[0-9a-f]{64}$`))

// commitRE is a full commit id; artifactIDRE an artifact's id.
var (
	commitRE     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	artifactIDRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

var metricLabel = shaped("a metric label", regexp.MustCompile(`^(baseline|group):[A-Za-z0-9._-]{1,64}$`))
var metricIDRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var ciRepo = shaped("a repository name", regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`))
var ciRunName = shaped("a PipelineRun name", regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`))

// approvalIDRE is the shape of an approval's id.
var approvalIDRE = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)
