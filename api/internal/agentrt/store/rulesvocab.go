package store

import (
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	// The rest of the closed vocabulary (decoupling item 5, S4, 2026-11-18)
	// is legacyvocab's; the store links it for the tests of the trust anchors,
	// which have always had the vocabulary from here.
	_ "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/legacyvocab"
)

// The rule types, their bodies and their values: the rules' own part of the
// closed vocabulary, declared to the event log from here (the rest is
// legacyvocab's and the other domains'). Each body is closed (design v5
// §5.1.3): every required field present with a value of its kind, no other
// field. rule_expired is a type with no body yet.
var ruleBodies = map[string][]eventlog.Field{
	"rule_signed":    {{Name: "rule_id", Kind: eventlog.ID}, {Name: "rule_version", Kind: eventlog.Int}, {Name: "job_ids", Kind: eventlog.IDs, Optional: true}, {Name: "job_candidate_ids", Kind: eventlog.IDs, Optional: true}, {Name: "capability_facts", Kind: eventlog.IDs, Optional: true}},
	"rule_tightened": {{Name: "rule_id", Kind: eventlog.ID}, {Name: "rule_version", Kind: eventlog.Int}, {Name: "capability_facts", Kind: eventlog.IDs, Optional: true}},
	// rule_approved: a capacity change the Owner approved on a card
	// (ruleapprove.go): the approval, the draft's artifact and each field it
	// changes with its old and new value.
	"rule_approved": {{Name: "rule_id", Kind: eventlog.ID}, {Name: "rule_version", Kind: eventlog.Int}, {Name: "approval_id", Kind: eventlog.ID}, {Name: "text_artifact_id", Kind: eventlog.ID}, {Name: "changes", Kind: eventlog.Any}},
	"rule_proposed": {{Name: "rule_id", Kind: eventlog.ID}, {Name: "rule_version", Kind: eventlog.Int}, {Name: "direction", Kind: eventlog.Str}, {Name: "text_artifact_id", Kind: eventlog.ID}, {Name: "reason", Kind: eventlog.Str}, {Name: "token", Kind: eventlog.Str, Optional: true}},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types:  []string{"rule_expired"},
		Bodies: ruleBodies,
		Values: ruleValues,
	})
}

// Values (review 145 B6): what a field may hold beyond its kind. An enum's
// values are the states the schema's checks allow (0001_core.sql); an id has
// the shape the artifact store mints; an approval's is appr_ and 16 hex
// digits. A value refused here is the request's (ErrInvalid).
var (
	artifactID  = eventlog.Shaped("an artifact id (sha256:<64 hex>)", artifactIDRE)
	approvalRef = eventlog.Shaped("an approval (appr_<16 hex>)", approvalIDRE)

	ruleValues = map[string]map[string]eventlog.Value{
		"rule_proposed": {"direction": eventlog.OneOf("initial", "loosen", "tighten"), "text_artifact_id": artifactID},
		"rule_approved": {"approval_id": approvalRef, "text_artifact_id": artifactID},
	}
)

// artifactIDRE is an artifact's id; approvalIDRE the shape of an approval's.
var (
	artifactIDRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	approvalIDRE = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)
)

// badValue is why v, of its field's kind already, is refused, or ""
// (eventlog.BadValue). Nothing calls it since the vocabulary's value tests left
// for agentrt/integration (2026-11-18), where they call eventlog.BadValue. It
// stays for the reason VerifyStream does (event.go): it is the only production
// use of that name, without which TestNoNewNameIsExportedForAnotherPackagesTests
// refuses those tests. It goes with them (TD-582).
//
//nolint:unused // see above
func badValue(eventType, name string, v any) string { return eventlog.BadValue(eventType, name, v) }
