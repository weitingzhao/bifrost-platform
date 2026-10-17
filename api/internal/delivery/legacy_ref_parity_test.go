package delivery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Freeze the repository sets, request order and refusal from main 26a10b5.
// Enumerate all names known to the action, plus fallback, independently of the
// ref table. Adding another table row must fail even if its values happen to match.
func TestNonPlatformRefChecksMatchMain(t *testing.T) {
	assertJSONEqual(t, sortedKeysStringMap(pipelineRepoRefParams), []string{"bifrost-deliver-platform", "bifrost-deliver-platform-prod"})
	names := map[string]bool{"unknown": true, "bifrost-smoke": true, "bifrost-clone-frontend-smoke": true}
	for name := range baselinePipelines(t) {
		names[name] = true
	}
	for name := range kanikoPipelineNames {
		names[name] = true
	}
	for name := range pipelineMirrorRepos {
		names[name] = true
	}
	names["bifrost-ci-platform"] = true
	names["bifrost-ci-frontend"] = true
	delete(names, "bifrost-ci-python")
	for _, repo := range pythonCIRepos {
		names["bifrost-ci-python/"+repo] = true
	}
	for call := range names {
		name, ciRepo, _ := strings.Cut(call, "/")
		if isPlatformDelivery(name) {
			continue
		}
		repos := []string{"bifrost-trade-core", "bifrost-trade-worker", "bifrost-trade-socket", "bifrost-trade-api", "bifrost-trade-frontend", "bifrost-trade-infra", "bifrost-ui", "bifrost-platform"}
		if name == "bifrost-deliver-stg" || name == "bifrost-deliver-prod" {
			repos = repos[:7]
		}
		if isCIPipeline(name) {
			switch name {
			case "bifrost-ci-platform":
				ciRepo = "bifrost-platform"
			case "bifrost-ci-frontend":
				ciRepo = "bifrost-trade-frontend"
			}
			repos = []string{ciRepo}
		}
		for _, mode := range []string{"branch", "tag", "commit", "missing", "unknown", "partial", "socket unknown", "commit error", "SHA branch"} {
			t.Run(call+"/"+mode, func(t *testing.T) {
				revision := "main"
				if isCIPipeline(name) || requiresFullSHA(name) || mode == "commit" || mode == "commit error" || mode == "SHA branch" {
					revision = factSHA1
				}
				refs := map[string]string{}
				for _, repo := range repos {
					refs[repo] = revision
				}
				s, dyn, _ := pinnedService(t, name, refs, nil, "")
				paths := []string{}
				s.httpClient = &http.Client{Transport: contractTransport(func(r *http.Request) (*http.Response, error) {
					paths = append(paths, r.Method+" "+r.URL.Path)
					code, body := http.StatusOK, "[]"
					switch {
					case mode == "unknown" || (mode == "socket unknown" && strings.Contains(r.URL.Path, "/bifrost-trade-socket/")):
						code = http.StatusInternalServerError
					case mode == "partial" && strings.HasSuffix(r.URL.Path, "/tags"):
						code = http.StatusInternalServerError
					case strings.HasSuffix(r.URL.Path, "/branches") && (mode == "branch" || mode == "SHA branch"):
						body = fmt.Sprintf(`[{"name":%q,"commit":{"id":%q}}]`, revision, factSHA2)
					case strings.HasSuffix(r.URL.Path, "/tags") && mode == "tag":
						body = fmt.Sprintf(`[{"name":%q,"commit":{"sha":%q}}]`, revision, factSHA2)
					case strings.Contains(r.URL.Path, "/git/commits/"):
						code = http.StatusNotFound
						if mode == "commit" {
							code, body = http.StatusOK, `{"sha":"`+revision+`"}`
						}
						if mode == "commit error" {
							code = http.StatusInternalServerError
						}
					}
					return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
				})}
				// main probes the top-level ref before merging params.revision. Include
				// every declared per-repo pin to detect accidentally consuming any of them.
				extra := map[string]string{}
				pipe := baselinePipelines(t)[name]
				if pipe != nil && !isCIPipeline(name) {
					for key := range declaredPipelineParams(pipe) {
						if callerMayOverride(key) {
							extra[key] = factSHA2
						}
					}
				}
				if name == "bifrost-ci-python" {
					extra["repo"] = ciRepo
				}
				resp, _, err := s.StartPipelineRun(context.Background(), name, revision, "", "tester", extra)
				expectedPaths := []string{}
				for _, repo := range repos {
					base := "GET /api/v1/repos/bifrost/" + repo
					expectedPaths = append(expectedPaths, base+"/branches")
					if mode == "branch" || mode == "SHA branch" {
						continue
					}
					expectedPaths = append(expectedPaths, base+"/tags")
					if mode != "tag" && callerParamSHA.MatchString(revision) {
						expectedPaths = append(expectedPaths, base+"/git/commits/"+revision)
					}
				}
				assertJSONEqual(t, paths, expectedPaths)
				if mode == "missing" || mode == "commit error" || (isCIPipeline(name) && mode == "socket unknown") {
					missing := append([]string{}, repos...)
					sort.Strings(missing)
					want := fmt.Sprintf("revision %q is missing in: %s — create the same branch/tag in those repos (or deploy a ref present in all)", revision, strings.Join(missing, ", "))
					if isCIPipeline(name) {
						want = fmt.Sprintf("revision %q is missing in: %s — sync this repository's mirror (or, where Gitea is its primary, push it there) before starting CI", revision, ciRepo)
					}
					if err == nil || resp.OK || resp.Changed || resp.Message != want {
						t.Fatal(resp, err, "want", want)
					}
					assertNoContractCreate(t, dyn)
				} else if name == "bifrost-clone-frontend-smoke" {
					if err == nil || resp.OK || !strings.Contains(resp.Message, `workspace "source"`) {
						t.Fatal(resp, err)
					}
					assertNoContractCreate(t, dyn)
				} else {
					if err != nil || !resp.OK {
						t.Fatal(resp, err)
					}
					createdContractRun(t, dyn)
				}
			})
		}
	}
}

func sortedKeysStringMap(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestLegacyMissingPrecedesPipelineRead(t *testing.T) {
	pipe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tekton.dev/v1", "kind": "Pipeline", "metadata": map[string]any{"name": "unknown", "namespace": "cicd"}, "spec": "invalid"}}
	s, dyn, _ := contractService(t, pipe, "missing")
	assertContractRefused(t, s, dyn, "unknown", "main", nil, `revision "main" is missing in:`)
	for _, action := range dyn.Actions() {
		if action.GetResource().Resource == "pipelines" {
			t.Fatal("legacy gate moved after Pipeline read", action)
		}
	}
}
