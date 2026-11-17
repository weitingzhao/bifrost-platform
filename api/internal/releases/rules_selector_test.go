package releases

import "testing"

// Origin: 2026-11-17 01:45 CST, the platform's count of its own Kubernetes
// requests: about eighty unnamed LISTs of PipelineRuns a minute from each
// environment's workers. Current looked at every run of the namespace (2,058
// kept, a hundred a page) each time the agent runtime asked what an
// environment runs, every fifteen seconds. Only the runs of the pipelines
// that deploy to that lane and environment can unsettle the answer, so only
// those are listed; a lane one of whose deploying rules knows its runs by a
// name prefix cannot be narrowed by label and lists everything, as before.
func TestDeployingPipelinesNarrowsOnlyWhatALabelCanTell(t *testing.T) {
	rule := func(pipeline, prefix, lane, env string, deploys bool) Rule {
		var r Rule
		r.Match.Pipeline, r.Match.NamePrefix, r.Lane, r.Env, r.Deploys = pipeline, prefix, lane, env, deploys
		return r
	}
	rules := Rules{Rules: []Rule{
		rule("", "bifrost-deliver-prod-pinned-", "trade", "prod", true),
		rule("bifrost-deliver-prod", "", "trade", "prod", true),
		rule("bifrost-deliver-stg", "", "trade", "stg", true),
		rule("bifrost-deliver-platform", "", "platform", "stg", true),
		rule("bifrost-build-platform-extra", "", "platform", "stg", true),
		rule("bifrost-deliver-platform-prod", "", "platform", "prod", true),
		rule("bifrost-build-market-data", "", "plugin", "prod", false),
	}}
	for _, c := range []struct{ lane, env, want string }{
		{"platform", "prod", "tekton.dev/pipeline in (bifrost-deliver-platform-prod)"},
		{"platform", "stg", "tekton.dev/pipeline in (bifrost-build-platform-extra,bifrost-deliver-platform)"},
		{"trade", "stg", "tekton.dev/pipeline in (bifrost-deliver-stg)"},
		{"trade", "prod", ""},  // one of its rules is a name prefix: every run is looked at
		{"plugin", "prod", ""}, // nothing deploys there by the rules: no narrowing is claimed
		{"nowhere", "stg", ""},
	} {
		if got := rules.deployingPipelines(c.lane, c.env); got != c.want {
			t.Errorf("%s/%s: %q, want %q", c.lane, c.env, got, c.want)
		}
	}
}
