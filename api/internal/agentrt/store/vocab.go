package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"time"
)

// A body field's kind. Values are what canon.Parse or a Go caller produce.
type kind int

const (
	kStr  kind = iota // a non-empty string
	kID               // a non-empty string naming a row (some of a fixed shape: fieldValues)
	kInt              // an integer
	kIDs              // a non-empty list of row ids
	kAny              // any canonical JSON value
	kBool             // true or false
)

type field struct {
	name     string
	kind     kind
	optional bool
}

// eventTypes is the closed vocabulary, as the database's event_type_closed
// constraint has it (migrations 0006); TestEventTypesAreTheDatabases holds
// the two together. bodies below covers only the types the store writes.
var eventTypes = map[string]bool{
	"plan_node_created": true, "plan_node_retitled": true, "plan_node_state_changed": true,
	"plan_change_proposed": true, "plan_change_decided": true, "plan_changed": true, "mission_proposed": true,
	"mission_state_changed": true, "mission_amended": true, "task_proposed": true, "task_state_changed": true,
	"task_dependency_added": true, "task_cancelled": true, "route_decided": true, "attempt_leased": true,
	"attempt_started": true, "attempt_progress": true, "attempt_checkpointed": true, "attempt_stale": true,
	"attempt_renewed": true, "attempt_computed": true, "attempt_persisted": true, "attempt_submitted": true,
	"attempt_lost": true, "attempt_ended": true, "attempt_salvaged": true, "attempt_cancelled": true,
	"artifact_put": true, "verification_recorded": true, "owner_wait_opened": true, "owner_wait_closed": true,
	"approval_seen": true, "question_seen": true, "ci_seen": true, "pipeline_seen": true, "deploy_seen": true,
	"owner_signoff": true, "actor_registered": true, "host_registered": true, "host_state_changed": true,
	"host_retired": true, "control_set": true, "control_cleared": true, "rule_proposed": true, "rule_signed": true,
	"rule_tightened": true, "rule_expired": true, "capability_scored": true, "candidate_status_changed": true,
	"budget_charged": true, "pool_cooldown_set": true, "pool_measured": true, "message_posted": true,
	"message_acked": true, "policy_alert": true, "decision_recorded": true, "note": true, "wait_limit_passed": true,
	"legacy_imported": true,
}

// IsEventType says whether t is an event type of the closed vocabulary.
func IsEventType(t string) bool { return eventTypes[t] }

