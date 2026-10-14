package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
// it cannot parse, it fails on one; and it fails while a delivery run of the
// lane and environment newer than that record exists, so the recorder's lag
// and a delivery in flight read as unsettled, never as the previous
// deployment.
func (s *Service) Current(ctx context.Context, lane, env string) (Record, error) {
	core, dyn, err := s.clients()
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
	var newest *Record
	for _, cm := range cms.Items {
		var rec Record
		if perr := json.Unmarshal([]byte(cm.Data[dataKey]), &rec); perr != nil {
			return Record{}, fmt.Errorf("%w: %s is unreadable: %v", ErrUnsettled, cm.Name, perr)
		}
		if rec.Lane != lane || rec.Env != env || !rec.Deploys {
			continue
		}
		if newest == nil || rec.CompletedAt.After(newest.CompletedAt) {
			r := rec
			newest = &r
		}
	}
	if newest == nil {
		return Record{}, ErrNoDeployment
	}
	runs, err := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return Record{}, err
	}
	for i := range runs.Items {
		run := &runs.Items[i]
		rule, ok := s.rules.Match(run.GetName(), run.GetLabels()["tekton.dev/pipeline"])
		if !ok || !rule.Deploys || rule.Lane != lane || rule.Env != env || run.GetName() == newest.Run {
			continue
		}
		started := nestedTime(run.Object, "status", "startTime")
		if started.IsZero() {
			started = run.GetCreationTimestamp().Time
		}
		if started.IsZero() || started.After(newest.StartedAt) {
			return Record{}, fmt.Errorf("%w: run %s is newer than the record of %s", ErrUnsettled, run.GetName(), newest.Run)
		}
	}
	return *newest, nil
}
