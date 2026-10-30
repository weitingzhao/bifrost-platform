package store

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

const StgReleaseStream = "landing:bifrost-platform"

// StgReleaseError distinguishes a malformed record, illegal edge, duplicate,
// conflict, exhausted budget, unauthorized writer and a retired write format.
type StgReleaseError struct{ Code, Kind, Reason string }

func (e *StgReleaseError) Error() string {
	return fmt.Sprintf("stg_release %s %s: %s", e.Kind, e.Code, e.Reason)
}

func stgError(code, kind, reason string) error {
	return &StgReleaseError{Code: code, Kind: kind, Reason: reason}
}

// StgRecord is the canonical, closed note.stg_release v1 payload. Returned
// records own their JSON values; changing a caller's map cannot change a fold.
type StgRecord map[string]any

func (r StgRecord) String(k string) string { s, _ := r[k].(string); return s }
func (r StgRecord) Int(k string) int64     { n, _ := cardNumber(r[k]); return n }
func (r StgRecord) Time(k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, r.String(k))
	return t
}
func (r StgRecord) object(k string) StgRecord {
	m, _ := r[k].(map[string]any)
	return StgRecord(m)
}

type stgSpec struct{ scope, fields string }

var stgSpecs = map[string]stgSpec{
	"opened":             {"batch", "members first_pending_at"},
	"p_selected":         {"batch", "P at"},
	"ci_accepted":        {"batch", "P ci"},
	"snapshot":           {"batch", "try inputs base set not_before"},
	"started":            {"batch", "try intent_seq run labels annotations fingerprint spend lease deadline"},
	"resend":             {"batch", "try intent_seq lease deadline why"},
	"unsent":             {"batch", "try proof"},
	"bound":              {"batch", "try uid"},
	"run_evidence":       {"batch", "try uid status raw at"},
	"effect":             {"batch", "try class workloads at"},
	"verify":             {"batch", "try outcome why evidence"},
	"member":             {"batch", "root op R to ref"},
	"retry":              {"batch", "try not_before"},
	"quarantined":        {"batch", "try why at"},
	"quarantine_closed":  {"batch", "try question_id by"},
	"settled":            {"batch", "members why"},
	"evidence":           {"ext", "source run P U infra content deployed health"},
	"deployment_checked": {"ext", "content deployed health"},
	"covers":             {"ext", "root op R"},
	"granted":            {"grant", "members gen trigger_seq question_id"},
	"withheld":           {"grant", "members why trigger_seq"},
	"blocked":            {"block", "why P base first_at evidence_version detail"},
	"ci_observed":        {"block", "interval_seq P ci status"},
	"rule_observed":      {"block", "interval_seq rule_id rule_version read_at"},
	"unblocked":          {"block", "why ref"},
	"diag":               {"batch", "try what gen"},
}

