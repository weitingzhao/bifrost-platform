package landingci

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var s5FailedCondition = []any{map[string]any{"type": "Succeeded", "status": "False"}}

// s5FailRun marks the run failed and the named children failed; children
// after a failed freeze check were skipped and have no TaskRun reference.
func s5FailRun(t *testing.T, o []*unstructured.Unstructured, failed ...string) {
	t.Helper()
	setField(t, o[0], s5FailedCondition, "status", "conditions")
	index := map[string]int{"freeze-check": 6, "s5-fetch": 1, "s5-check": 3}
	for _, stage := range failed {
		setField(t, o[index[stage]], s5FailedCondition, "status", "conditions")
	}
	if len(failed) == 1 && failed[0] == "freeze-check" {
		children, _, _ := unstructured.NestedSlice(o[0].Object, "status", "childReferences")
		setField(t, o[0], children[:1], "status", "childReferences")
		setField(t, o[0], []any{map[string]any{"name": "s5-fetch"}, map[string]any{"name": "s5-check"}}, "status", "skippedTasks")
	}
}

// B1/B3: an identity-checked failed run keeps a specific code, and only the
// refused release freeze check is an infrastructure (retryable) failure.
func TestS5FailedRunAttribution(t *testing.T) {
	cases := []struct {
		failed    []string
		reason    string
		retryable bool
	}{
		{[]string{"freeze-check"}, "S5_EVIDENCE_MISSING", true},
		{[]string{"s5-fetch"}, "S5_EVIDENCE_MISSING", false},
		{[]string{"s5-check"}, "S5_BEHAVIOR", false},
		{nil, "S5_BEHAVIOR", false}, // the run's own timeout or cancellation
	}
	for _, tc := range cases {
		k := s5TestKey()
		c, id, o := s5Fixture(t, k)
		s5FailRun(t, o, tc.failed...)
		got := s5ReadFixture(t, k, c, id, o)
		if got.Status != "Failed" || got.Reason != tc.reason || got.Retryable != tc.retryable {
			t.Fatal(tc.failed, got)
		}
		r := s5LandingResult(k, id, got)
		if r.Status != "Failed" || r.Reason != tc.reason || r.Retryable != tc.retryable || r.IdentityVerified || r.Evidence != nil {
			t.Fatal("landing result", tc.failed, r)
		}
	}
	// A failed run is still identity-checked first: a forged pipeline or a
	// foreign child is never an infrastructure retry.
	for name, mutate := range map[string]func([]*unstructured.Unstructured){
		"forged pipeline": func(o []*unstructured.Unstructured) {
			setField(t, o[0], []any{}, "status", "pipelineSpec", "tasks")
		},
		"foreign freeze child": func(o []*unstructured.Unstructured) { o[6].SetOwnerReferences(nil) },
		"unknown child": func(o []*unstructured.Unstructured) {
			setField(t, o[0], []any{map[string]any{"name": o[6].GetName(), "kind": "TaskRun", "apiVersion": "tekton.dev/v1", "pipelineTaskName": "other"}}, "status", "childReferences")
		},
	} {
		k := s5TestKey()
		c, id, o := s5Fixture(t, k)
		s5FailRun(t, o, "freeze-check")
		mutate(o)
		if got := s5ReadFixture(t, k, c, id, o); got.Reason != "S5_CI_IDENTITY" || got.Retryable {
			t.Fatal(name, got)
		}
	}
}

