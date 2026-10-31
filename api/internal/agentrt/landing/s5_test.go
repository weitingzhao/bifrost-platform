package landing

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func s5Facts() Facts {
	f := eligibleFacts()
	f.Rule.Version = 3
	f.Rule.Landing.Repos[0].Allow = append(f.Rule.Landing.Repos[0].Allow, rules.S5Paths()...)
	p := rules.S5Paths()[0]
	f.Changes = []rules.LandingChange{{Kind: "M", OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "return value + 1"}}
	d := strings.Repeat("a", 64)
	f.S5Policy = S5Policy{d, "sha256:" + d, d, d, d, d, U}
	f.S5Patch = PatchEvidence{A: f.A, ParentA: f.Base, Base: f.Base, SourcePatchSHA256: d, SourceStablePatchID: A, DiffAlgorithmVersion: "git-stable-v1", OldNewPathsAndModes: []S5PathMode{{p, p, "100644", "100644", "M", false}}}
	f.S5Review = ReviewBinding{Repo: f.Repo, Task: f.Task, AcceptedAttempt: f.Attempt, A: f.A, Base: f.Base, AcceptanceVerificationID: "v_accept", ImplementerAttempt: f.Attempt, ImplementerExecutor: "exec:codex", ImplementerProvider: "openai", ReviewVerificationID: "v_review", ReviewerTask: "t_review", ReviewerAttempt: "a_review", ReviewerExecutor: "exec:claude", ReviewerProvider: "anthropic", Verdict: "pass", ReviewArtifactID: "sha256:" + d, ReviewArtifactSHA256: d, ReviewedPatchSHA256: d, ReviewedStablePatchID: A}
	return f
}
func s5Ready(t *testing.T) (Facts, S5CIResult) {
	t.Helper()
	f := s5Facts()
	f.Operation = Operation{Task: f.Task, Attempt: f.Attempt, A: f.A, R: R, M0: M0, Phase: "branch_pushed", PreparedAt: epoch.Add(2 * time.Minute)}
	f.S5Patch.M0 = M0
	f.S5Patch.R = R
	f.S5Patch.ParentR = M0
	f.S5Patch.ReplayMatch = true
	f.S5Patch.ReplayPatchSHA256 = strings.Repeat("b", 64)
	f.S5Patch.ReplayStablePatchID = A
	f.S5Patch.BaselineTree = M0
	f.S5Patch.ResultTree = R
	k, why := NewS5CIKey(f, "land:"+f.Task+":1", 1)
	if why != "" {
		t.Fatal(why)
	}
	kd, e := k.Digest()
	if e != nil {
		t.Fatal(e)
	}
	name, e := k.RunName()
	if e != nil {
		t.Fatal(e)
	}
	f.Operation.S5 = S5State{Required: true, Key: k, KeyDigest: kd, UID: "uid-1"}
	// Exactly the change's S5 candidate (s5check.EvidencePaths), never every
	// enrolled file: the rule enrolls all four, the change touches one.
	files := []S5FileEvidence{{rules.S5Paths()[0], A, R}}
	ci := S5CIResult{Key: k, Run: name, UID: "uid-1", KeyDigest: kd, Status: "Succeeded", CreatedAt: epoch.Add(3 * time.Minute), CompletedAt: epoch.Add(4 * time.Minute), IdentityVerified: true, Evidence: &S5ContentEvidence{S5ContentBinding: S5BindingForKey(k), CIUID: "uid-1", KeyDigest: kd, Files: files, Passed: true}}
	return f, ci
}
func TestS5FirstClaimIsPreparationOnly(t *testing.T) {
	f := s5Facts()
	now := epoch.Add(time.Hour)
	if f.GrantEnabled || f.ExecutorPin != "" {
		t.Fatal("fixture needs no grant or pin")
	}
	q := ClassifyS5PathsAndRelations(f, f.S5Policy)
	if !q.NeedsS5Evidence || !q.RelationRequired {
		t.Fatal(q)
	}
	if d := MayPrepareS5(f, q, now); !d.Allowed {
		t.Fatal(d)
	}
	if EligibleToClaim(f, now).Allowed || EligibleToPush(f, now).Allowed {
		t.Fatal("legacy granted S5")
	}
	if why, soft := AuthorizeLanding(f, VerifiedS5{}, now); why == "" || !slices.Contains(soft, rules.LandingFloorSoft) {
		t.Fatal(why, soft)
	}
	if f.ReservedToday != 0 || f.Operation.R != "" {
		t.Fatal("preparation changed quota or result")
	}
}
func TestS5EvidenceAndFinalBinding(t *testing.T) {
	f, ci := s5Ready(t)
	v, why := VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
	if why != "" {
		t.Fatal(why)
	}
	if why, soft := AuthorizeLanding(f, v, epoch.Add(time.Hour)); why != "" || len(soft) > 0 {
		t.Fatal(why, soft)
	}
	cases := map[string]struct {
		mutate func(*Facts, *S5CIResult)
		want   string
	}{
		"forged labels":            {func(_ *Facts, c *S5CIResult) { c.IdentityVerified = false }, "S5_CI_IDENTITY"},
		"legacy main UI":           {func(_ *Facts, c *S5CIResult) { c.Key.UISHA = "main" }, "S5_BINDING"},
		"old generation":           {func(_ *Facts, c *S5CIResult) { c.Key.Generation = 2 }, "S5_GENERATION_STALE"},
		"UID replaced":             {func(_ *Facts, c *S5CIResult) { c.UID = "other" }, "S5_CI_IDENTITY"},
		"full digest collision":    {func(_ *Facts, c *S5CIResult) { c.KeyDigest = strings.Repeat("f", 64) }, "S5_CI_IDENTITY"},
		"new failure":              {func(_ *Facts, c *S5CIResult) { c.Status = "Failed" }, "S5_BEHAVIOR"},
		"missing mandatory result": {func(_ *Facts, c *S5CIResult) { c.Evidence = nil }, "S5_EVIDENCE_MISSING"},
		"missing changed file":     {func(_ *Facts, c *S5CIResult) { c.Evidence.Files = c.Evidence.Files[:0] }, "S5_EVIDENCE_MISSING"},
		"unchanged enrolled file": {func(_ *Facts, c *S5CIResult) {
			c.Evidence.Files = append(c.Evidence.Files, S5FileEvidence{rules.S5Paths()[1], A, A})
		}, "S5_BINDING"},
		"candidate forged evidence": {func(_ *Facts, c *S5CIResult) { c.Evidence.RuleDigest = strings.Repeat("f", 64) }, "S5_BINDING"},
		"content failure":           {func(_ *Facts, c *S5CIResult) { c.Evidence.Findings = []S5Finding{{Code: "S5_CALLER_CHANGED"}} }, "S5_CALLER_CHANGED"},
		"old CI":                    {func(f *Facts, c *S5CIResult) { c.CreatedAt = f.Operation.PreparedAt.Add(-time.Second) }, "S5_CI_IDENTITY"},
		"profile changed":           {func(f *Facts, _ *S5CIResult) { f.S5Policy.ProfileDigest = strings.Repeat("c", 64) }, "S5_BINDING"},
		"rule changed":              {func(f *Facts, _ *S5CIResult) { f.Rule.Version++ }, "S5_BINDING"},
		"same A new M0 R":           {func(f *Facts, _ *S5CIResult) { f.Operation.M0 = U; f.S5Patch.M0 = U; f.S5Patch.ParentR = U }, "S5_BINDING"},
		"old patch":                 {func(f *Facts, _ *S5CIResult) { f.S5Patch.R = U }, "S5_PATCH_MISMATCH"},
		"same provider":             {func(f *Facts, _ *S5CIResult) { f.S5Review.ReviewerProvider = f.S5Review.ImplementerProvider }, "S5_REVIEW_PROVIDER"},
		"no provider":               {func(f *Facts, _ *S5CIResult) { f.S5Review.ReviewerProvider = "" }, "S5_REVIEW_PROVIDER"},
		"new review fail":           {func(f *Facts, _ *S5CIResult) { f.S5Review.Verdict = "fail" }, "S5_REVIEW_BINDING"},
		"wrong attempt":             {func(f *Facts, _ *S5CIResult) { f.S5Review.AcceptedAttempt = "other" }, "S5_REVIEW_BINDING"},
		"missing artifact":          {func(f *Facts, _ *S5CIResult) { f.S5Review.ReviewArtifactID = "" }, "S5_REVIEW_MISSING"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f, c := s5Ready(t)
			tc.mutate(&f, &c)
			_, why := VerifyS5Evidence(c, f, f.S5Policy, f.S5Review)
			if why != tc.want {
				t.Fatalf("want %s got %s", tc.want, why)
			}
		})
	}
	for name, mutate := range map[string]func(*Facts){"expired": func(f *Facts) { f.Rule.Expires = epoch }, "tightened": func(f *Facts) { f.Rule.Landing.Repos[0].Allow = []string{"console/src/components/**/*.tsx"} }, "review": func(f *Facts) { f.S5Review.Verdict = "fail" }, "policy": func(f *Facts) { f.S5Policy.TestsDigest = strings.Repeat("e", 64) }, "main": func(f *Facts) { f.Operation.M0 = U }, "quota": func(f *Facts) { f.ReservedToday = 5 }} {
		t.Run("final "+name, func(t *testing.T) {
			f, c := s5Ready(t)
			v, why := VerifyS5Evidence(c, f, f.S5Policy, f.S5Review)
			if why != "" {
				t.Fatal(why)
			}
			mutate(&f)
			if why, _ := AuthorizeLanding(f, v, epoch.Add(time.Hour)); why == "" {
				t.Fatal("stale verified capability authorized")
			}
		})
	}
}
func TestS5MixedAndCallerOnly(t *testing.T) {
	f := s5Facts()
	display := eligibleFacts().Changes[0]
	f.Changes = append(f.Changes, display)
	f.Changes[1].AddedText = "fetch('/action')"
	if got := rules.S5Refusals(f.Rule.Landing, f.Repo, f.Changes); !slices.Contains(got, rules.LandingPresentation) {
		t.Fatal(got)
	}
	f.Changes = []rules.LandingChange{display}
	q := ClassifyS5PathsAndRelations(f, f.S5Policy)
	if !q.NeedsS5Evidence || !q.RelationRequired || len(q.CandidatePaths) != 0 {
		t.Fatal(q)
	}
	if EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("caller-only bypass")
	}
	for _, kind := range []string{"A", "D", "R", "C", "T"} {
		f := s5Facts()
		f.Changes[0].Kind = kind
		q := ClassifyS5PathsAndRelations(f, f.S5Policy)
		if !slices.Contains(q.Reasons, "S5_CHANGE_KIND") {
			t.Fatal(kind, q)
		}
	}
	f = s5Facts()
	f.Changes[0].OldPath = "api/internal/agentrt/store/landing.go"
	f.Changes[0].Kind = "R"
	if d := PreparationDecision(f, epoch.Add(time.Hour)); d.Allowed || d.Reason != "floor" {
		t.Fatal(d)
	}
}
func TestS5LegacyAndGoRouting(t *testing.T) {
	for _, path := range append(rules.S5Paths(), "console/src/components/A.tsx", "api/internal/example/scalar.go", "api/internal/agentrt/store/s5.go") {
		for _, text := range []string{"<p>Ready</p>", "fetch('/action')"} {
			f := eligibleFacts()
			f.Changes[0].OldPath = path
			f.Changes[0].NewPath = path
			f.Changes[0].AddedText = text
			before := EligibleToClaim(f, epoch.Add(time.Hour))
			after := PreparationDecision(f, epoch.Add(time.Hour))
			if !reflect.DeepEqual(before, after) {
				t.Fatal(path, before, after)
			}
			if ClassifyS5PathsAndRelations(f, f.S5Policy).NeedsS5Evidence {
				t.Fatal("v2 activated S5")
			}
		}
	}
	// Go is closed for this batch (§4): no Go path is an S5 candidate, so a
	// Go change never reaches an S5 lane (the checker's own Go refusal,
	// S5_LANGUAGE_DISABLED, is in rules/s5check).
	for _, path := range append(rules.S5Paths(), "api/internal/agentrt/landing/s5.go", "console/src/lib/patrol/cronSchedule.go") {
		if strings.HasSuffix(path, ".go") == rules.S5Candidate("bifrost-platform", path) {
			t.Fatal("Go S5 candidate", path)
		}
	}
}
func TestS5ProtocolRecoveryAndGeneration(t *testing.T) {
	f, _ := s5Ready(t)
	k := f.Operation.S5.Key
	op := k.Op
	entries := []Entry{}
	add := func(kind string, fields Record) error {
		// v1.3 carries only the S5 records (C1); the rest stay at v1.2.
		if _, set := fields["v"]; !set {
			fields["v"] = "1.2"
			if strings.HasPrefix(kind, "s5_") {
				fields["v"] = S5Version
			}
		}
		fields["kind"] = kind
		fields["op"] = op
		e := Entry{epoch.Add(time.Duration(len(entries)+1) * time.Second), fields}
		copy := append(slices.Clone(entries), e)
		if _, err := Fold(copy); err != nil {
			return err
		}
		entries = copy
		return nil
	}
	if err := add("claimed", Record{"v": S5Version, "task": f.Task, "attempt": f.Attempt, "A": f.A, "rule_version": 3}); err == nil {
		t.Fatal("v1.3 accepted for a non-S5 kind")
	}
	if err := add("claimed", Record{"task": f.Task, "attempt": f.Attempt, "A": f.A, "rule_version": 3}); err != nil {
		t.Fatal(err)
	}
	if err := add("s5_required", Record{}); err != nil {
		t.Fatal(err)
	}
	if err := add("prepared", Record{"R": R, "M0": M0, "committer_date": epoch.Format(time.RFC3339), "dry_run": false}); err != nil {
		t.Fatal(err)
	}
	ref := "refs/heads/land/prod/" + f.Task + "/1"
	if err := add("branch_intent", Record{"ref": ref, "R": R}); err == nil {
		t.Fatal("branch intent without key")
	}
	raw, _ := json.Marshal(k)
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	_ = dec.Decode(&obj)
	kd, _ := k.Digest()
	if err := add("s5_key", Record{"key": obj, "key_digest": kd}); err != nil {
		t.Fatal(err)
	}
	if err := add("s5_key", Record{"key": obj, "key_digest": kd}); err == nil {
		t.Fatal("duplicate generation")
	}
	if err := add("branch_intent", Record{"ref": ref, "R": R}); err != nil {
		t.Fatal(err)
	}
	if err := add("branch_pushed", Record{"ref": ref, "R": R}); err != nil {
		t.Fatal(err)
	}
	if err := add("s5_run", Record{"key_digest": kd, "uid": "uid-1"}); err != nil {
		t.Fatal(err)
	}
	if err := add("s5_run", Record{"key_digest": kd, "uid": "uid-2"}); err == nil {
		t.Fatal("UID rebound")
	}
	k.Generation++
	k.ProfileDigest = strings.Repeat("e", 64)
	raw, _ = json.Marshal(k)
	obj = nil
	dec = json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	_ = dec.Decode(&obj)
	kd, _ = k.Digest()
	if err := add("s5_key", Record{"key": obj, "key_digest": kd}); err != nil {
		t.Fatal(err)
	}
	state, err := Fold(entries)
	if err != nil {
		t.Fatal(err)
	}
	o := state.Operations[op]
	if o.S5.Key.Generation != 2 || o.S5.UID != "" || o.CI != nil || o.Phase != "branch_pushed" || len(state.Reservations) != 0 {
		t.Fatalf("%+v", o)
	}
	for n := 1; n <= len(entries); n++ {
		if _, err := Fold(entries[:n]); err != nil {
			t.Fatal("restart", n, err)
		}
	}
	old := Record{"v": "1.2", "kind": "s5_run", "op": op, "key_digest": kd, "uid": "uid"}
	if _, err := Parse(old); err == nil {
		t.Fatal("old protocol accepted S5")
	}
	// The operation ends with an S5 code only at v1.3, and v1.3 ends it
	// only with an S5 code.
	for _, bad := range []Record{
		{"v": "1.2", "kind": "failed", "op": op, "why": "S5_BEHAVIOR"},
		{"v": S5Version, "kind": "failed", "op": op, "why": "bound"},
		{"v": S5Version, "kind": "failed", "op": op, "why": "S5_behavior"},
		{"v": S5Version, "kind": "s5_key", "op": op, "key": obj, "key_digest": kd, "retry": "manual"},
	} {
		if _, err := Parse(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if err := add("failed", Record{"v": S5Version, "why": "S5_CALLER_CHANGED"}); err != nil {
		t.Fatal(err)
	}
	state, err = Fold(entries)
	if err != nil {
		t.Fatal(err)
	}
	if o := state.Operations[op]; o.Phase != "failed" || o.Outcome != "S5_CALLER_CHANGED" || o.Active() || !o.BlocksClaim() {
		t.Fatalf("S5 refusal did not end the operation: %+v", o)
	}
	name, err := k.RunName()
	if err != nil || len(name) > 58 || !strings.HasSuffix(name, "-g2") {
		t.Fatal(name, err)
	}
}

func TestS5PreservesExplicitGrantLaneForNondeferrableChanges(t *testing.T) {
	f := s5Facts()
	f.GrantEnabled = true
	f.ExecutorPin = "coordinator"
	core := "api/internal/agentrt/store/landing.go"
	f.Changes = append(f.Changes, rules.LandingChange{Kind: "M", OldPath: core, NewPath: core, OldMode: "100644", NewMode: "100644", Added: 1})
	if !ClassifyS5PathsAndRelations(f, f.S5Policy).NeedsS5Evidence || S5AutomaticRoute(f) {
		t.Fatal("core change selected automatic S5")
	}
	if d := PreparationDecision(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("original D preparation changed", d)
	}
	if d := EligibleToPush(f, epoch.Add(time.Hour)); d.Allowed {
		t.Fatal("D preparation reached main")
	}
	f.Operation.S5.Required = true
	if !S5AutomaticRoute(f) {
		t.Fatal("persisted S5 downgraded to D")
	}
}

// B3: a release-freeze failure may open the next generation with the same
// material, explicitly, after a bound run, within the count and backoff.
func TestS5InfrastructureRetryGeneration(t *testing.T) {
	f, _ := s5Ready(t)
	k := f.Operation.S5.Key
	op := k.Op
	at := epoch
	entries := []Entry{}
	add := func(after time.Duration, kind string, fields Record) error {
		fields["v"] = "1.2"
		if strings.HasPrefix(kind, "s5_") {
			fields["v"] = S5Version
		}
		fields["kind"], fields["op"] = kind, op
		e := Entry{at.Add(after), fields}
		next := append(slices.Clone(entries), e)
		if _, err := Fold(next); err != nil {
			return err
		}
		entries, at = next, e.At
		return nil
	}
	key := func(k S5CIKey) (map[string]any, string) {
		raw, _ := json.Marshal(k)
		var obj map[string]any
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		_ = dec.Decode(&obj)
		kd, _ := k.Digest()
		return obj, kd
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(add(time.Second, "claimed", Record{"task": f.Task, "attempt": f.Attempt, "A": f.A, "rule_version": 3}))
	must(add(time.Second, "s5_required", Record{}))
	must(add(time.Second, "prepared", Record{"R": R, "M0": M0, "committer_date": epoch.Format(time.RFC3339), "dry_run": false}))
	obj, kd := key(k)
	must(add(time.Second, "s5_key", Record{"key": obj, "key_digest": kd}))
	retry := func(after time.Duration) error {
		next := k
		next.Generation++
		obj, kd := key(next)
		if err := add(after, "s5_key", Record{"key": obj, "key_digest": kd, "retry": "infra"}); err != nil {
			return err
		}
		k = next
		return nil
	}
	if retry(time.Hour) == nil {
		t.Fatal("retry before any run was bound")
	}
	must(add(time.Second, "s5_run", Record{"key_digest": kd, "uid": "uid-1"}))
	if retry(S5InfraRetryAfter(0)-2*time.Second) == nil {
		t.Fatal("retry inside the backoff")
	}
	for n := 0; n < S5InfraRetries; n++ {
		must(retry(S5InfraRetryAfter(n)))
		_, kd = key(k)
		must(add(time.Second, "s5_run", Record{"key_digest": kd, "uid": "uid-retry"}))
	}
	if retry(24*time.Hour) == nil {
		t.Fatal("retry beyond the bound")
	}
	s, err := Fold(entries)
	if err != nil {
		t.Fatal(err)
	}
	if o := s.Operations[op]; o.S5.InfraRetries != S5InfraRetries || o.S5.Key.Generation != uint64(1+S5InfraRetries) {
		t.Fatalf("%+v", o.S5)
	}
	// A failed generation is never retried, and an ordinary key must still
	// change material.
	must(add(time.Second, "s5_failed", Record{"key_digest": kd, "why": "S5_BEHAVIOR"}))
	if retry(24*time.Hour) == nil {
		t.Fatal("failed generation retried")
	}
	same := k
	same.Generation++
	obj, kd = key(same)
	if add(time.Hour, "s5_key", Record{"key": obj, "key_digest": kd}) == nil {
		t.Fatal("unchanged material without retry marker")
	}
}