var (
	stgBatchRE = regexp.MustCompile(`^stg:bifrost-platform:([1-9][0-9]*)$`)
	stgIDRE    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_:./-]{0,199}$`)
	stgHex16   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	stgDigest  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	stgNameRE  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// ParseStgRelease checks the exact field set, including nested protocol
// objects. raw/workloads/health/detail retain adapter evidence as JSON objects:
// their semantic acquisition belongs to B5-b/c, not this no-I/O protocol.
func ParseStgRelease(v any) (StgRecord, error) {
	if r, ok := v.(StgRecord); ok {
		v = map[string]any(r)
	}
	b, err := canon.Encode(v)
	if err != nil {
		return nil, stgError("invalid", "", err.Error())
	}
	x, err := canon.Parse(b)
	if err != nil {
		return nil, stgError("invalid", "", err.Error())
	}
	m, ok := x.(map[string]any)
	if !ok {
		return nil, stgError("invalid", "", "expected an object")
	}
	r := StgRecord(m)
	k := r.String("kind")
	fail := func(why string) (StgRecord, error) { return nil, stgError("invalid", k, why) }
	spec, ok := stgSpecs[k]
	if !ok || r.String("v") != "1" || r.String("scope") != spec.scope {
		return fail("unknown version, kind or scope")
	}
	id := map[string]string{"batch": "batch", "ext": "evidence", "grant": "grant", "block": "block"}[spec.scope]
	fields := "v kind scope " + id + " " + spec.fields
	if k == "evidence" && r.String("source") == "automatic" {
		fields += " batch"
	}
	if k == "withheld" && r.String("why") == "owner" {
		fields += " question_id"
	}
	if !stgKeys(r, fields) {
		return fail("fields do not match kind")
	}
	for key, val := range r {
		valid := true
		s, isString := val.(string)
		switch key {
		case "v", "kind", "scope":
		case "batch":
			valid = stgBatchNumber(s) > 0
		case "evidence":
			// Failed verification has no trusted deployment evidence. The
			// field stays present but empty; verified/superseded must name one.
			valid = isString && (stgEvidenceID(s) || k == "verify" && r.String("outcome") == "failed" && s == "")
		case "block":
			valid = stgHex16.MatchString(s)
		case "P", "U", "R", "infra":
			valid = landingSHA.MatchString(s)
		case "root":
			valid = landingRoot.MatchString(s)
		case "at", "first_at", "not_before", "deadline", "read_at":
			valid = stgTime(val)
		case "try":
			valid = stgInteger(val, 1) || k == "diag" && (r.String("what") == "ci_waiting" || r.String("what") == "lease_lost") && stgInteger(val, 0)
		case "gen", "trigger_seq", "interval_seq", "rule_version":
			valid = stgInteger(val, 1)
		case "intent_seq":
			valid = stgInteger(val, 1) || k == "started" && stgInteger(val, 0)
		case "fingerprint":
			valid = cardDigest.MatchString(s)
		case "members":
			valid = stgMembers(val, k == "opened", false)
		case "set":
			valid = stgMembers(val, false, true)
		case "first_pending_at":
			obj, yes := val.(map[string]any)
			valid = yes && len(obj) > 0
			for sha, tm := range obj {
				valid = valid && landingSHA.MatchString(sha) && stgTime(tm)
			}
		case "inputs":
			valid = stgInputs(val)
		case "base":
			valid = stgBase(val)
			if k == "blocked" && r.String("why") == "baseline_unknown" {
				o, yes := val.(map[string]any)
				valid = yes && stgKeys(o, "P") && o["P"] == ""
			}
		case "ci":
			valid = stgCI(val)
		case "lease":
			o, yes := val.(map[string]any)
			valid = yes && stgKeys(o, "who gen") && stgString(o["who"]) && stgInteger(o["gen"], 1)
		case "spend":
			valid = stgSpend(val)
		case "labels", "annotations":
			valid = stgStringMap(val)
		case "raw":
			o, yes := val.(map[string]any)
			valid = yes && stgKeys(o, "results child_refs conditions")
			for _, name := range []string{"results", "child_refs", "conditions"} {
				_, yes = o[name].([]any)
				valid = valid && yes
			}
		case "content":
			o, yes := val.(map[string]any)
			valid = yes && stgKeys(o, "api_digest console_digest") && stgDigest.MatchString(fmt.Sprint(o["api_digest"])) && stgDigest.MatchString(fmt.Sprint(o["console_digest"]))
		case "deployed":
			o, yes := val.(map[string]any)
			valid = yes && stgKeys(o, "pin_sync_ok workloads at") && o["pin_sync_ok"] == true && stgObject(o["workloads"]) && stgTime(o["at"])
		case "workloads", "detail":
			valid = stgObject(val)
		case "health":
			o, yes := val.(map[string]any)
			valid = yes && o["status"] == "ok"
		case "source":
			valid = slices.Contains([]string{"automatic", "external"}, s)
		case "status":
			valid = slices.Contains([]string{"Succeeded", "Failed"}, s)
			if k == "ci_observed" {
				valid = s == "Succeeded"
			}
		case "outcome":
			valid = slices.Contains([]string{"verified", "failed", "superseded"}, s)
		case "class":
			valid = slices.Contains([]string{"unchanged", "this_try", "other", "unknown"}, s)
		case "to":
			valid = slices.Contains([]string{"delivered", "disposed", "released", "exhausted", "covered"}, s)
		case "by":
			valid = s == "owner"
		case "proof":
			valid = slices.Contains([]string{"gate_failed_before_send", "deadline_passed_not_found"}, s)
		case "what":
			valid = slices.Contains([]string{"lease_lost", "running_long", "verify_unknown", "ci_waiting", "finalizer_stuck"}, s)
		case "why":
			valid = stgString(val)
			switch k {
			case "resend":
				valid = s == "not_found_unbound"
			case "quarantined":
				valid = slices.Contains([]string{"object_lost", "collision", "unknown_execution"}, s)
			case "withheld":
				valid = s == "owner" || s == "hard_floor"
			case "unblocked":
				valid = slices.Contains([]string{"inputs_changed", "rule_changed", "evidence_changed", "granted", "external_release"}, s)
			}
		case "ref":
			valid = stgString(val) || stgInteger(val, 1)
		default:
			valid = stgIDRE.MatchString(s)
		}
		if !valid {
			return fail("invalid " + key)
		}
	}
	if k == "opened" {
		pending := r.object("first_pending_at")
		ms := r.members("members")
		if len(pending) != len(ms) {
			return fail("first_pending_at must name exactly the members")
		}
		for _, m := range ms {
			if !stgTime(pending[m.R]) {
				return fail("missing first_pending_at for R")
			}
		}
	}
	if (k == "granted" || k == "withheld" && r.String("why") == "owner") && r.String("grant") != r.String("question_id") {
		return fail("grant must equal question_id")
	}
	// A failed/unusable CI need not have a batch ci_accepted row. When the
	// adapter has one, reserve detail.ci for the exact rejected observation.
	// Older blocks without it still bind all CI identities in their prefix.
	if k == "blocked" {
		if v, exists := r.object("detail")["ci"]; exists {
			ci, ok := v.(map[string]any)
			c := StgRecord(ci)
			if !ok || !stgKeys(ci, "uid created_at read_at") || !stgString(ci["uid"]) || !stgTime(ci["created_at"]) || !stgTime(ci["read_at"]) || c.Time("created_at").After(c.Time("read_at")) {
				return fail("invalid blocked CI observation")
			}
		}
	}
	return r, nil
}

func badStgRelease(v any) string {
	_, err := ParseStgRelease(v)
	if err != nil {
		return err.Error()
	}
	return ""
}

func stgKeys(m map[string]any, names string) bool {
	keys := strings.Fields(names)
	if len(keys) != len(m) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
func stgInteger(v any, min int64) bool { n, ok := cardNumber(v); return ok && n >= min }
func stgString(v any) bool             { s, ok := v.(string); return ok && strings.TrimSpace(s) != "" }
func stgTime(v any) bool {
	s, ok := v.(string)
	t, err := time.Parse(time.RFC3339Nano, s)
	return ok && err == nil && !t.IsZero()
}
func stgObject(v any) bool { m, ok := v.(map[string]any); return ok && len(m) > 0 }
func stgStringMap(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for k, v := range m {
		if k == "" || !stgString(v) {
			return false
		}
	}
	return true
}
func stgBatchNumber(s string) int64 {
	m := stgBatchRE.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return n
}
func stgEvidenceID(s string) bool {
	p := strings.Split(s, "/")
	if len(p) != 3 {
		return false
	}
	for _, p := range p {
		if len(p) > 253 || !stgNameRE.MatchString(p) {
			return false
		}
	}
	return true
}

// StgMemberKey is an outcome identity; a commit's budget is intentionally
// separate and shared by all batches, under (R, generation).
type StgMemberKey struct{ Root, Op, R string }
type StgMember struct {
	StgMemberKey
	Gen int64
}

func stgMember(m map[string]any) StgMemberKey {
	return StgMemberKey{StgRecord(m).String("root"), StgRecord(m).String("op"), StgRecord(m).String("R")}
}
func (r StgRecord) members(k string) []StgMember {
	var out []StgMember
	a, _ := r[k].([]any)
	for _, v := range a {
		m, _ := v.(map[string]any)
		out = append(out, StgMember{stgMember(m), StgRecord(m).Int("gen")})
	}
	return out
}
func stgMembers(v any, gen, empty bool) bool {
	a, ok := v.([]any)
	if !ok || !empty && len(a) == 0 {
		return false
	}
	seen := map[string]bool{}
	fields := "root op R"
	if gen {
		fields += " gen"
	}
	for _, v := range a {
		m, ok := v.(map[string]any)
		r := StgRecord(m)
		if !ok || !stgKeys(m, fields) || !landingRoot.MatchString(r.String("root")) || !stgIDRE.MatchString(r.String("op")) || !landingSHA.MatchString(r.String("R")) || seen[r.String("R")] || gen && !stgInteger(m["gen"], 1) {
			return false
		}
		seen[r.String("R")] = true
	}
	return true
}
func stgInputs(v any) bool {
	m, ok := v.(map[string]any)
	r := StgRecord(m)
	if !ok || !stgKeys(m, "namespace pipeline revision uiRevision infraRevision params_canonical") || r.String("namespace") != "cicd" || r.String("pipeline") != "bifrost-deliver-platform" || !stgStringMap(m["params_canonical"]) {
		return false
	}
	for _, k := range []string{"revision", "uiRevision", "infraRevision"} {
		if !landingSHA.MatchString(r.String(k)) || r.object("params_canonical").String(k) != r.String(k) {
			return false
		}
	}
	return true
}
func stgBase(v any) bool {
	m, ok := v.(map[string]any)
	r := StgRecord(m)
	return ok && stgKeys(m, "P api_digest console_digest evidence") && landingSHA.MatchString(r.String("P")) && stgDigest.MatchString(r.String("api_digest")) && stgDigest.MatchString(r.String("console_digest")) && stgEvidenceID(r.String("evidence"))
}
func stgCI(v any) bool {
	m, ok := v.(map[string]any)
	r := StgRecord(m)
	if !ok || !stgKeys(m, "name uid created_at platform_commit ui_commit infra_commit pipeline_ref repo_url read_at") || r.String("pipeline_ref") != "bifrost-ci-platform" {
		return false
	}
	for _, k := range []string{"name", "uid", "repo_url"} {
		if !stgString(m[k]) {
			return false
		}
	}
	for _, k := range []string{"platform_commit", "ui_commit", "infra_commit"} {
		if !landingSHA.MatchString(r.String(k)) {
			return false
		}
	}
	return stgTime(m["created_at"]) && stgTime(m["read_at"]) && !r.Time("created_at").After(r.Time("read_at"))
}
func stgSpend(v any) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	seen := map[string]bool{}
	for _, v := range a {
		m, ok := v.(map[string]any)
		r := StgRecord(m)
		if !ok || !stgKeys(m, "R gen n") || !landingSHA.MatchString(r.String("R")) || !stgInteger(m["gen"], 1) || !stgInteger(m["n"], 1) || r.Int("n") > 3 || seen[r.String("R")] {
			return false
		}
		seen[r.String("R")] = true
	}
	return true
}
