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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
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
	// Rows: records also written as rows of the record table (SetTable).
	Rows   int      `json:"rows"`
	Errors []string `json:"errors"`
	// Backfilled: ConfigMap records copied into the table by this pass.
	Backfilled int `json:"backfilled,omitempty"`
}

// Service records finished delivery runs as ConfigMaps and reads them back.
type Service struct {
	rules     Rules
	rulesPath string
	configDir string
	rulesErr  error
	clients   Clients
	now       func() time.Time

	// kick asks the recorder loop for a pass now (Kick); one waiting ask is
	// enough, so it holds one.
	kick chan struct{}

	mu     sync.Mutex
	last   *TickResult
	table  TableWriter
	reader TableReader
	// runReader reads runs through the bus run cache (SetRunReader).
	runReader RunReader
	// copiedMaps: this process has copied the ConfigMap records into the table.
	copiedMaps bool
}

func NewService(configDir string, clients Clients) *Service {
	rules, path, err := LoadRules(configDir)
	if err != nil {
		slog.Warn("release rules not loaded", "path", path, "err", err)
	}
	if rules.Namespace == "" {
		rules.Namespace = "cicd"
	}
	return &Service{rules: rules, rulesPath: path, configDir: configDir, rulesErr: err, clients: clients,
		now: func() time.Time { return time.Now().UTC() }, kick: make(chan struct{}, 1)}
}

// RuleOf is the release rule of a pipeline (a pipelineRef run's
// tekton.dev/pipeline label): its lane, environment and whether it deploys.
func (s *Service) RuleOf(pipeline string) (Rule, bool) { return s.rules.Match("", pipeline) }

// PipelineOf is the pipeline whose runs deploy lane to env, by the rule that
// names it; ok is false when no deploying rule with a pipeline does.
func (s *Service) PipelineOf(lane, env string) (string, bool) {
	for _, r := range s.rules.Rules {
		if r.Deploys && r.Lane == lane && r.Env == env && r.Match.Pipeline != "" {
			return r.Match.Pipeline, true
		}
	}
	return "", false
}

// Enabled: there are rules to record by.
func (s *Service) Enabled() bool { return len(s.rules.Rules) > 0 }