// bodies is the closed body of each event type the store writes so far
// (design v5 §5.1.3): every required field present with a value of its kind,
// no other field. The database checks the type against the full vocabulary;
// a type with no body here is refused rather than written with a guessed shape.
var bodies = map[string][]field{
	"actor_registered":        {{"actors", kIDs, false}, {"capability_facts", kIDs, true}, {"note", kStr, true}, {"token", kStr, true}},
	"plan_node_created":       {{"node_kind", kStr, false}, {"label", kStr, false}, {"title", kStr, false}, {"ordinal", kInt, false}},
	"plan_node_retitled":      {{"title", kStr, false}, {"label", kStr, true}},
	"plan_node_state_changed": {{"from", kStr, false}, {"to", kStr, false}, {"cause", kStr, false}},
	"plan_changed":            {{"change", kStr, false}, {"decided_event_id", kInt, true}, {"nodes", kAny, true}, {"bindings", kAny, true}, {"note", kStr, true}},
	"plan_change_proposed":    {{"changes", kAny, false}, {"reason", kStr, false}, {"standing", kBool, false}, {"token", kStr, true}, {"question_id", kStr, true}},
	"plan_change_decided":     {{"proposal_event_id", kInt, false}, {"decision", kStr, false}, {"by", kStr, false}, {"question_id", kStr, true}, {"error", kStr, true}},
	"mission_proposed":        {{"title", kStr, false}, {"token", kStr, true}},
	"mission_state_changed":   {{"from", kStr, false}, {"to", kStr, false}, {"reason", kStr, true}, {"question_id", kID, true}, {"answer", kStr, true}},
	"mission_amended":         {{"reason", kStr, false}, {"success", kAny, true}, {"out_of_scope", kAny, true}, {"max_attempts", kInt, true}, {"max_wall_hours", kInt, true}},
	"decision_recorded":       {{"decision", kStr, false}, {"why", kStr, false}, {"dropped", kStr, true}, {"reverse", kStr, true}},
	"task_proposed":           {{"title", kStr, false}, {"role", kStr, false}, {"off_plan_reason", kStr, true}, {"core", kBool, true}, {"needs", kIDs, true}, {"repo", kStr, true}, {"base_commit", kStr, true}, {"token", kStr, true}},
	"task_state_changed":      {{"from", kStr, false}, {"to", kStr, false}, {"reason", kStr, true}, {"failed", kBool, true}},
	"task_dependency_added":   {{"depends_on_task_id", kID, false}},
	"route_decided":           {{"job", kStr, false}, {"candidates", kAny, false}, {"excluded", kAny, false}, {"inputs", kAny, false}, {"fingerprint", kStr, false}},
	"attempt_leased":          {{"attempt_no", kInt, false}, {"lease_epoch", kInt, false}, {"executor", kID, false}, {"host_id", kID, true}, {"model", kStr, true}, {"model_provider", kStr, true}, {"choice", kAny, true}},
	"attempt_started":         {{"model", kStr, true}, {"model_provider", kStr, true}},
	"attempt_checkpointed":    {{"checkpoint_seq", kInt, false}, {"artifacts", kIDs, true}, {"too_large", kBool, true}},
	"attempt_stale":           {{"lease_expires_at", kStr, false}},
	"attempt_renewed":         {{"lease_expires_at", kStr, false}},
	"attempt_computed":        {{"end_how", kStr, false}, {"conclusion", kStr, false}, {"exit_code", kInt, true}},
	"attempt_submitted":       {},
	"attempt_lost":            {{"reason", kStr, false}},
	"attempt_ended":           {{"end_how", kStr, false}, {"group_cleared", kBool, false}},
	"attempt_salvaged":        {{"artifacts", kIDs, true}, {"pushed_ref", kStr, true}, {"pushed_commit", kStr, true}},
	"attempt_cancelled":       {{"reason", kStr, false}, {"accepted_attempt", kID, true}},
	"host_registered":         {{"host_id", kID, false}, {"actor_id", kID, false}, {"host_kind", kStr, false}, {"token", kStr, true}},
	"host_state_changed":      {{"host_id", kID, false}, {"from", kStr, false}, {"to", kStr, false}},
	"attempt_persisted":       {{"pushed_commit", kStr, true}, {"pushed_ref", kStr, true}, {"artifacts", kIDs, true}},
	"verification_recorded":   {{"verification_id", kID, false}, {"method", kStr, false}, {"verdict", kStr, false}, {"check_ref", kStr, true}, {"env", kStr, true}, {"injected", kBool, true}},
	"artifact_put":            {{"artifact_id", kID, false}, {"artifact_kind", kStr, false}, {"size_bytes", kInt, false}, {"uploaded_by", kStr, false}},
	"message_posted":          {{"message_id", kID, false}, {"to", kID, false}, {"kind", kStr, false}},
	"message_acked":           {{"message_id", kID, false}},
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
	// TD-381, TD-382). Every rule but repeated_failure names its artifact
	// (checkBody).
	"policy_alert": {{"rule", kStr, false}, {"source", kStr, false}, {"phrase", kStr, false}, {"artifact_id", kID, true}, {"message_id", kID, true},
		{"line_digest", kStr, true}},
	"control_set":       {{"control_id", kID, false}, {"scope", kStr, false}, {"kind", kStr, false}, {"reason", kStr, false}, {"scope_ref", kID, true}, {"token", kStr, true}},
	"control_cleared":   {{"control_id", kID, false}},
	"pool_cooldown_set": {{"vendor_pool_id", kID, false}, {"until", kStr, false}, {"reason", kStr, false}},
	"capability_scored": {{"source", kStr, false}, {"rows", kInt, false}, {"samples", kAny, false}, {"xmin", kInt, true}, {"late", kAny, true}, {"expires_at", kStr, true},
		{"run_id", kStr, true}, {"host", kID, true}, {"executor", kID, true}, {"provider", kStr, true}, {"model", kStr, true}, {"questions", kStr, true}, {"results", kAny, true},
		{"started_at", kStr, true}, {"digest", kStr, true}},
	"pool_measured":  {{"vendor_pool_id", kID, false}, {"source", kStr, false}, {"used_permille", kInt, true}, {"resets_at", kStr, true}, {"limited", kBool, true}, {"text", kStr, true}},
	"budget_charged": {{"attempt_id", kID, false}, {"wall_s", kInt, false}, {"vendor_pool_id", kID, true}, {"tokens", kInt, true}, {"usd_micros", kInt, true}},
	// legacy_imported: the head of an import (source names what it came from,
	// count how many it holds) or, on a recorder log's stream, one line of
	// the log (source recorder; the line's seq, its original digest, the
	// body as kept, masked when its local paths were; recorderimport.go).
	"legacy_imported": {{"source", kStr, false}, {"count", kInt, true}, {"work_id", kStr, true}, {"seq", kInt, true},
		{"digest", kStr, true}, {"record", kStr, true}, {"masked", kBool, true}},
	"rule_signed":              {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"job_ids", kIDs, true}, {"job_candidate_ids", kIDs, true}, {"capability_facts", kIDs, true}},
	"rule_tightened":           {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"capability_facts", kIDs, true}},
	"rule_proposed":            {{"rule_id", kID, false}, {"rule_version", kInt, false}, {"direction", kStr, false}, {"text_artifact_id", kID, false}, {"reason", kStr, false}, {"token", kStr, true}},
	"candidate_status_changed": {{"job_candidate_id", kID, false}, {"from", kStr, false}, {"to", kStr, false}, {"rule_id", kID, false}, {"rule_version", kInt, false}},
	"approval_seen":            {{"approval", kInt, false}, {"status", kStr, false}},
	"question_seen":            {{"question", kID, false}, {"status", kStr, false}, {"option", kStr, true}, {"text", kStr, true}, {"answered_by", kStr, true}, {"answered_at", kStr, true}},
	"ci_seen":                  {{"repo", kStr, false}, {"commit", kStr, false}, {"status", kStr, false}, {"run", kStr, true}},
	"deploy_seen":              {{"env", kStr, false}, {"commit", kStr, false}, {"deploy_ref", kAny, false}},
	"owner_signoff":            {{"ref", kStr, false}},
	// note: landed, the commit an accepted result first stood on main as, when
	// the controller first saw it there; pruned, an attempt branch its host
	// deleted; prune_refused, one Gitea would not let it delete; prune_failed,
	// one it could not reach Gitea to delete (prune.go).
	// On an item's assessment stream (assess.go, TD-391): ask, the option
	// keys and refs its spec gave; assessment, what the seat handed in, its
	// refs resolved; unavailable, why there is none. On the exams stream,
	// caused by a paper's issue (examvoid.go, TD-407): voided, the approval
	// that voided the paper, which no run was handed in for.
	"note": {{"text", kStr, false}, {"manual", kStr, true}, {"evidence", kIDs, true}, {"landed", kStr, true},
		{"pruned", kStr, true}, {"prune_refused", kStr, true}, {"prune_failed", kStr, true},
		{"ask", kAny, true}, {"assessment", kAny, true}, {"unavailable", kStr, true}, {"voided", kStr, true}},
	// owner_wait_opened, owner_wait_closed (v5 §5.1.3, with the question they
	// are about): a blocked task's decision card, asked for the block its
	// blocked_event_id names (the card-th asked for it), and how the card
	// ended (blockedcard.go); by names the rule that answered it for the
	// Owner (cardauto.go, TD-390), never present when he answered.
	"owner_wait_opened": {{"question_id", kID, false}, {"blocked_event_id", kInt, false}, {"card", kInt, false}},
	"owner_wait_closed": {{"question_id", kID, false}, {"outcome", kStr, false}, {"answer", kStr, true}, {"text", kStr, true}, {"by", kStr, true}},
}

