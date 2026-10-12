package delivery

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func infraTektonDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../bifrost-trade-infra/k8s/cicd/tekton"))
	if _, err := os.Stat(dir); err != nil {
		// clone-infra places the checkout beside platform in Tekton's source workspace.
		// CI YAML does not set BIFROST_CI, so also recognize the workspace path.
		if os.Getenv("BIFROST_CI") != "" || os.Getenv("CI") != "" || strings.HasPrefix(filepath.ToSlash(file), "/workspace/") {
			t.Fatalf("CI requires sibling infra checkout: %v", err)
		}
		t.Skipf("sibling infra checkout absent: %v", err)
	}
	return dir
}

func readYAMLObjects(t *testing.T, path string) []*unstructured.Unstructured {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := kyaml.NewYAMLOrJSONDecoder(f, 4096)
	var out []*unstructured.Unstructured
	for {
		var m map[string]any
		err := dec.Decode(&m)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(m) > 0 {
			out = append(out, &unstructured.Unstructured{Object: m})
		}
	}
	return out
}

func infraPipelines(t *testing.T) map[string]*unstructured.Unstructured {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(infraTektonDir(t), "pipeline-*.yaml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("pipeline definitions: %v", err)
	}
	out := map[string]*unstructured.Unstructured{}
	for _, path := range paths {
		for _, obj := range readYAMLObjects(t, path) {
			if obj.GetKind() == "Pipeline" {
				out[obj.GetName()] = obj
			}
		}
	}
	return out
}

func baselineRunSpecs(t *testing.T) map[string]any {
	t.Helper()
	pipes := infraPipelines(t)
	names := map[string]bool{"unknown": true, "bifrost-smoke": true, "bifrost-clone-frontend-smoke": true, "bifrost-build-flex-query": true}
	for name := range kanikoPipelineNames {
		names[name] = true
	}
	out := map[string]any{}
	for name := range names {
		pipe := pipes[name]
		if pipe == nil {
			pipe = &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
		}
		for _, tagged := range []bool{false, true} {
			tag := ""
			suffix := "empty"
			if tagged {
				tag = "1.2.3"
				suffix = "tagged"
			}
			spec, rev, err := buildPipelineRunSpec(name, factSHA1, tag, nil, pipe)
			if err != nil {
				t.Fatal(err)
			}
			out[name+"/"+suffix] = map[string]any{"spec": spec, "revision": rev}
		}
	}
	return out
}

func TestNonCIRunsUnchangedFromMain(t *testing.T) {
	want, err := os.ReadFile("testdata/non_ci_runs_main.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(baselineRunSpecs(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(append(got, '\n')) != string(want) {
		t.Fatal("non-CI run JSON differs from main 2726c47 baseline")
	}
}

func TestPipelineRunRequirementsRatchet(t *testing.T) {
	count := 0
	for name, pipe := range infraPipelines(t) {
		if !isCIPipeline(name) && !pipelineTakesRevision(name) && len(pipelineRunWorkspaces(name)) == 0 && !isKanikoPipeline(name) {
			continue
		}
		count++
		t.Run(name, func(t *testing.T) {
			extra := map[string]string{}
			if name == "bifrost-ci-python" {
				extra["repo"] = pythonCIRepos[0]
			}
			spec, _, err := buildPipelineRunSpec(name, factSHA1, "1.2.3", extra, pipe)
			if err != nil {
				t.Fatal(err)
			}
			run, err := unstructuredFrom(map[string]any{"spec": spec})
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRunRequirements(pipe, run); err != nil {
				t.Fatal(err)
			}
		})
	}
	if count < 13 {
		t.Fatalf("only %d supported pipelines checked", count)
	}
}

