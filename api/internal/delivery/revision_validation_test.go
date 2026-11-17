//bifrost:owner delivery

package delivery

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestValidateRevision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rev   string
		valid bool
	}{
		{"main", true},
		{"feature/release-ui-polish", true},
		{"v0.2.0", true},
		{"# pasted markdown", false},
		{"line1\nline2", false},
		{string(make([]byte, 257)), false},
	}
	for _, tc := range cases {
		err := validateRevision(tc.rev)
		if tc.valid && err != nil {
			t.Errorf("validateRevision(%q) = %v, want nil", tc.rev, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("validateRevision(%q) = nil, want error", tc.rev)
		}
	}
}

func TestIntersectRefSets(t *testing.T) {
	t.Parallel()
	got := intersectRefSets([]map[string]bool{
		{"main": true, "feature/x": true},
		{"main": true, "feature/y": true},
	})
	if len(got) != 1 || got[0] != "main" {
		t.Fatalf("intersectRefSets = %v, want [main]", got)
	}
}

func TestReposForPipeline(t *testing.T) {
	t.Parallel()
	platform := reposForPipeline("bifrost-deliver-platform")
	if len(platform) != 2 {
		t.Fatalf("platform repos = %v, want 2", platform)
	}
	trade := reposForPipeline("bifrost-deliver-stg")
	if len(trade) != 7 {
		t.Fatalf("trade repos = %v, want 7", trade)
	}
	prod := reposForPipeline("bifrost-deliver-prod")
	if len(prod) != 7 {
		t.Fatalf("deliver-prod repos = %v, want 7", prod)
	}
	unknown := reposForPipeline("does-not-exist")
	if len(unknown) != len(trackedGiteaRepos) {
		t.Fatalf("unknown pipeline should fall back to all tracked repos")
	}
}

// 2026-11-17, OWNER_LAW #30: the supply chain's tracked repos and the
// Releases page's line repositories were hand-kept lists that still named the
// retired bifrost-trade-socket; they are now read from each Pipeline's clone
// tasks. The fixture is cut from infra k8s/cicd/tekton: deliver-platform's
// three clones at three revision parameters, deliver-stg's several clones at
// one, and build-frontend-stg's URL held in a pipeline parameter default.
func TestPipelineClones(t *testing.T) {
	t.Parallel()
	const platform = `{"spec":{"params":[{"name":"revision"},{"name":"giteaBase","default":"http://gitea/bifrost"}],
	  "tasks":[
	    {"name":"mirror-sync","params":[{"name":"mirrorRepos","value":"bifrost-platform bifrost-ui"}]},
	    {"name":"clone-platform","params":[{"name":"url","value":"$(params.giteaBase)/bifrost-platform.git"},{"name":"revision","value":"$(params.revision)"}]},
	    {"name":"clone-ui","params":[{"name":"url","value":"$(params.giteaBase)/bifrost-ui.git"},{"name":"revision","value":"$(params.uiRevision)"}]},
	    {"name":"build","taskSpec":{"volumes":[{"name":"d","configMap":{"name":"bifrost-api-stg-dockerfile"}}]}}],
	  "finally":[{"name":"clone-infra","params":[{"name":"url","value":"$(params.giteaBase)/bifrost-trade-infra.git"},{"name":"revision","value":"$(params.infraRevision)"}]}]}}`
	const trade = `{"spec":{"tasks":[
	    {"name":"clone-core","params":[{"name":"url","value":"$(params.giteaBase)/bifrost-trade-core.git"},{"name":"revision","value":"$(params.revision)"}]},
	    {"name":"clone-api","params":[{"name":"url","value":"$(params.giteaBase)/bifrost-trade-api.git"},{"name":"revision","value":"$(params.revision)"}]}]}}`
	const viaDefault = `{"spec":{"params":[{"name":"frontendRepo","default":"http://gitea/bifrost/bifrost-trade-frontend.git"}],
	  "tasks":[{"name":"clone","params":[{"name":"url","value":"$(params.frontendRepo)"},{"name":"revision","value":"$(params.revision)"}]}]}}`
	cases := []struct {
		name, spec, rev string
		repos           []string
	}{
		{"platform", platform, "bifrost-platform", []string{"bifrost-platform", "bifrost-trade-infra", "bifrost-ui"}},
		{"trade", trade, "", []string{"bifrost-trade-api", "bifrost-trade-core"}},
		{"default", viaDefault, "bifrost-trade-frontend", []string{"bifrost-trade-frontend"}},
		{"empty", `{}`, "", []string{}},
	}
	for _, tc := range cases {
		var obj map[string]any
		if err := json.Unmarshal([]byte(tc.spec), &obj); err != nil {
			t.Fatal(err)
		}
		repos, rev := pipelineClones(obj)
		if !reflect.DeepEqual(repos, tc.repos) || rev != tc.rev {
			t.Errorf("%s: pipelineClones = %v %q, want %v %q", tc.name, repos, rev, tc.repos, tc.rev)
		}
		if tc.name == "platform" {
			cms := map[string]bool{}
			dockerfileConfigMapRefs(obj, cms)
			if got := sortedSet(cms); !reflect.DeepEqual(got, []string{"bifrost-api-stg-dockerfile"}) {
				t.Errorf("dockerfileConfigMapRefs = %v", got)
			}
		}
	}
}
