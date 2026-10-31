package landingci

import (
	"encoding/json"
	"strings"
)

// s5CheckerCodes are the §5 content/behavior codes the trusted checker
// reports as findings. Only these pass through from evidence: the gate and
// lifecycle codes (S5_EVIDENCE_MISSING, S5_BINDING, S5_CI_IDENTITY,
// S5_GENERATION_STALE, S5_REVIEW_*, S5_PATCH_MISMATCH, S5_PREPARATION_ONLY)
// are the platform's own and some of them mean "wait"; a checker that emits
// one, or any unknown code, is a deterministic S5_BEHAVIOR failure.
var s5CheckerCodes = map[string]bool{
	"S5_PATH_NOT_ENROLLED": true, "S5_CHANGE_KIND": true, "S5_PARSE": true, "S5_TYPE": true, "S5_SYNTAX_UNKNOWN": true,
	"S5_IMPORT_RUNTIME": true, "S5_IMPORT_UNKNOWN": true, "S5_TYPE_CAPABILITY": true, "S5_TYPE_CONTRACT": true,
	"S5_GLOBAL_UNKNOWN": true, "S5_CALL_UNKNOWN": true, "S5_CLOCK_SITE": true, "S5_MUTATION_ESCAPE": true,
	"S5_DATE_PARSE_SITE": true, "S5_ENVIRONMENT_SITE": true, "S5_IMPLICIT_COERCION": true, "S5_BUILTIN_ARGUMENT": true,
	"S5_DATA_PROVENANCE": true, "S5_PROTOTYPE_ACCESS": true, "S5_SELECTOR_UNKNOWN": true, "S5_CALLER_CHANGED": true,
	"S5_TRUST_USE": true, "S5_REQUIRED_GUARD": true, "S5_EXPORT_CONTRACT": true, "S5_LANGUAGE_DISABLED": true,
	"S5_BASELINE_UNREVIEWED": true, "S5_BEHAVIOR": true,
}

func firstS5(values []any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	m, _ := values[0].(map[string]any)
	return m
}

func s5Bindings(k S5CIKey, uid string) map[string]any {
	b, _ := k.Canonical()
	var all map[string]any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	_ = d.Decode(&all)
	for _, name := range []string{"env", "tenant", "task", "attempt", "op", "profileDigest"} {
		delete(all, name)
	}
	digest, _ := k.Digest()
	all["policyDigest"], all["ciUID"], all["keyDigest"] = k.ProfileDigest, uid, digest
	return all
}

func s5CheckedResults(values map[string]string, k S5CIKey, uid string) (json.RawMessage, string) {
	digest, _ := k.Digest()
	want := map[string]string{"base-commit": k.M0, "revision-commit": k.R, "baseline-tree": k.BaselineTree,
		"result-tree": k.ResultTree, "ui-commit": k.UISHA, "key-digest": digest, "generation": s5Params(k)["generation"]}
	if len(values) != len(want)+2 {
		return nil, "S5_EVIDENCE_MISSING"
	}
	for name, v := range want {
		if values[name] == "" {
			return nil, "S5_EVIDENCE_MISSING"
		}
		if values[name] != v {
			return nil, "S5_BINDING"
		}
	}
	raw := []byte(values["evidence"])
	if len(raw) == 0 || len(raw) > 3000 || values["evidence-digest"] == "" {
		return nil, "S5_EVIDENCE_MISSING"
	}
	if s5Hash(raw) != values["evidence-digest"] {
		return nil, "S5_BINDING"
	}
	var evidence map[string]any
	if decodeS5(raw, &evidence) != nil {
		return nil, "S5_CI_IDENTITY"
	}
	bindings := s5Bindings(k, uid)
	if len(evidence) != len(bindings)+3 {
		return nil, "S5_BINDING"
	}
	for name, v := range bindings {
		if !s5Same(evidence[name], v) {
			return nil, "S5_BINDING"
		}
	}
	findings, ok := evidence["findings"].([]any)
	if !ok {
		return nil, "S5_BINDING"
	}
	if evidence["passed"] != true || len(findings) != 0 {
		// The checker sorts findings by path, AST position and code (§5);
		// the first one is the refusal. A rejection without a well-formed
		// code is still a behavior failure, never a pass.
		if first, _ := firstS5(findings)["code"].(string); evidence["passed"] == false && s5CheckerCodes[first] {
			return nil, first
		}
		return nil, "S5_BEHAVIOR"
	}
	files, ok := evidence["files"].([]any)
	if !ok {
		return nil, "S5_BINDING"
	}
	seen := map[string]bool{}
	for _, value := range files {
		file, ok := value.(map[string]any)
		if !ok || len(file) != 3 {
			return nil, "S5_BINDING"
		}
		path, _ := file["path"].(string)
		before, _ := file["beforeBlob"].(string)
		after, _ := file["afterBlob"].(string)
		if path == "" || strings.HasPrefix(path, "/") || seen[path] || !nonzeroHex(before, 40) || !nonzeroHex(after, 40) {
			return nil, "S5_BINDING"
		}
		for _, part := range strings.Split(path, "/") {
			if part == ".." {
				return nil, "S5_BINDING"
			}
		}
		seen[path] = true
	}
	return json.RawMessage(raw), ""
}
