package store

import (
	"regexp"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
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
	"artifact_put": true, "verification_recorded": true, "owner_wait_opened": true, "owner_wait_closed": true, "actor_registered": true, "host_registered": true, "host_state_changed": true,
	"host_retired": true, "rule_proposed": true, "rule_signed": true,
	"rule_tightened": true, "rule_expired": true, "rule_approved": true, "capability_scored": true, "candidate_status_changed": true,
	"budget_charged": true, "pool_cooldown_set": true, "pool_measured": true, "wait_limit_passed": true,
	"legacy_imported": true,
	"host_slots_set":  true,
	"ci_reused":       true, "ci_red_classified": true,
}

// IsEventType says whether t is an event type of the closed vocabulary.
func IsEventType(t string) bool { return eventlog.IsEventType(t) }

// bodies is the closed body of each event type the store writes so far
// (design v5 §5.1.3): every required field present with a value of its kind,
// no other field. The database checks the type against the full vocabulary;
// a type with no body here is refused rather than written with a guessed shape.
var bodies = map[string][]field{

	"actor_registered": {{"actors", kIDs, false}, {"capability_facts", kIDs, true}, {"note", kStr, true}, {"token", kStr, true}},
	"artifact_put":     {{"artifact_id", kID, false}, {"artifact_kind", kStr, false}, {"size_bytes", kInt, false}, {"uploaded_by", kStr, false}},
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
		Events: []func(eventlog.Event) error{retiredRecord},
	})
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

var (
	ulid        = `[0-9A-HJKMNP-TV-Z]{26}`
	messageID   = shaped("a message id", regexp.MustCompile(`^m_`+ulid+`$`))
	artifactID  = shaped("an artifact id (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	commit      = shaped("a commit (40 hex)", regexp.MustCompile(`^[0-9a-f]{40}$`))
	fingerprint = shaped("a fingerprint (sha256:<64 hex>)", regexp.MustCompile(`^sha256:[0-9a-f]{64}$`))
	approvalRef = shaped("an approval (appr_<16 hex>)", approvalIDRE)
)

// fieldValues are the checked fields, by event type and field.
var fieldValues = map[string]map[string]value{
	"artifact_put":    {"artifact_id": artifactID, "artifact_kind": oneOf(keysOf(artifact.ArtifactKinds)...)},
	"rule_proposed":   {"direction": oneOf("initial", "loosen", "tighten"), "text_artifact_id": artifactID},
	"rule_approved":   {"approval_id": approvalRef, "text_artifact_id": artifactID},
	"legacy_imported": {"digest": fingerprint},
}

// badValue is why v, of its field's kind already, is refused, or ""
// (eventlog.BadValue).
func badValue(eventType, name string, v any) string { return eventlog.BadValue(eventType, name, v) }

// artifactIDRE is an artifact's id.
var (
	artifactIDRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// approvalIDRE is the shape of an approval's id.
var approvalIDRE = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)
