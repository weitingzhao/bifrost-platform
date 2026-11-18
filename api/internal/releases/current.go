package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// ErrUnsettled: what the environment runs now cannot be told from the
	// records: a record of it is unreadable, or a delivery run newer than
	// its newest record exists (running, failed, or finished and not
	// recorded yet).
	ErrUnsettled = errors.New("releases: the environment's deployment is not settled in the records")
	// ErrNoDeployment: no deploying record of the lane and environment.
	ErrNoDeployment = errors.New("releases: no deployment recorded")
)

// Current is the newest deploying record of lane and env, for a reader that
// must not take an older deployment for the current one (the agent
// runtime's gates_passed, design v5 T33). Unlike List, which skips a record
// it cannot parse, it fails on one, or on one the labels chose that lacks
// its run, lane, environment, times or deploying mark, or names another
// lane or environment (review 162 A3). It also fails while another delivery
// run of the lane and environment may have changed the environment after
// that record: one not finished (whenever it started), or one that finished
// at or after the record's completion (review 162 A2). So the recorder's
// lag and a delivery in flight read as unsettled, never as the previous
// deployment. A delivery that failed or went unrecorded and was then pruned
// from Tekton is not seen (review 162 A1, the Owner's decision).
//
// The records come from the record table once it is wired (2026-11-18: 1,272
// ConfigMaps and rows compared field by field, the same runs and the same
// content but for recorded_at, which no reader of Current uses), and from the
// ConfigMaps before that. A deployment recorded as a ConfigMap whose row
// failed reads as unsettled, never as the deployment before it, until the
// recorder's next pass copies the row.
//
// A table with no row of lane and env is not where they are kept: only PROD's
// workers record, into PROD's database, so STG's table stays empty while the
// ConfigMaps in the shared cluster hold every environment's records
// (2026-11-18: ac7ece90 answered "no deployment" on STG).
func (s *Service) Current(ctx context.Context, lane, env string) (Record, error) {
	if r := s.tableReader(); r != nil {
		rec, err := s.currentFromTable(ctx, r, lane, env)
		if !errors.Is(err, ErrNoDeployment) {
			return rec, err
		}
	}
	return s.currentFromMaps(ctx, lane, env)
}

// currentFromTable is Current from the rows of lane and env.
func (s *Service) currentFromTable(ctx context.Context, r TableReader, lane, env string) (Record, error) {
	recs, err := r.Deployments(ctx, lane, env)
	if err != nil {
		return Record{}, err
	}
	for _, rec := range recs {
		if err := checkDeployment(rec, "the row of "+rec.Run, lane, env); err != nil {
			return Record{}, err
		}
	}
	return s.settledNewest(ctx, recs, lane, env)
}

// checkDeployment fails a record that does not read as a deployment of lane and env.
func checkDeployment(rec Record, name, lane, env string) error {
	if rec.Run == "" || rec.Lane != lane || rec.Env != env || !rec.Deploys || rec.StartedAt.IsZero() || rec.CompletedAt.IsZero() {
		return fmt.Errorf("%w: %s does not read as a deployment of %s/%s", ErrUnsettled, name, lane, env)
	}
	return nil
}

// currentFromMaps is Current from the record ConfigMaps of lane and env.
func (s *Service) currentFromMaps(ctx context.Context, lane, env string) (Record, error) {
	core, _, err := s.clients()
	if err != nil {
		return Record{}, err
	}
	ns := s.rules.Namespace
	cms, err := core.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelRecord + "," + labelLane + "=" + labelValue(lane) + "," + labelEnv + "=" + labelValue(env),
	})
	if err != nil {
		return Record{}, err
	}
	recs := make([]Record, 0, len(cms.Items))
	for _, cm := range cms.Items {
		var rec Record
		if perr := json.Unmarshal([]byte(cm.Data[dataKey]), &rec); perr != nil {
			return Record{}, fmt.Errorf("%w: %s is unreadable: %v", ErrUnsettled, cm.Name, perr)
		}
		if err := checkDeployment(rec, cm.Name, lane, env); err != nil {
			return Record{}, err
		}
		recs = append(recs, rec)
	}
	return s.settledNewest(ctx, recs, lane, env)
}

// settledNewest is Current's answer from lane/env's deploying records, wherever
// they were read: the newest, unless a delivery run of lane/env may have
// changed the environment after it.
func (s *Service) settledNewest(ctx context.Context, recs []Record, lane, env string) (Record, error) {
	var newest *Record
	for i := range recs {
		if newest == nil || recs[i].CompletedAt.After(newest.CompletedAt) {
			newest = &recs[i]
		}
	}
	if newest == nil {
		return Record{}, ErrNoDeployment
	}
	_, dyn, err := s.clients()
	if err != nil {
		return Record{}, err
	}
	ns := s.rules.Namespace
	runs, err := listRunsOf(ctx, dyn, ns, s.rules.deployingPipelines(lane, env))
	if err != nil {
		return Record{}, err
	}
	for i := range runs {
		run := &runs[i]
		rule, ok := s.rules.Match(run.GetName(), run.GetLabels()["tekton.dev/pipeline"])
		if !ok || !rule.Deploys || rule.Lane != lane || rule.Env != env || run.GetName() == newest.Run {
			continue
		}
		completed := nestedTime(run.Object, "status", "completionTime")
		if completed.IsZero() || !completed.Before(newest.CompletedAt) {
			return Record{}, fmt.Errorf("%w: run %s may have changed %s/%s after the record of %s", ErrUnsettled, run.GetName(), lane, env, newest.Run)
		}
	}
	return *newest, nil
}

// deployingPipelines are the pipelines whose runs can change lane/env by the
// rules, as a label selector on tekton.dev/pipeline; "" when a deploying rule
// of that lane and environment knows its runs by name prefix (an inline
// pipelineSpec's run carries its own name in that label), and then every run
// has to be looked at.
//
// 2026-11-17: Current looked at every run of the namespace, a hundred a page.
// The agent runtime asks it for the deployment every fifteen seconds, and
// with 2,058 runs kept that was about eighty LISTs a minute from each
// environment's workers, the largest unnamed share of the platform's own
// requests. Only the runs of these pipelines can make the answer unsettled.
func (r Rules) deployingPipelines(lane, env string) string {
	return r.pipelinesOf(func(rule Rule) bool { return rule.Deploys && rule.Lane == lane && rule.Env == env })
}

// recordedPipelines is that selector for every run the rules know: what Tick
// records. 2026-11-17, measured on PROD: Tick read every run of the namespace
// a hundred a page, 2,127 runs, and was the largest share of the platform's
// LISTs of runs left (147 in ten minutes). A run Tick records is finished, so
// Tekton has labelled it.
func (r Rules) recordedPipelines() string {
	return r.pipelinesOf(func(Rule) bool { return true })
}

func (r Rules) pipelinesOf(keep func(Rule) bool) string {
	var names []string
	for _, rule := range r.Rules {
		if !keep(rule) {
			continue
		}
		if rule.Match.Pipeline == "" {
			return ""
		}
		names = append(names, rule.Match.Pipeline)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "tekton.dev/pipeline in (" + strings.Join(names, ",") + ")"
}
