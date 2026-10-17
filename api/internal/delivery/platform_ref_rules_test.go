package delivery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPlatformSameSHARefused(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%v", name, explicit), func(t *testing.T) {
				s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA1), nil, "")
				params := map[string]string{}
				if explicit {
					params["uiRevision"] = factSHA1
				}
				assertPinnedCall(t, s, dyn, name, factSHA1, params, "pass uiRevision for bifrost-ui")
				if len(m.paths) != 0 {
					t.Fatal("same SHA reached mirrors", m.paths)
				}
			})
		}
	}
}

func TestPlatformRevisionFromParamsIsEffective(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA2), nil, "")
		assertPinnedCall(t, s, dyn, name, "", map[string]string{"revision": factSHA1, "uiRevision": factSHA2}, "")
		assertPinnedSync(t, m, false)
	}
}

func TestPlatformInvalidInputsNoMirrorRequest(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, ref := range []string{strings.Repeat("a", 7), strings.Repeat("a", 39), strings.Repeat("A", 40), strings.Repeat("A", 7)} {
			for _, param := range []string{"", "revision", "uiRevision"} {
				t.Run(name+"/"+param+"/"+ref, func(t *testing.T) {
					s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA2), nil, "")
					rev := ""
					params := map[string]string{"uiRevision": factSHA2}
					if param == "" {
						rev = ref
					} else {
						params[param] = ref
					}
					assertPinnedCall(t, s, dyn, name, rev, params, "use the full 40-character lowercase SHA")
					if len(m.paths) != 0 {
						t.Fatal(m.paths)
					}
				})
			}
		}
		t.Run(name+"/conflict", func(t *testing.T) {
			s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA2), nil, "")
			resp, _, err := s.StartPipelineRun(context.Background(), name, "main", "", "tester", map[string]string{"revision": factSHA1, "uiRevision": factSHA2})
			if err == nil || resp.OK || resp.Changed {
				t.Fatal(resp, err)
			}
			for _, word := range []string{`revision "main"`, "params.revision", factSHA1} {
				if !strings.Contains(resp.Message, word) {
					t.Fatal(resp.Message)
				}
			}
			assertNoContractCreate(t, dyn)
			if len(m.paths) != 0 {
				t.Fatal(m.paths)
			}
		})
	}
}

func TestPlatformUnknownDoesNotMaskMissing(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA2), []string{"bifrost-ui"}, "platform unknown")
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		resp, _, err := s.StartPipelineRun(ctx, name, factSHA1, "", "tester", map[string]string{"uiRevision": factSHA2})
		cancel()
		if err == nil || resp.OK || resp.Changed || !strings.Contains(resp.Message, "bifrost-ui") || strings.Contains(resp.Message, "bifrost-platform") || strings.Contains(resp.Message, "unverified") {
			t.Fatal(resp, err)
		}
		assertNoContractCreate(t, dyn)
		assertPinnedSync(t, m, true)
		if m.gets["bifrost-ui"] < 6 {
			t.Fatal("no second look", m.paths)
		}
	}
}

func TestPlatformNamesPollAfterSync(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, mode := range []string{"delayed branch", "delayed tag", "delayed commit"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				revision, ui := "release-next", "release-next"
				params := map[string]string{}
				if mode == "delayed commit" {
					revision, ui = factSHA1, factSHA2
					params["uiRevision"] = ui
				}
				s, dyn, m := pinnedService(t, name, platformRefs(revision, ui), []string{"bifrost-platform", "bifrost-ui"}, mode)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				resp, _, err := s.StartPipelineRun(ctx, name, revision, "", "tester", params)
				if err != nil || !resp.OK {
					t.Fatal(resp, err)
				}
				createdContractRun(t, dyn)
				assertPinnedSync(t, m, true)
				for repo := range m.refs {
					if m.afterSync[repo] < 3 {
						t.Fatal("name was only checked once", m.paths)
					}
				}
			})
		}
	}
}

func TestCIKnownMissingNeverSyncs(t *testing.T) {
	for _, name := range []string{"bifrost-ci-platform", "bifrost-ci-frontend", "bifrost-ci-python"} {
		repos := []string{"bifrost-platform"}
		if name == "bifrost-ci-frontend" {
			repos = []string{"bifrost-trade-frontend"}
		}
		if name == "bifrost-ci-python" {
			repos = pythonCIRepos
		}
		for _, repo := range repos {
			t.Run(name+"/"+repo, func(t *testing.T) {
				pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": name, "namespace": "cicd"}, "spec": map[string]any{}}}
				s, dyn, paths := contractService(t, pipe, "missing")
				extra := map[string]string{}
				if name == "bifrost-ci-python" {
					extra["repo"] = repo
				}
				assertContractRefused(t, s, dyn, name, factSHA1, extra, fmt.Sprintf("revision %q is missing in: %s — sync this repository's mirror (or, where Gitea is its primary, push it there) before starting CI", factSHA1, repo))
				for _, path := range *paths {
					if strings.Contains(path, "mirror-sync") || !strings.HasPrefix(path, "/api/v1/repos/bifrost/"+repo+"/") {
						t.Fatal(*paths)
					}
				}
			})
		}
	}
}