func TestCIRunTriggerEquivalence(t *testing.T) {
	pipes := infraPipelines(t)
	count := 0
	for _, obj := range readYAMLObjects(t, filepath.Join(infraTektonDir(t), "trigger-trade-ci.yaml")) {
		if obj.GetKind() != "TriggerTemplate" {
			continue
		}
		templates, ok, err := unstructured.NestedSlice(obj.Object, "spec", "resourcetemplates")
		if err != nil || !ok || len(templates) != 1 {
			t.Fatal(obj.GetName(), err)
		}
		template := templates[0].(map[string]any)
		name, _, _ := unstructured.NestedString(template, "spec", "pipelineRef", "name")
		if !isCIPipeline(name) {
			continue
		}
		count++
		defaults := map[string]string{}
		raw, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
		for _, p := range raw {
			m := p.(map[string]any)
			if v, ok := m["default"].(string); ok {
				defaults[m["name"].(string)] = v
			}
		}
		defaults["revision"] = factSHA1
		repos := []string{"bifrost-platform"}
		if name == "bifrost-ci-frontend" {
			repos = []string{"bifrost-trade-frontend"}
		}
		if name == "bifrost-ci-python" {
			repos = pythonCIRepos
		}
		for _, repo := range repos {
			t.Run(name+"/"+repo, func(t *testing.T) {
				defaults["repo"] = repo
				b, err := json.Marshal(template)
				if err != nil {
					t.Fatal(err)
				}
				text := string(b)
				for k, v := range defaults {
					text = strings.ReplaceAll(text, "$(tt.params."+k+")", v)
				}
				var expected map[string]any
				if err := json.Unmarshal([]byte(text), &expected); err != nil {
					t.Fatal(err)
				}
				extra := map[string]string{}
				if name == "bifrost-ci-python" {
					extra["repo"] = repo
				}
				spec, _, err := buildPipelineRunSpec(name, factSHA1, "", extra, pipes[name])
				if err != nil {
					t.Fatal(err)
				}
				run, err := unstructuredFrom(map[string]any{"spec": spec})
				if err != nil {
					t.Fatal(err)
				}
				// Param order is irrelevant, but names, values and multiplicity must match.
				gotParams, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
				wantParams, _, _ := unstructured.NestedSlice(expected, "spec", "params")
				byName := func(items []any) map[string]any {
					m := map[string]any{}
					for _, p := range items {
						x := p.(map[string]any)
						m[x["name"].(string)] = x["value"]
					}
					return m
				}
				assertJSONEqual(t, byName(gotParams), byName(wantParams))
				if len(gotParams) != len(wantParams) {
					t.Fatal("param count differs")
				}
				for _, field := range []string{"workspaces", "taskRunTemplate"} {
					want, _, _ := unstructured.NestedFieldNoCopy(expected, "spec", field)
					got, _, _ := unstructured.NestedFieldNoCopy(run.Object, "spec", field)
					assertJSONEqual(t, got, want)
				}
				s, dyn, _ := contractService(t, pipes[name], "present")
				response, _, err := s.StartPipelineRun(context.Background(), name, factSHA1, "", "", extra)
				if err != nil || !response.OK {
					t.Fatal(response, err)
				}
				created := createdContractRun(t, dyn)
				if created.GetLabels()["bifrost.io/repo"] != repo || created.GetLabels()["bifrost.io/revision"] != factSHA1 || created.GetLabels()["bifrost.io/trigger"] != "platform-api" || created.GetLabels()["tekton.dev/pipeline"] != name {
					t.Fatal(created.GetLabels())
				}
				assertJSONEqual(t, created.Object["spec"], run.Object["spec"])
				wantLabel, _, _ := unstructured.NestedString(expected, "metadata", "labels", "bifrost.io/repo")
				if wantLabel != repo {
					t.Fatalf("trigger repo label %q differs", wantLabel)
				}
			})
		}
	}
	if count != 3 {
		t.Fatalf("matched %d CI templates", count)
	}
}

func TestPythonCIRepoFilterEquivalence(t *testing.T) {
	found := false
	for _, obj := range readYAMLObjects(t, filepath.Join(infraTektonDir(t), "trigger-trade-ci.yaml")) {
		if obj.GetKind() != "EventListener" {
			continue
		}
		triggers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "triggers")
		for _, raw := range triggers {
			trigger := raw.(map[string]any)
			if trigger["name"] != "python-ci" {
				continue
			}
			found = true
			interceptors := trigger["interceptors"].([]any)
			filter := ""
			for _, raw := range interceptors {
				for _, p := range raw.(map[string]any)["params"].([]any) {
					m := p.(map[string]any)
					if m["name"] == "filter" {
						filter = m["value"].(string)
					}
				}
			}
			re := regexp.MustCompile(`body\.repository\.name in \[([^]]+)\]`)
			match := re.FindStringSubmatch(filter)
			if len(match) != 2 {
				t.Fatal("cannot parse python-ci repo filter", filter)
			}
			list := []string{}
			for _, part := range strings.Split(match[1], ",") {
				list = append(list, strings.Trim(strings.TrimSpace(part), "'"))
			}
			got := append([]string{}, pythonCIRepos...)
			sort.Strings(got)
			sort.Strings(list)
			assertJSONEqual(t, got, list)
		}
	}
	if !found {
		t.Fatal("python-ci filter missing")
	}
}