// B1: a bound refusal reported by the checker keeps its finding code.
func TestS5CheckerFindingCodeIsKept(t *testing.T) {
	finding := map[string]any{"code": "S5_CALLER_CHANGED", "path": s5LandingPath, "line": 1, "column": 2, "detail": "x"}
	cases := map[string]struct {
		passed   any
		findings []any
		want     string
	}{
		"finding":           {false, []any{finding}, "S5_CALLER_CHANGED"},
		"first of several":  {false, []any{finding, map[string]any{"code": "S5_TYPE_CONTRACT"}}, "S5_CALLER_CHANGED"},
		"malformed code":    {false, []any{map[string]any{"code": "caller"}}, "S5_BEHAVIOR"},
		"no finding":        {false, []any{}, "S5_BEHAVIOR"},
		"passed with codes": {true, []any{finding}, "S5_BEHAVIOR"},
		// Gate/lifecycle codes are the platform's own; from the checker
		// they are a deterministic behavior failure, never a wait.
		"wait-class preparation": {false, []any{map[string]any{"code": "S5_PREPARATION_ONLY"}}, "S5_BEHAVIOR"},
		"wait-class evidence":    {false, []any{map[string]any{"code": "S5_EVIDENCE_MISSING"}}, "S5_BEHAVIOR"},
		"wait-class review":      {false, []any{map[string]any{"code": "S5_REVIEW_MISSING"}}, "S5_BEHAVIOR"},
		"unknown code":           {false, []any{map[string]any{"code": "S5_SOMETHING_NEW"}}, "S5_BEHAVIOR"},
	}
	for name, tc := range cases {
		k := s5TestKey()
		values := s5TestResults(k, "uid")
		var e map[string]any
		_ = decodeS5([]byte(values["evidence"]), &e)
		e["passed"], e["findings"] = tc.passed, tc.findings
		b, _ := canonicalS5(e)
		values["evidence"], values["evidence-digest"] = string(b), s5Hash(b)
		if content, why := s5CheckedResults(values, k, "uid"); content != nil || why != tc.want {
			t.Fatal(name, why)
		}
		// Bindings are still checked before the code is believed.
		e["ciUID"] = "other"
		b, _ = canonicalS5(e)
		values["evidence"], values["evidence-digest"] = string(b), s5Hash(b)
		if _, why := s5CheckedResults(values, k, "uid"); why != "S5_BINDING" {
			t.Fatal(name, "unbound refusal", why)
		}
	}
}

// B5: only the fetch step may carry the pinned reader secret reference.
func TestS5PodReaderSecretOnlyInFetch(t *testing.T) {
	k := s5TestKey()
	c, id, o := s5Fixture(t, k)
	cs, _, _ := unstructured.NestedSlice(o[2].Object, "spec", "containers")
	secret := 0
	for _, v := range cs[0].(map[string]any)["env"].([]any) {
		if _, ok := v.(map[string]any)["valueFrom"]; ok {
			secret++
		}
	}
	if secret != 2 {
		t.Fatal("fixture fetch pod lacks the reader secret", secret)
	}
	if got := s5ReadFixture(t, k, c, id, o); got.Status != "Succeeded" {
		t.Fatal(got)
	}
	for name, pod := range map[string]int{"other secret in fetch": 2, "secret in check": 4} {
		_, _, o := s5Fixture(t, k)
		cs, _, _ := unstructured.NestedSlice(o[pod].Object, "spec", "containers")
		co := cs[0].(map[string]any)
		ref := map[string]any{"name": "GIT_PASSWORD", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "gitea-admin", "key": "password"}}}
		env := []any{}
		for _, v := range co["env"].([]any) {
			if v.(map[string]any)["name"] != "GIT_PASSWORD" {
				env = append(env, v)
			}
		}
		co["env"] = append(env, ref)
		setField(t, o[pod], cs, "spec", "containers")
		if got := s5ReadFixture(t, k, c, id, o); got.Status == "Succeeded" || got.Reason != "S5_CI_IDENTITY" {
			t.Fatal(name, got)
		}
	}
}