// This ratchet intentionally follows the infra checkout, rather than a frozen
// fixture. PROD clones source-gate results: follow that task's named inputs too.
func TestPlatformCloneRefParamsFollowInfra(t *testing.T) {
	pipes := infraPipelines(t)
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		pipe := pipes[name]
		if pipe == nil {
			t.Fatal("missing infra pipeline", name)
		}
		tasks, _, err := unstructured.NestedSlice(pipe.Object, "spec", "tasks")
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, raw := range tasks {
			task := raw.(map[string]any)
			taskName, _ := task["name"].(string)
			if !strings.HasPrefix(taskName, "clone-") {
				continue
			}
			params := task["params"].([]any)
			values := map[string]string{}
			for _, p := range params {
				m := p.(map[string]any)
				values[m["name"].(string)], _ = m["value"].(string)
			}
			repo := strings.TrimSuffix(values["url"][strings.LastIndex(values["url"], "/")+1:], ".git")
			key := pipelineRepoRefParams[name][repo]
			if key == "" {
				t.Fatalf("%s clone %s absent from table", name, repo)
			}
			expected := "$(params." + key + ")"
			if strings.HasPrefix(values["revision"], "$(tasks.check-source.results.") {
				resultKey := map[string]string{"commit": "revision", "ui-commit": "uiRevision"}
				result := strings.TrimSuffix(strings.TrimPrefix(values["revision"], "$(tasks.check-source.results."), ")")
				if resultKey[result] != key {
					t.Fatalf("%s/%s clone result %s does not select %s", name, repo, result, key)
				}
				found := false
				for _, sourceRaw := range tasks {
					source := sourceRaw.(map[string]any)
					if source["name"] != "check-source" {
						continue
					}
					for _, p := range source["params"].([]any) {
						m := p.(map[string]any)
						if m["name"] == key && m["value"] == expected {
							found = true
						}
					}
				}
				if !found {
					t.Fatalf("%s source gate does not take %s", name, expected)
				}
			} else if values["revision"] != expected {
				t.Fatalf("%s/%s clone revision=%s, want %s", name, repo, values["revision"], expected)
			}
			seen[repo] = true
		}
		assertJSONEqual(t, seen, map[string]bool{"bifrost-platform": true, "bifrost-ui": true})
	}
}

func TestPlatformPollingErrorRequiresSeenPresent(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, mode := range []string{"poll error missing", "poll error recover"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				s, dyn, m := pinnedService(t, name, platformRefs(factSHA1, factSHA2), []string{"bifrost-ui"}, mode)
				ctx, cancel := context.WithTimeout(context.Background(), 4500*time.Millisecond)
				defer cancel()
				resp, _, err := s.StartPipelineRun(ctx, name, factSHA1, "", "tester", map[string]string{"uiRevision": factSHA2})
				if mode == "poll error missing" {
					if err == nil || resp.OK || resp.Changed || !strings.Contains(resp.Message, "mirror was synced but the ref is still not present") || strings.Contains(resp.Message, "mirror sync did not complete") {
						t.Fatal(resp, err)
					}
					assertNoContractCreate(t, dyn)
				} else {
					if err != nil || !resp.OK {
						t.Fatal(resp, err)
					}
					createdContractRun(t, dyn)
				}
				if m.afterSync["bifrost-ui"] < 3 {
					t.Fatal("did not poll past error and missing", m.paths)
				}
				assertPinnedSync(t, m, true)
			})
		}
	}
}

func TestPlatformHexadecimalNames(t *testing.T) {
	for _, name := range []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"} {
		for _, ref := range []string{"20261012", "deadbeef", "26A10B5"} {
			t.Run(name+"/"+ref, func(t *testing.T) {
				s, dyn, m := pinnedService(t, name, platformRefs(ref, ref), nil, "")
				sentence := "use the full 40-character lowercase SHA"
				if ref == "20261012" {
					sentence = ""
				}
				assertPinnedCall(t, s, dyn, name, ref, nil, sentence)
				if ref == "20261012" && len(m.paths) != 2 {
					t.Fatal("digit name did not use branch lookup", m.paths)
				}
				if sentence != "" && len(m.paths) != 0 {
					t.Fatal("invalid SHA reached mirror", m.paths)
				}
			})
		}
	}
}
