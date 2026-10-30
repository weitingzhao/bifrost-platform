package delivery

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kt "k8s.io/client-go/testing"
)

func TestSTGExternalListAllPagesAndExactPipeline(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "wrong-pipeline", "running", "unknown", "wrong-namespace", "missing-uid", "repeated-cursor", "read-error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			run, _ := stgRunWorld(t)
			switch mode {
			case "wrong-pipeline":
				stgSet(t, run, "bifrost-deliver-platform-prod", "spec", "pipelineRef", "name")
			case "running":
				stgSet(t, run, []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}, "status", "conditions")
			case "unknown":
				stgSet(t, run, []any{}, "status", "conditions")
			case "wrong-namespace":
				run.SetNamespace("other")
			case "missing-uid":
				run.SetUID(types.UID(""))
			}
			a, dyn, _ := stgAdapter(t, run)
			calls := 0
			dyn.PrependReactor("list", "pipelineruns", func(kt.Action) (bool, runtime.Object, error) {
				calls++
				if mode == "read-error" {
					return true, nil, fmt.Errorf("list unavailable")
				}
				page := &unstructured.UnstructuredList{}
				if calls == 1 || mode == "repeated-cursor" {
					page.SetContinue("page2")
				} else {
					page.Items = []unstructured.Unstructured{*run}
				}
				return true, page, nil
			})
			got, err := a.ListSucceededSTG(context.Background())
			if mode == "repeated-cursor" || mode == "read-error" {
				if err == nil {
					t.Fatal("incomplete list accepted")
				}
				return
			}
			if err != nil || calls != 2 || (len(got) == 1) != (mode == "success") {
				t.Fatal("pagination/ref/status", got, err, calls)
			}
		})
	}
}

func TestSTGExternalContentOwnRunProof(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"success", "missing-sync", "failed-sync", "other-deliver", "wrong-repo", "duplicate-clone", "wrong-owner", "wrong-uid"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			run, children := stgRunWorld(t)
			switch mode {
			case "missing-sync":
				delete(children, "gitops-sync")
			case "failed-sync":
				stgSet(t, children["gitops-sync"], []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
			case "other-deliver":
				params, _, _ := unstructured.NestedSlice(children["gitops-sync"].Object, "spec", "params")
				for _, v := range params {
					m := v.(map[string]any)
					if m["name"] == "deliverRun" {
						m["value"] = "other"
					}
				}
				stgSet(t, children["gitops-sync"], params, "spec", "params")
			case "wrong-repo":
				stgSet(t, children["clone-platform"], []any{map[string]any{"name": "url", "value": "https://wrong/repo.git"}}, "spec", "params")
			case "duplicate-clone":
				results, _, _ := unstructured.NestedSlice(children["clone-platform"].Object, "status", "results")
				results = append(results, results[0])
				stgSet(t, children["clone-platform"], results, "status", "results")
			case "wrong-owner":
				owners := children["clone-platform"].GetOwnerReferences()
				owners[0].UID = "other"
				children["clone-platform"].SetOwnerReferences(owners)
			case "wrong-uid":
				run.SetUID("other")
			}
			obs, err := readSTGWorld(t, run, children)
			if err == nil {
				var c STGContent
				c, err = obs.ExternalContent()
				if err == nil && (c.P != stgInput().P || c.APIDigest != stgAPI) {
					t.Fatal("wrong content", c)
				}
			}
			if (err == nil) != (mode == "success") {
				t.Fatal("external evidence acceptance", mode, err)
			}
		})
	}
}