func checkBody(eventType string, body map[string]any) error {
	spec, ok := bodies[eventType]
	if !ok {
		return fmt.Errorf("store: event type %q has no body defined yet", eventType)
	}
	allowed := map[string]bool{}
	for _, f := range spec {
		allowed[f.name] = true
		v, present := body[f.name]
		if !present {
			if f.optional {
				continue
			}
			return fmt.Errorf("store: %s needs %q", eventType, f.name)
		}
		if !ofKind(v, f.kind) {
			return fmt.Errorf("store: %s field %q has the wrong kind of value", eventType, f.name)
		}
		if why := badValue(eventType, f.name, v); why != "" {
			return invalid("%s field %q %s", eventType, f.name, why)
		}
	}
	var extra []string
	for k := range body {
		if !allowed[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("store: %s does not take %v", eventType, extra)
	}
	if _, named := body["artifact_id"]; eventType == "policy_alert" && !named && body["rule"] != "repeated_failure" {
		return fmt.Errorf("store: policy_alert needs \"artifact_id\"")
	}
	return nil
}

func ofKind(v any, k kind) bool {
	switch k {
	case kStr, kID:
		s, ok := v.(string)
		return ok && s != ""
	case kInt:
		switch n := v.(type) {
		case int, int32, int64, uint32:
			return true
		case json.Number:
			_, err := n.Int64()
			return err == nil
		}
		return false
	case kIDs:
		switch l := v.(type) {
		case []any:
			if len(l) == 0 {
				return false
			}
			for _, e := range l {
				if s, ok := e.(string); !ok || s == "" {
					return false
				}
			}
			return true
		case []string:
			if len(l) == 0 {
				return false
			}
			for _, s := range l {
				if s == "" {
					return false
				}
			}
			return true
		}
		return false
	case kBool:
		_, ok := v.(bool)
		return ok
	}
	return true
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

// value checks one string of a field; ok is "" or why it is refused.
type value func(s string) string

// oneOf is an enum.
func oneOf(vals ...string) value {
	return func(s string) string {
		if slices.Contains(vals, s) {
			return ""
		}
		return fmt.Sprintf("is %q, not one of %v", s, vals)
	}
}

// statesOf is every state a state machine names.
func statesOf(moves map[string][]string) []string {
	var out []string
	for from, tos := range moves {
		out = append(out, from)
		out = append(out, tos...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// shaped is an id or a reference of a fixed shape.
func shaped(name string, re *regexp.Regexp) value {
	return func(s string) string {
		if re.MatchString(s) {
			return ""
		}
		return fmt.Sprintf("is %q, not %s", s, name)
	}
}

func instant(s string) string {
	if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
		return fmt.Sprintf("is %q, not an RFC 3339 instant", s)
	}
	return ""
}

var (
	ulid        = `[0-9A-HJKMNP-TV-Z]{26}`
	taskID      = shaped("a task id", regexp.MustCompile(`^t_`+ulid+`$`))
	attemptID   = shaped("an attempt id", regexp.MustCompile(`^a_`+ulid+`$`))
	messageID   = shaped("a message id", regexp.MustCompile(`^m_`+ulid+`$`))
	verifyID    = shaped("a verification id", regexp.MustCompile(`^v_`+ulid+`$`))
	artifactID  = shaped("an artifact id (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	commit      = shaped("a commit (40 hex)", regexp.MustCompile(`^[0-9a-f]{40}$`))
	agentBranch = shaped("an attempt branch (refs/heads/agent/<task>/a<n>)", agentRef)
	fingerprint = shaped("a fingerprint (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	executorID  = shaped("an executor (exec:<tool>)", regexp.MustCompile(`^exec:[a-z]+$`))
	approvalRef = shaped("an approval (appr_<16 hex>)", approvalIDRE)
	attemptEnds = []string{"exit", "hard_limit", "over_capacity", "cancelled", "daemon_restart", "host_reboot", "lease_lost"}
)

// fieldValues are the checked fields, by event type and field.
var fieldValues = map[string]map[string]value{
	"plan_node_created":       {"node_kind": oneOf("round", "item", "step", "arch_root", "arch_row")},
	"plan_node_state_changed": {"from": oneOf(statesOf(stepMoves)...), "to": oneOf(statesOf(stepMoves)...)},
	"plan_change_decided":     {"decision": oneOf("applied", "refused", "failed", "withdrawn", "expired")},
	"mission_state_changed":   {"from": oneOf(statesOf(missionMoves)...), "to": oneOf(statesOf(missionMoves)...)},
	"task_proposed":           {"role": oneOf("plan", "implement", "verify", "review", "brief", "other")},
	"task_state_changed":      {"from": oneOf(statesOf(taskMoves)...), "to": oneOf(statesOf(taskMoves)...)},
	"task_dependency_added":   {"depends_on_task_id": taskID},
	"attempt_leased":          {"executor": executorID},
	"attempt_checkpointed":    {"artifacts": artifactID},
	"attempt_stale":           {"lease_expires_at": instant},
	"attempt_renewed":         {"lease_expires_at": instant},
	"attempt_computed":        {"end_how": oneOf(attemptEnds...), "conclusion": oneOf("done", "failed", "none")},
	"attempt_ended":           {"end_how": oneOf(attemptEnds...)},
	"attempt_salvaged":        {"artifacts": artifactID},
	"attempt_persisted":       {"artifacts": artifactID},
	"attempt_cancelled":       {"accepted_attempt": attemptID},
	"host_registered":         {"host_kind": oneOf("dev", "vm")},
	"host_state_changed":      {"from": oneOf("active", "draining", "retired"), "to": oneOf("active", "draining", "retired")},
	"verification_recorded":   {"verification_id": verifyID, "method": oneOf("deterministic", "review", "gate"), "verdict": oneOf("pass", "fail", "none"), "env": oneOf("dev", "stg", "prod")},
	"artifact_put":            {"artifact_id": artifactID, "artifact_kind": oneOf(keysOf(ArtifactKinds)...)},
	"message_posted":          {"message_id": messageID, "kind": oneOf("ask", "answer", "help", "handover", "note", "wake")},
	"message_acked":           {"message_id": messageID},
	"policy_alert": {"rule": oneOf("approval_claim", "seat_alert", "local_path", "workspace_ref", "repeated_failure"),
		"source":      oneOf("task_spec", "message", "seat_answer", "seat_plan", "report", "verify", "review", "attempt"),
		"phrase":      oneOf(append(slices.Clone(approvalClaims), seatAlertPhrase, localPathPhrase, workspaceRefPhrase, repeatedFailurePhrase)...),
		"artifact_id": artifactID, "message_id": messageID,
		"line_digest": fingerprint},
	"control_set":              {"scope": oneOf(keysOf(controlKinds)...), "kind": oneOf("stop", "pause", "drain")},
	"pool_cooldown_set":        {"until": instant},
	"capability_scored":        {"source": oneOf("exam", "benchmark", "in_job"), "expires_at": instant, "started_at": instant},
	"pool_measured":            {"source": oneOf("reading", "exit_text", "owner"), "resets_at": instant},
	"budget_charged":           {"attempt_id": attemptID},
	"rule_proposed":            {"direction": oneOf("initial", "loosen", "tighten"), "text_artifact_id": artifactID},
	"candidate_status_changed": {"from": oneOf(candidateStatus...), "to": oneOf(candidateStatus...)},
	"question_seen":            {"answered_at": instant},
	"owner_wait_closed":        {"outcome": oneOf(OutcomeRetried, OutcomeDropped, OutcomeClosed, OutcomeNone, OutcomeMoot, "expired", "withdrawn")},
	"ci_seen":                  {"commit": commit},
	"deploy_seen":              {"env": oneOf("dev", "stg", "prod"), "commit": commit},
	"note":                     {"landed": commit, "pruned": agentBranch, "prune_refused": agentBranch, "prune_failed": agentBranch, "voided": approvalRef},
	"route_decided":            {"fingerprint": fingerprint},
	"legacy_imported":          {"digest": fingerprint},
}

// badValue is why v, of its field's kind already, is refused, or "". A list
// is checked element by element.
func badValue(eventType, name string, v any) string {
	check, ok := fieldValues[eventType][name]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return check(t)
	case []string:
		for _, s := range t {
			if why := check(s); why != "" {
				return why
			}
		}
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				if why := check(s); why != "" {
					return why
				}
			}
		}
	}
	return ""
}
