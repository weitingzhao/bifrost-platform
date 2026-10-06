package releases

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

var (
	pipelineRunGVR = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	taskRunGVR     = schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "taskruns"}
)

// Clients returns the kube clients per call, so a rotated kubeconfig is picked up.
type Clients func() (kubernetes.Interface, dynamic.Interface, error)

// TickResult is what one recorder pass did.
type TickResult struct {
	At       time.Time `json:"at"`
	Runs     int       `json:"runs"`
	Matched  int       `json:"matched"`
	Recorded int       `json:"recorded"`
	Errors   []string  `json:"errors"`
}

// Service records finished delivery runs as ConfigMaps and reads them back.
type Service struct {
	rules     Rules
	rulesPath string
	rulesErr  error
	clients   Clients
	now       func() time.Time

	mu   sync.Mutex
	last *TickResult
}

func NewService(configDir string, clients Clients) *Service {
	rules, path, err := LoadRules(configDir)
	if err != nil {
		slog.Warn("release rules not loaded", "path", path, "err", err)
	}
	if rules.Namespace == "" {
		rules.Namespace = "cicd"
	}
	return &Service{rules: rules, rulesPath: path, rulesErr: err, clients: clients,
		now: func() time.Time { return time.Now().UTC() }}
}

// Enabled: there are rules to record by.
func (s *Service) Enabled() bool { return len(s.rules.Rules) > 0 }

// RecorderWanted says whether this process should write records. The records
// live in the shared cluster, so by default only an in-cluster process (the
// workers pod) writes them; a laptop dev server reads them but does not, unless
// PLATFORM_RELEASE_RECORDER=on. PLATFORM_RELEASE_RECORDER=off turns it off anywhere.
func RecorderWanted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_RELEASE_RECORDER"))) {
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// Start runs Tick now and then every interval until ctx ends (workers role only:
// records are idempotent by name, but one writer keeps the pass cheap).
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if !s.Enabled() {
		slog.Info("release recorder off: no rules", "path", s.rulesPath)
		return
	}
	safego.Go("releases.recorder", func() {
		first := true
		tick := func() {
			r := s.Tick(ctx)
			// the first pass always logs, so a silent recorder is visibly alive
			if first || r.Recorded > 0 || len(r.Errors) > 0 {
				slog.Info("release recorder", "first", first, "runs", r.Runs, "matched", r.Matched, "recorded", r.Recorded,
					"errors", len(r.Errors), "rules", len(s.rules.Rules), "path", s.rulesPath)
				for _, e := range r.Errors {
					slog.Warn("release recorder error", "err", e)
				}
			}
			first = false
		}
		safego.Do("releases.recorder.tick", tick)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				safego.Do("releases.recorder.tick", tick)
			}
		}
	})
}

// Tick records every finished, successful, matching run that has no record yet.
func (s *Service) Tick(ctx context.Context) TickResult {
	res := TickResult{At: s.now(), Errors: []string{}}
	defer func() {
		s.mu.Lock()
		s.last = &res
		s.mu.Unlock()
	}()
	core, dyn, err := s.clients()
	if err != nil {
		res.Errors = append(res.Errors, "clients: "+err.Error())
		return res
	}
	ns := s.rules.Namespace

	have := map[string]*corev1.ConfigMap{}
	cms, err := core.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: labelRecord})
	if err != nil {
		res.Errors = append(res.Errors, "list records: "+err.Error())
		return res
	}
	for i := range cms.Items {
		have[cms.Items[i].Name] = &cms.Items[i]
	}

	runs, err := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		res.Errors = append(res.Errors, "list runs: "+err.Error())
		return res
	}
	res.Runs = len(runs.Items)
	for i := range runs.Items {
		run := &runs.Items[i]
		rule, ok := s.rules.Match(run.GetName(), run.GetLabels()["tekton.dev/pipeline"])
		if !ok {
			continue
		}
		res.Matched++
		prev := have[recordName(run.GetName())]
		if (prev != nil && prev.Labels[labelComplete] != "false") || !succeeded(run) {
			continue
		}
		trs, err := dyn.Resource(taskRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "tekton.dev/pipelineRun=" + run.GetName(),
		})
		if err != nil {
			res.Errors = append(res.Errors, run.GetName()+": taskruns: "+err.Error())
			continue
		}
		rec := buildRecord(run, rule, trs.Items, s.now())
		if prev != nil && !fewerMissing(prev, rec) {
			continue
		}
		if err := s.write(ctx, core, ns, rec, prev); err != nil {
			res.Errors = append(res.Errors, run.GetName()+": "+err.Error())
			continue
		}
		res.Recorded++
	}
	return res
}

// fewerMissing: the rebuilt record resolves at least one clone the stored one could not.
func fewerMissing(prev *corev1.ConfigMap, rec Record) bool {
	var old Record
	if err := json.Unmarshal([]byte(prev.Data[dataKey]), &old); err != nil {
		return true
	}
	return len(rec.Missing) < len(old.Missing)
}

// write creates the record, or replaces an incomplete one (prev) with a fuller rebuild.
func (s *Service) write(ctx context.Context, core kubernetes.Interface, ns string, rec Record, prev *corev1.ConfigMap) error {
	body, err := encode(rec)
	if err != nil {
		return err
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      recordName(rec.Run),
			Namespace: ns,
			Labels: map[string]string{
				labelRecord:                    recordVersion,
				labelLane:                      labelValue(rec.Lane),
				labelEnv:                       labelValue(rec.Env),
				labelRun:                       labelValue(rec.Run),
				labelComplete:                  fmt.Sprint(len(rec.Missing) == 0),
				"app.kubernetes.io/managed-by": "bifrost-platform",
			},
		},
		Data: map[string]string{dataKey: body},
	}
	if prev != nil {
		cm.ResourceVersion = prev.ResourceVersion
		_, err = core.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			return nil // another recorder updated it first
		}
		return err
	}
	_, err = core.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil // another recorder got there first
	}
	return err
}

// List returns every record, newest completion first.
func (s *Service) List(ctx context.Context) ([]Record, error) {
	core, _, err := s.clients()
	if err != nil {
		return nil, err
	}
	cms, err := core.CoreV1().ConfigMaps(s.rules.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labelRecord})
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(cms.Items))
	for _, cm := range cms.Items {
		var rec Record
		if err := json.Unmarshal([]byte(cm.Data[dataKey]), &rec); err != nil {
			slog.Warn("release record unreadable", "name", cm.Name, "err", err)
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CompletedAt.After(out[j].CompletedAt) })
	return out, nil
}

// Status is GET /api/v1/releases metadata.
type Status struct {
	RulesPath string      `json:"rules_path"`
	Rules     int         `json:"rules"`
	RulesErr  string      `json:"rules_error,omitempty"`
	Namespace string      `json:"namespace"`
	LastTick  *TickResult `json:"last_tick,omitempty"`
}

func (s *Service) Status() Status {
	st := Status{RulesPath: s.rulesPath, Rules: len(s.rules.Rules), Namespace: s.rules.Namespace}
	if s.rulesErr != nil {
		st.RulesErr = s.rulesErr.Error()
	}
	s.mu.Lock()
	st.LastTick = s.last
	s.mu.Unlock()
	return st
}
