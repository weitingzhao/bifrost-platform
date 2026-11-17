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

// Origin: 2026-11-17 04:00 CST, the same count on PROD an hour after Current
// was narrowed: the recorder's Tick was the largest share left, 147 LISTs of
// PipelineRuns in ten minutes, reading all 2,127 runs a hundred a page to
// find the few the rules know. It lists the runs of the rules' pipelines; a
// rule that knows its runs by a name prefix cannot be told by label, and then
// everything is listed, as before.
func TestRecordedPipelinesIsEveryRuleOrNothing(t *testing.T) {
	rule := func(pipeline, prefix string, deploys bool) Rule {
		var r Rule
		r.Match.Pipeline, r.Match.NamePrefix, r.Deploys = pipeline, prefix, deploys
		return r
	}
	byLabel := Rules{Rules: []Rule{rule("bifrost-deliver-stg", "", true), rule("bifrost-build-market-data", "", false)}}
	if got, want := byLabel.recordedPipelines(), "tekton.dev/pipeline in (bifrost-build-market-data,bifrost-deliver-stg)"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	byName := Rules{Rules: append(byLabel.Rules, rule("", "pinned-", true))}
	if got := byName.recordedPipelines(); got != "" {
		t.Errorf("a name-prefix rule was narrowed away: %q", got)
	}
	if got := (Rules{}).recordedPipelines(); got != "" {
		t.Errorf("no rules: %q", got)
	}
}
