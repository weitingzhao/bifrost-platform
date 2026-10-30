package delivery

import (
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CheckInputs compares independently completed clones with the frozen inputs,
// without requiring successful builds or deployment. ObserveRun/RestoreSTGRun
// must first validate the run and child identity chain. Missing clone evidence
// stays unknown, but cannot hide a mismatch in another validated clone.
func (o STGRunObservation) CheckInputs(expected STGContent) error {
	if o.Run == nil || (o.Status != "Succeeded" && o.Status != "Failed") {
		return ErrSTGEvidence
	}
	ref, _, _ := unstructured.NestedMap(o.Run.Object, "spec", "pipelineRef")
	_, inline, _ := unstructured.NestedFieldNoCopy(o.Run.Object, "spec", "pipelineSpec")
	if inline || !reflect.DeepEqual(ref, map[string]any{"name": STGReleasePipeline}) {
		return ErrSTGEvidence
	}
	var unknown error
	for _, check := range []struct{ task, repo, want string }{
		{"clone-platform", "bifrost-platform", expected.P},
		{"clone-ui", "bifrost-ui", expected.U},
		{"clone-infra", "bifrost-trade-infra", expected.Infra},
	} {
		commit, err := o.archivedResult(check.task, "bifrost-git-clone-gitea", "commit")
		if err != nil || !callerParamSHA.MatchString(commit) {
			unknown = ErrSTGEvidence
			continue
		}
		params, err := namedStrings(o.Children[check.task], "spec", "params")
		if err != nil || params["url"] != "http://gitea.cicd.svc.cluster.local:3000/bifrost/"+check.repo+".git" {
			unknown = ErrSTGEvidence
			continue
		}
		if commit != check.want {
			return ErrSTGWrongInput
		}
	}
	return unknown
}