// RecorderWanted says whether this process should write records. The records
// live in the shared cluster, so by default only an in-cluster process (the
// workers pod) writes them; a laptop dev server reads them but does not, unless
// PLATFORM_RELEASE_RECORDER=on. PLATFORM_RELEASE_RECORDER=off turns it off
// anywhere, and =rows records quietly (RecorderQuiet).
func RecorderWanted() bool {
	switch recorderSetting() {
	case "on", "true", "1", recorderRows:
		return true
	case "off", "false", "0":
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

const recorderRows = "rows"

func recorderSetting() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_RELEASE_RECORDER")))
}

// RecorderQuiet: this platform keeps its own table of the records and announces
// nothing. STG does: each platform has its own database, the runs are shared,
// and the release events are PROD's to publish.
func RecorderQuiet() bool { return recorderSetting() == recorderRows }

// Kick asks the recorder for a pass now instead of at its next interval: the
// caller saw a run complete. It never blocks; asks made while one waits are
// one pass. Without a started recorder it does nothing.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Start runs Tick now, then every interval and at every Kick, until ctx ends
// (workers role only: records are idempotent by name, but one writer keeps the
// pass cheap).
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if !s.Enabled() {
		slog.Info("release recorder off: no rules", "path", s.rulesPath)
		return
	}
	safego.Go("releases.recorder", func() {
		first := true
		tick := func() {
			r := s.Tick(ctx)
			id := maintainer.PlatformID(maintainer.LoopReleaseRecorder)
			if len(r.Errors) > 0 {
				maintainer.Failure(id)
			} else {
				maintainer.Success(id)
			}
			// the first pass always logs, so a silent recorder is visibly alive
			if first || r.Recorded > 0 || len(r.Errors) > 0 || r.Backfilled > 0 {
				// rows and table: what reached the record table (2026-11-17: the
				// first PROD pass with the table logged recorded=1 errors=0, and
				// whether a row was written could not be told from the log).
				slog.Info("release recorder", "first", first, "runs", r.Runs, "matched", r.Matched, "recorded", r.Recorded,
					"rows", r.Rows, "table", s.tableWriter() != nil,
					"backfilled", r.Backfilled,
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
			case <-s.kick:
				safego.Do("releases.recorder.tick", tick)
			}
		}
	})
}

// Tick records every finished, successful, matching run that has no record yet:
// as rows when the service has the table, else as ConfigMaps.
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
	if r := s.tableReader(); r != nil {
		s.tickRows(ctx, r, core, dyn, &res)
		return res
	}
	// A platform with a ledger keeps its records there. The recorder's first pass
	// can come before the ledger's pool is open: it waits for the next one rather
	// than writing a ConfigMap (2026-11-18: each STG restart added one).
	if os.Getenv("AGENTRT_ENV") != "" {
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

	runs, err := s.readRuns(ctx, dyn, RouteTick, func(Rule) bool { return true })
	if err != nil {
		res.Errors = append(res.Errors, "list runs: "+err.Error())
		return res
	}
	res.Runs = len(runs)
	for i := range runs {
		run := &runs[i]
		rule, ok := s.rules.Match(run.GetName(), run.GetLabels()["tekton.dev/pipeline"])
		if !ok {
			continue
		}
		res.Matched++
		prev := have[recordName(run.GetName())]
		current := prev != nil && prev.Labels[labelRecord] == recordVersion
		if (current && prev.Labels[labelComplete] != "false") || !succeeded(run) {
			continue
		}
		// The list says which runs to record; the record is built from the
		// run itself (the bus run cache keeps only part of one).
		full, err := dyn.Resource(pipelineRunGVR).Namespace(ns).Get(ctx, run.GetName(), metav1.GetOptions{})
		if err != nil {
			res.Errors = append(res.Errors, run.GetName()+": run: "+err.Error())
			continue
		}
		trs, err := dyn.Resource(taskRunGVR).Namespace(ns).List(ctx, metav1.ListOptions{
			LabelSelector: "tekton.dev/pipelineRun=" + run.GetName(),
		})
		if err != nil {
			res.Errors = append(res.Errors, run.GetName()+": taskruns: "+err.Error())
			continue
		}
		rec := buildRecord(full, rule, trs.Items, s.now())
		if current && !fewerMissing(prev, rec) {
			continue
		}
		if err := s.write(ctx, core, ns, rec, prev); err != nil {
			res.Errors = append(res.Errors, run.GetName()+": "+err.Error())
			continue
		}
		res.Recorded++
		if wrote, err := s.writeRow(ctx, rec, true); err != nil {
			res.Errors = append(res.Errors, run.GetName()+": row: "+err.Error())
		} else if wrote {
			res.Rows++
		}
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
	if r := s.tableReader(); r != nil {
		rows, err := r.Records(ctx)
		if err != nil {
			return nil, err
		}
		// No row at all: a platform whose recorder has not filled its table.
		if len(rows) > 0 {
			sortNewest(rows)
			return rows, nil
		}
	}
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
	sortNewest(out)
	return out, nil
}

// Status is GET /api/v1/releases metadata.
type Status struct {
	RulesPath string      `json:"rules_path"`
	Rules     int         `json:"rules"`
	RulesErr  string      `json:"rules_error,omitempty"`
	Namespace string      `json:"namespace"`
	LastTick  *TickResult `json:"last_tick,omitempty"`
	// RuleList is release-rules.yaml's rules as loaded, in order: the
	// Releases page builds its release lines from them (LAW-8).
	RuleList []Rule `json:"rule_list"`
}

func (s *Service) Status() Status {
	st := Status{RulesPath: s.rulesPath, Rules: len(s.rules.Rules), Namespace: s.rules.Namespace,
		RuleList: append([]Rule{}, s.rules.Rules...)}
	if s.rulesErr != nil {
		st.RulesErr = s.rulesErr.Error()
	}
	s.mu.Lock()
	st.LastTick = s.last
	s.mu.Unlock()
	return st
}

// listRuns lists a namespace's PipelineRuns page by page, each without the
// copy of the Pipeline Tekton keeps in its status and without managedFields.
// Neither is read here (a record's pinned clones come from spec, its task
// results from the TaskRuns), and the copy is most of a run: measured
// 2026-11-15 on 1182 retained runs it is 77% of the listed bytes, and the
// workers that hold these lists were OOMKilled at 512Mi in STG.
func listRuns(ctx context.Context, dyn dynamic.Interface, ns string) ([]unstructured.Unstructured, error) {
	return listRunsOf(ctx, dyn, ns, "")
}

// listRunsOf is listRuns for the runs a label selector takes; "" is all.
func listRunsOf(ctx context.Context, dyn dynamic.Interface, ns, selector string) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	// A hundred a page for the whole namespace (the workers once held these
	// lists at 512Mi); the runs a selector takes are a few hundred at most,
	// and one page of them is one request where two were made every fifteen
	// seconds (agentrt.deployed, 80 LISTs in ten minutes, 2026-11-17).
	options := metav1.ListOptions{Limit: 100, LabelSelector: selector}
	if selector != "" {
		options.Limit = 500
	}
	seen := map[string]bool{}
	for {
		page, err := dyn.Resource(pipelineRunGVR).Namespace(ns).List(ctx, options)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			unstructured.RemoveNestedField(page.Items[i].Object, "status", "pipelineSpec")
			unstructured.RemoveNestedField(page.Items[i].Object, "metadata", "managedFields")
		}
		out = append(out, page.Items...)
		options.Continue = page.GetContinue()
		if options.Continue == "" {
			return out, nil
		}
		if seen[options.Continue] {
			return nil, fmt.Errorf("pipeline run pagination repeated a cursor")
		}
		seen[options.Continue] = true
	}
}

// sortNewest orders records newest first, and records completed at the same
// time by run name, so the pages of GET /api/v1/releases?before= neither skip
// nor repeat one.
func sortNewest(recs []Record) {
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].CompletedAt.Equal(recs[j].CompletedAt) {
			return recs[i].CompletedAt.After(recs[j].CompletedAt)
		}
		return recs[i].Run > recs[j].Run
	})
}