// A1: Tekton v1.6.0 merges taskRunSpecs[].podTemplate over
// taskRunTemplate.podTemplate field by field, so the freeze override must
// grant the token itself. Only the freeze check gets one.
func TestS5FreezeCheckAloneRunsWithToken(t *testing.T) {
	c := s5TestContract(t)
	// The reviewer's independent merge: default fields, then the override's.
	merged := map[string]any{}
	for k, v := range c.RunTemplate["taskRunTemplate"].(map[string]any)["podTemplate"].(map[string]any) {
		merged[k] = v
	}
	for k, v := range c.RunTemplate["taskRunSpecs"].([]any)[0].(map[string]any)["podTemplate"].(map[string]any) {
		merged[k] = v
	}
	if merged["automountServiceAccountToken"] != true {
		t.Fatal("freeze-check pod gets no token after Tekton's merge", merged)
	}
	if sa, pod := s5TaskRunTemplate(c, "freeze-check"); sa != "default" || !s5Same(pod, merged) {
		t.Fatal("reader merge differs", sa, pod)
	}
	for _, stage := range []string{"s5-fetch", "s5-check"} {
		if sa, pod := s5TaskRunTemplate(c, stage); sa != "bifrost-s5" || pod["automountServiceAccountToken"] != false {
			t.Fatal("S5 task with a token", stage, sa, pod)
		}
	}
	// The trust root refuses a template that breaks either side.
	for name, mutate := range map[string]func(s5Contract){
		"freeze without token": func(c s5Contract) {
			delete(c.RunTemplate["taskRunSpecs"].([]any)[0].(map[string]any)["podTemplate"].(map[string]any), "automountServiceAccountToken")
		},
		"S5 task override": func(c s5Contract) {
			c.RunTemplate["taskRunSpecs"] = append(c.RunTemplate["taskRunSpecs"].([]any), map[string]any{"pipelineTaskName": "s5-check", "podTemplate": map[string]any{"automountServiceAccountToken": true}})
		},
	} {
		bad := s5TestContract(t)
		mutate(bad)
		if _, err := bad.policy(); err == nil {
			t.Fatal("trust root accepted", name)
		}
	}
	k := s5TestKey()
	freezeTask, freezePod, checkTask, checkPod := 6, 7, 3, 4
	for name, mutate := range map[string]func(*testing.T, []*unstructured.Unstructured){
		"freeze TaskRun unmerged template": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezeTask], map[string]any{"nodeSelector": map[string]any{"kubernetes.io/arch": "amd64"}}, "spec", "podTemplate")
		},
		"freeze TaskRun without token": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezeTask], false, "spec", "podTemplate", "automountServiceAccountToken")
		},
		"freeze TaskRun as bifrost-s5": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezeTask], "bifrost-s5", "spec", "serviceAccountName")
		},
		"freeze TaskRun with trees": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezeTask], []any{map[string]any{"name": "trees", "persistentVolumeClaim": map[string]any{"claimName": "trees"}}}, "spec", "workspaces")
		},
		"freeze Pod without token": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezePod], false, "spec", "automountServiceAccountToken")
		},
		"freeze Pod other SA": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezePod], "tekton-deliver", "spec", "serviceAccountName")
		},
		"freeze Pod mounts the trees": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[freezePod], []any{map[string]any{"name": "t", "persistentVolumeClaim": map[string]any{"claimName": "trees"}}}, "spec", "volumes")
		},
		"freeze Pod foreign": func(t *testing.T, o []*unstructured.Unstructured) { o[freezePod].SetOwnerReferences(nil) },
		"check TaskRun with token": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[checkTask], true, "spec", "podTemplate", "automountServiceAccountToken")
		},
		"check Pod with token": func(t *testing.T, o []*unstructured.Unstructured) {
			setField(t, o[checkPod], true, "spec", "automountServiceAccountToken")
		},
	} {
		c, id, o := s5Fixture(t, k)
		if got := s5ReadFixture(t, k, c, id, o); got.Status != "Succeeded" {
			t.Fatal("positive control", got)
		}
		mutate(t, o)
		if got := s5ReadFixture(t, k, c, id, o); got.Status == "Succeeded" || got.Reason != "S5_CI_IDENTITY" {
			t.Fatal(name, got)
		}
	}
}
