package delivery

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// GET /api/v1/delivery/context?repo=<repo>[&commit=<sha>]: one read of the
// live release situation of one repository (what STG and PROD run, main's
// head, runs in flight, pending approvals, the release window, the Owner's
// last decision, the known regressions on main not fixed yet, and the lessons
// not yet made machine checks), so whoever is
// about to release does not ask another agent. It writes nothing.
//
// Every fact is read on its own, under its own timeout, and carries when it
// was read and from where. A fact that cannot be read now is null with the
// reason; it is never replaced by an older value, and it never fails the
// other facts or the response.

// releaseContextSchema bumps when the response changes shape.
const releaseContextSchema = 2

// releaseContextTimeout bounds each source, so a slow one cannot hang the route.
const releaseContextTimeout = 5 * time.Second

// releaseContextMaxPages bounds the run listing (100 runs a page).
const releaseContextMaxPages = 100

var lowerFullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Fact is one reading: Value is null exactly when Error says why.
type Fact[T any] struct {
	Value  *T     `json:"value"`
	ReadAt string `json:"read_at"`
	Source string `json:"source"`
	Error  string `json:"error,omitempty"`
}

// Deployed is what an environment runs, from the release records.
type Deployed struct {
	Commit      string `json:"commit"`
	Run         string `json:"run"`
	CompletedAt string `json:"completed_at"`
}

// MainRef is the repository's main head on the platform's Gitea mirror.
type MainRef struct {
	Commit string `json:"commit"`
}

// RunInFlight is a PipelineRun of the repository that has not finished.
type RunInFlight struct {
	Name      string `json:"name"`
	Pipeline  string `json:"pipeline"`
	StartedAt string `json:"started_at"`
	Revision  string `json:"revision,omitempty"`
	Delivery  bool   `json:"delivery"`
}

// PendingApproval is an approval still waiting for a decision that concerns the repository.
type PendingApproval struct {
	ID        string `json:"id"`
	Number    int    `json:"number,omitempty"`
	Action    string `json:"action"`
	CreatedAt string `json:"created_at"`
	Requester string `json:"requester"`
	Summary   string `json:"summary"`
}

// WindowView is the release window as stored. Held is whether it is live now;
// who/what/reason/expires_at are the stored record's even when it has ended.
type WindowView struct {
	Held      bool         `json:"held"`
	Who       string       `json:"who"`
	What      string       `json:"what"`
	Reason    string       `json:"reason"`
	ExpiresAt string       `json:"expires_at"`
	EndedAt   string       `json:"ended_at,omitempty"`
	Queue     []WindowWait `json:"queue"`
}

// WindowWait is one caller queued for the window.
type WindowWait struct {
	Who   string `json:"who"`
	What  string `json:"what"`
	Since string `json:"since"`
	Until string `json:"until"`
}

// OwnerAction is the latest time the Owner decided an approval or answered a
// question. It says nothing about whether the Owner is around.
type OwnerAction struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

// Lesson is a release lesson that has not become a machine check yet.
type Lesson struct {
	ID     string   `json:"id"`
	Repos  []string `json:"repos"`
	Cards  []string `json:"cards"`
	What   string   `json:"what"`
	Check  string   `json:"check"`
	Source string   `json:"source"`
}

// KnownRegression is a regression a line recorded on the repository's main
// with no fix recorded yet (bus/regressions). Information: no verdict and no
// gate reads it (2026-11-17, the coordinator).
type KnownRegression struct {
	Introduced string `json:"introduced"`
	Impact     string `json:"impact"`
	By         string `json:"by"`
	KnownAt    string `json:"known_at"`
}

// ContextFacts are the facts of a release context.
type ContextFacts struct {
	STG              Fact[Deployed]          `json:"stg"`
	PROD             Fact[Deployed]          `json:"prod"`
	Main             Fact[MainRef]           `json:"main"`
	MainAheadOfSTG   Fact[bool]              `json:"main_ahead_of_stg"`
	RunsInFlight     Fact[[]RunInFlight]     `json:"runs_in_flight"`
	PendingApprovals Fact[[]PendingApproval] `json:"pending_approvals"`
	Window           Fact[WindowView]        `json:"window"`
	OwnerLastAction  Fact[OwnerAction]       `json:"owner_last_action"`
	OpenRegressions  Fact[[]KnownRegression] `json:"open_regressions"`
	Lessons          Fact[[]Lesson]          `json:"lessons"`
}

// Verdict is true, false, or null when it cannot be told; Why says which and why.
type Verdict struct {
	Value *bool  `json:"value"`
	Why   string `json:"why"`
}

// ContextVerdicts are judgements about the commit asked for.
type ContextVerdicts struct {
	CommitIsSTG       Verdict `json:"commit_is_stg"`
	MainAheadOfCommit Verdict `json:"main_ahead_of_commit"`
	OthersInFlight    Verdict `json:"others_in_flight"`
}

// ReleaseContext is the body of GET /api/v1/delivery/context.
type ReleaseContext struct {
	Schema   int              `json:"schema"`
	Repo     string           `json:"repo"`
	Commit   string           `json:"commit,omitempty"`
	AsOf     string           `json:"as_of"`
	Facts    ContextFacts     `json:"facts"`
	Verdicts *ContextVerdicts `json:"verdicts,omitempty"`
}

// RunsPage lists one page of PipelineRuns: next is "" on the last page.
type RunsPage func(ctx context.Context, continueToken string) (items []unstructured.Unstructured, next string, err error)

// ReleaseContextSources are the readers a release context is assembled from.
// A nil reader reads as an error on its fact. HandleReleaseContext fills
// MainHead, Runs and Window from the delivery service when they are nil.
type ReleaseContextSources struct {
	// Current is the release records' newest deployment of a lane and env
	// (releases.Service.Current): ErrUnsettled and ErrNoDeployment are errors.
	Current func(ctx context.Context, lane, env string) (releases.Record, error)
	// Rule is the release rule of a pipeline (release-rules.yaml).
	Rule func(pipeline string) (releases.Rule, bool)
	// MainHead is main's commit on the Gitea mirror.
	MainHead func(ctx context.Context, repo string) (string, error)
	// Runs lists PipelineRuns page by page.
	Runs RunsPage
	// Window reads the stored release window record; found is false when
	// there is none.
	Window func(ctx context.Context) (rec windowRecord, found bool, err error)
	// Waiting is the approvals pending and not expired, read without a sweep.
	Waiting func() ([]approvals.Approval, error)
	// ApprovalHistory is every retained approval, read without a sweep.
	ApprovalHistory func() ([]approvals.Approval, error)
	// Questions is every retained question, read without a sweep.
	Questions func() ([]questions.Question, error)
	// OpenRegressions is the repository's known regressions on main with no
	// fix recorded, newest first.
	OpenRegressions func(ctx context.Context, repo string) ([]KnownRegression, error)
	// Now and Timeout default to time.Now and releaseContextTimeout.
	Now     func() time.Time
	Timeout time.Duration
}

// knownRepos are the repositories the release window can name.
func knownRepos() map[string]bool {
	out := map[string]bool{}
	for _, p := range windowPipelines {
		for r := range p.repos {
			out[r] = true
		}
	}
	return out
}

// HandleReleaseContext serves GET /api/v1/delivery/context with src; the
// readers it leaves nil are the delivery service's own.
func (h *Handler) HandleReleaseContext(src ReleaseContextSources) http.HandlerFunc {
	if src.MainHead == nil {
		src.MainHead = h.svc.PolicyFacts().MainHead
	}
	if src.Runs == nil {
		src.Runs = h.svc.cachedRunPage
	}
	if src.Window == nil {
		src.Window = h.svc.storedReleaseWindow
	}
	return func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSpace(r.URL.Query().Get("repo"))
		commit := strings.TrimSpace(r.URL.Query().Get("commit"))
		if !knownRepos()[repo] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo must be one of " + sortedKeys(knownRepos())})
			return
		}
		if commit != "" && !lowerFullSHA.MatchString(commit) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit must be a 40-character lowercase hex sha"})
			return
		}
		writeJSON(w, http.StatusOK, AssembleReleaseContext(r.Context(), src, repo, commit))
	}
}

// pipelineRunPage lists one page of the pipelines namespace's runs, without
// the copy of the Pipeline in their status and without managedFields
// (PipelineRunObservations has the measurement).
func (s *Service) pipelineRunPage(ctx context.Context, cont string) ([]unstructured.Unstructured, string, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return nil, "", err
	}
	page, err := dyn.Resource(pipelineRunGVR).Namespace(s.PipelinesNamespace()).List(ctx, metav1.ListOptions{Limit: 100, Continue: cont})
	if err != nil {
		return nil, "", err
	}
	for i := range page.Items {
		slimObservation(&page.Items[i])
	}
	return page.Items, page.GetContinue(), nil
}

// cachedRunPage is pipelineRunPage through the bus run cache (run_cache.go):
// while the cache is synced the first page is every run, from the cache once
// the route is trusted, else from a full list compared with it; without the
// cache it is pipelineRunPage.
func (s *Service) cachedRunPage(ctx context.Context, cont string) ([]unstructured.Unstructured, string, error) {
	if cont != "" || s.syncedCache() == nil {
		return s.pipelineRunPage(ctx, cont)
	}
	items, err := readThrough(s, routeReleaseContext,
		func(c RunCache) []unstructured.Unstructured { return c.Objects("") },
		func() ([]unstructured.Unstructured, error) { return s.allRunPages(ctx) },
		objectKeys)
	return items, "", err
}

// allRunPages is every page of pipelineRunPage, bounded as runsInFlight is.
func (s *Service) allRunPages(ctx context.Context) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	seen := map[string]bool{}
	cont := ""
	for page := 0; ; page++ {
		if page == releaseContextMaxPages {
			return nil, fmt.Errorf("more than %d pages of runs; not all were read", releaseContextMaxPages)
		}
		items, next, err := s.pipelineRunPage(ctx, cont)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if next == "" {
			return out, nil
		}
		if seen[next] {
			return nil, errors.New("pipeline run pagination repeated a cursor")
		}
		seen[next] = true
		cont = next
	}
}

// storedReleaseWindow reads the window record as stored. Unlike
// getReleaseWindow it records no expiry and keeps an ended record's holder
// and queue: it is a read for a report, not a gate.
func (s *Service) storedReleaseWindow(ctx context.Context) (windowRecord, bool, error) {
	cs, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return windowRecord{}, false, err
	}
	cm, err := cs.CoreV1().ConfigMaps(s.PipelinesNamespace()).Get(ctx, releaseWindowConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return windowRecord{}, false, nil
	}
	if err != nil {
		return windowRecord{}, false, err
	}
	rec, ok := parseWindowCM(cm)
	if !ok {
		return windowRecord{}, false, errWindowUnreadable
	}
	return rec, true, nil
}

// AssembleReleaseContext reads every fact of repo at once, each on its own,
// and judges commit when it is not "".
func AssembleReleaseContext(ctx context.Context, src ReleaseContextSources, repo, commit string) ReleaseContext {
	now := src.Now
	if now == nil {
		now = time.Now
	}
	timeout := src.Timeout
	if timeout <= 0 {
		timeout = releaseContextTimeout
	}
	out := ReleaseContext{Schema: releaseContextSchema, Repo: repo, Commit: commit}
	f := &out.Facts
	var wg sync.WaitGroup
	start := func(name string, fn func()) {
		wg.Add(1)
		safego.Go("release-context-"+name, func() {
			defer wg.Done()
			fn()
		})
	}
	start("stg", func() { f.STG = readDeployed(ctx, src, now, timeout, repo, "stg") })
	start("prod", func() { f.PROD = readDeployed(ctx, src, now, timeout, repo, "prod") })
	start("main", func() {
		f.Main = readFact(ctx, now, timeout, "main branch of "+repo+" on the platform's Gitea mirror", func(ctx context.Context) (MainRef, error) {
			if src.MainHead == nil {
				return MainRef{}, errors.New("this platform has no Gitea reader wired")
			}
			sha, err := src.MainHead(ctx, repo)
			return MainRef{Commit: sha}, err
		})
	})
	start("runs", func() {
		f.RunsInFlight = readFact(ctx, now, timeout, "Tekton PipelineRuns not finished whose repo, repo-url or pipeline names "+repo+" (listed 100 a page, or from the platform's watch of the runs once it matched the list; see bifrost_bus_readers_*)", func(ctx context.Context) ([]RunInFlight, error) {
			return runsInFlight(ctx, src, repo)
		})
	})
	start("approvals", func() {
		f.PendingApprovals = readFact(ctx, now, timeout, "approval records still pending (not expired) whose pipeline or params name "+repo, func(ctx context.Context) ([]PendingApproval, error) {
			if src.Waiting == nil {
				return nil, errors.New("this platform has no approval store wired")
			}
			list, err := src.Waiting()
			if err != nil {
				return nil, err
			}
			return pendingFor(list, repo), nil
		})
	})
	start("window", func() {
		f.Window = readFact(ctx, now, timeout, "release window ConfigMap "+releaseWindowConfigMap+" as stored", func(ctx context.Context) (WindowView, error) {
			if src.Window == nil {
				return WindowView{}, errors.New("this platform has no window reader wired")
			}
			rec, found, err := src.Window(ctx)
			if err != nil {
				return WindowView{}, err
			}
			return windowView(rec, found, now().UTC()), nil
		})
	})
	start("owner", func() {
		f.OwnerLastAction = readFact(ctx, now, timeout, "approval records decided by the Owner (approve or reject) and questions the Owner answered (not by a rule)", func(ctx context.Context) (OwnerAction, error) {
			return ownerLastAction(src)
		})
	})
	start("regressions", func() {
		f.OpenRegressions = readFact(ctx, now, timeout, "the ledger's main.regression.known events of "+repo+" with no main.regression.fixed after them", func(ctx context.Context) ([]KnownRegression, error) {
			if src.OpenRegressions == nil {
				return nil, errors.New("this platform has no regression reader wired")
			}
			return src.OpenRegressions(ctx, repo)
		})
	})
	start("lessons", func() {
		f.Lessons = readFact(ctx, now, timeout, "release_lessons.json embedded in this build (lessons not yet machine checks)", func(context.Context) ([]Lesson, error) {
			all, err := releaseLessons()
			if err != nil {
				return nil, err
			}
			return lessonsFor(all, repo), nil
		})
	})
	wg.Wait()
	asOf := now().UTC().Format(time.RFC3339)
	out.AsOf = asOf
	f.MainAheadOfSTG = mainAheadOfSTG(f.Main, f.STG, asOf)
	if commit != "" {
		out.Verdicts = verdicts(f, commit)
	}
	return out
}

// readFact runs read under its own timeout. A read that errs or does not
// answer in time is null with the reason; nothing older stands in for it.
func readFact[T any](ctx context.Context, now func() time.Time, timeout time.Duration, source string, read func(context.Context) (T, error)) Fact[T] {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	safego.Go("release-context-read", func() {
		v, err := read(ctx)
		done <- result{v, err}
	})
	fact := Fact[T]{Source: source}
	select {
	case r := <-done:
		if r.err != nil {
			fact.Error = r.err.Error()
		} else {
			v := r.v
			fact.Value = &v
		}
	case <-ctx.Done():
		fact.Error = fmt.Sprintf("no answer within %s: %v", timeout, ctx.Err())
	}
	fact.ReadAt = now().UTC().Format(time.RFC3339)
	return fact
}

// deployLane is the release lane whose deploying pipeline for env ships repo:
// the window table names the pipelines that ship a repo, the release rules
// say which of them deploys to env and in which lane.
func deployLane(rule func(string) (releases.Rule, bool), repo, env string) (string, bool) {
	if rule == nil {
		return "", false
	}
	pipelines := make([]string, 0, len(windowPipelines))
	for name, p := range windowPipelines {
		if p.repos[repo] {
			pipelines = append(pipelines, name)
		}
	}
	sort.Strings(pipelines)
	for _, name := range pipelines {
		if r, ok := rule(name); ok && r.Deploys && r.Env == env {
			return r.Lane, true
		}
	}
	return "", false
}

func readDeployed(ctx context.Context, src ReleaseContextSources, now func() time.Time, timeout time.Duration, repo, env string) Fact[Deployed] {
	lane, ok := deployLane(src.Rule, repo, env)
	if !ok {
		reason := "no release rule deploys " + repo + " to " + env + " (its pipelines build images that a pin and a GitOps sync deploy, or the rules are not loaded)"
		return Fact[Deployed]{Source: "release rules (release-rules.yaml) and the release window's pipeline table", Error: reason, ReadAt: now().UTC().Format(time.RFC3339)}
	}
	return readFact(ctx, now, timeout, "release records, lane "+lane+", env "+env, func(ctx context.Context) (Deployed, error) {
		if src.Current == nil {
			return Deployed{}, errors.New("this platform has no release records wired")
		}
		rec, err := src.Current(ctx, lane, env)
		if err != nil {
			return Deployed{}, err
		}
		build, ok := rec.Repos[repo]
		if !ok || build.SHA == "" {
			return Deployed{}, fmt.Errorf("the newest %s record of lane %s (run %s) names no commit for %s", env, lane, rec.Run, repo)
		}
		return Deployed{Commit: build.SHA, Run: rec.Run, CompletedAt: rec.CompletedAt.UTC().Format(time.RFC3339)}, nil
	})
}

// runsInFlight pages through the runs and keeps, page by page, only a short
// summary of the unfinished runs of repo: a full unpaged list of every run
// once took the workers past their 512Mi limit.
func runsInFlight(ctx context.Context, src ReleaseContextSources, repo string) ([]RunInFlight, error) {
	if src.Runs == nil {
		return nil, errors.New("this platform has no run lister wired")
	}
	out := []RunInFlight{}
	seen := map[string]bool{}
	cont := ""
	for page := 0; ; page++ {
		if page == releaseContextMaxPages {
			return nil, fmt.Errorf("more than %d pages of runs; not all were read", releaseContextMaxPages)
		}
		items, next, err := src.Runs(ctx, cont)
		if err != nil {
			return nil, err
		}
		for i := range items {
			if run, ok := inFlightOf(&items[i], repo); ok {
				out = append(out, run)
			}
		}
		if next == "" {
			break
		}
		if seen[next] {
			return nil, errors.New("pipeline run pagination repeated a cursor")
		}
		seen[next] = true
		cont = next
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt < out[j].StartedAt })
	return out, nil
}

// inFlightOf is a run's summary when it has not finished and concerns repo:
// its repo param or bifrost.io/repo label, its repo-url or url param, or a
// pipeline the release window table says ships repo.
func inFlightOf(run *unstructured.Unstructured, repo string) (RunInFlight, bool) {
	if finished(run) {
		return RunInFlight{}, false
	}
	pipeline := run.GetLabels()["tekton.dev/pipeline"]
	if pipeline == "" {
		pipeline, _, _ = unstructured.NestedString(run.Object, "spec", "pipelineRef", "name")
	}
	params := map[string]string{}
	ps, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, p := range ps {
		m, _ := p.(map[string]any)
		name, _ := m["name"].(string)
		if v, ok := m["value"].(string); ok {
			params[name] = strings.TrimSpace(v)
		}
	}
	wp, delivery := windowPipelines[pipeline]
	concerns := params["repo"] == repo || run.GetLabels()["bifrost.io/repo"] == repo ||
		urlNames(params["repo-url"], repo) || urlNames(params["url"], repo) || (delivery && wp.repos[repo])
	if !concerns {
		return RunInFlight{}, false
	}
	started, _, _ := unstructured.NestedString(run.Object, "status", "startTime")
	if started == "" {
		started = run.GetCreationTimestamp().UTC().Format(time.RFC3339)
	}
	return RunInFlight{Name: run.GetName(), Pipeline: pipeline, StartedAt: started, Revision: params["revision"], Delivery: delivery}, true
}

// finished: the run has a completion time, or its Succeeded condition is True or False.
func finished(run *unstructured.Unstructured) bool {
	if done, _, _ := unstructured.NestedString(run.Object, "status", "completionTime"); done != "" {
		return true
	}
	conds, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, c := range conds {
		m, _ := c.(map[string]any)
		if m["type"] == "Succeeded" && (m["status"] == "True" || m["status"] == "False") {
			return true
		}
	}
	return false
}

// urlNames: a git URL whose last path element is repo (with or without .git).
func urlNames(raw, repo string) bool {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return false
	}
	return strings.TrimSuffix(path.Base(raw), ".git") == repo
}

// pendingFor keeps the approvals that concern repo: a pipeline run of a
// pipeline that ships it, or any parameter naming it.
func pendingFor(list []approvals.Approval, repo string) []PendingApproval {
	out := []PendingApproval{}
	for _, a := range list {
		if !approvalConcerns(a, repo) {
			continue
		}
		out = append(out, PendingApproval{
			ID: a.ID, Number: a.Number, Action: a.Action, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
			Requester: a.Requester, Summary: a.Summary,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
	return out
}

func approvalConcerns(a approvals.Approval, repo string) bool {
	if name, _ := a.Params["name"].(string); a.Action == "start_pipeline_run" && windowPipelines[name].repos[repo] {
		return true
	}
	for _, v := range a.KeyParams {
		if valueNames(v, repo) {
			return true
		}
	}
	return anyNames(a.Params, repo)
}

func anyNames(v any, repo string) bool {
	switch x := v.(type) {
	case string:
		return valueNames(x, repo)
	case map[string]any:
		for _, e := range x {
			if anyNames(e, repo) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if anyNames(e, repo) {
				return true
			}
		}
	}
	return false
}

// valueNames: the value is repo, a comma-separated list holding it (a
// window's what), or a URL ending in it.
func valueNames(v, repo string) bool {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == repo || urlNames(part, repo) {
			return true
		}
	}
	return false
}

func windowView(rec windowRecord, found bool, now time.Time) WindowView {
	view := WindowView{Queue: []WindowWait{}}
	if !found {
		return view
	}
	view.Held = rec.live(now)
	view.Who, view.What, view.Reason, view.ExpiresAt, view.EndedAt = rec.Who, rec.What, rec.Reason, rec.ExpiresAt, rec.EndedAt
	for _, e := range rec.Queue {
		view.Queue = append(view.Queue, WindowWait{Who: e.Who, What: e.What, Since: e.Since, Until: e.Until})
	}
	return view
}

// ownerLastAction is the newest Owner decision: an approval approved (it
// carries the channel the Owner approved on) or rejected (only the admin
// route rejects), or a question answered other than by a rule (only an admin
// answers). Both stores must read; one alone could be older than the other.
func ownerLastAction(src ReleaseContextSources) (OwnerAction, error) {
	if src.ApprovalHistory == nil || src.Questions == nil {
		return OwnerAction{}, errors.New("this platform does not have both the approval and the question store wired")
	}
	list, err := src.ApprovalHistory()
	if err != nil {
		return OwnerAction{}, fmt.Errorf("approval records unreadable: %w", err)
	}
	qs, err := src.Questions()
	if err != nil {
		return OwnerAction{}, fmt.Errorf("question records unreadable: %w", err)
	}
	var best OwnerAction
	var bestAt time.Time
	for _, a := range list {
		human := a.Channel != "" || (a.Status == approvals.StatusRejected && a.RejectReason != "")
		if human && !a.DecidedAt.IsZero() && a.DecidedAt.After(bestAt) {
			bestAt, best.Kind = a.DecidedAt, "approval"
		}
	}
	for _, q := range qs {
		if q.Answer != nil && q.Answer.Channel != questions.RuleChannel && !q.Answer.At.IsZero() && q.Answer.At.After(bestAt) {
			bestAt, best.Kind = q.Answer.At, "question"
		}
	}
	if bestAt.IsZero() {
		return OwnerAction{}, errors.New("no Owner decision is in the retained approval and question records")
	}
	best.At = bestAt.UTC().Format(time.RFC3339)
	return best, nil
}

//go:embed release_lessons.json
var releaseLessonsJSON []byte

func releaseLessons() ([]Lesson, error) {
	var out []Lesson
	if err := json.Unmarshal(releaseLessonsJSON, &out); err != nil {
		return nil, fmt.Errorf("release_lessons.json: %w", err)
	}
	return out, nil
}

// lessonsFor keeps the lessons of repo and those of every repo (no repos).
func lessonsFor(all []Lesson, repo string) []Lesson {
	out := []Lesson{}
	for _, l := range all {
		keep := len(l.Repos) == 0
		for _, r := range l.Repos {
			keep = keep || r == repo
		}
		if keep {
			out = append(out, l)
		}
	}
	return out
}

func mainAheadOfSTG(main Fact[MainRef], stg Fact[Deployed], asOf string) Fact[bool] {
	fact := Fact[bool]{ReadAt: asOf, Source: "derived: main.commit compared with stg.commit (true when they differ)"}
	switch {
	case main.Value == nil:
		fact.Error = "main is unread: " + main.Error
	case stg.Value == nil:
		fact.Error = "stg is unread: " + stg.Error
	default:
		v := main.Value.Commit != stg.Value.Commit
		fact.Value = &v
	}
	return fact
}

func verdicts(f *ContextFacts, commit string) *ContextVerdicts {
	yes, no := true, false
	out := &ContextVerdicts{}
	if f.STG.Value == nil {
		out.CommitIsSTG.Why = "cannot tell: stg is unread: " + f.STG.Error
	} else if f.STG.Value.Commit == commit {
		out.CommitIsSTG = Verdict{&yes, "STG runs this commit (run " + f.STG.Value.Run + ")"}
	} else {
		out.CommitIsSTG = Verdict{&no, "STG runs " + f.STG.Value.Commit + " (run " + f.STG.Value.Run + "), not this commit"}
	}
	if f.Main.Value == nil {
		out.MainAheadOfCommit.Why = "cannot tell: main is unread: " + f.Main.Error
	} else if f.Main.Value.Commit == commit {
		out.MainAheadOfCommit = Verdict{&no, "main is at this commit"}
	} else {
		out.MainAheadOfCommit = Verdict{&yes, "main is at " + f.Main.Value.Commit + ", not this commit (ancestry is not checked)"}
	}
	out.OthersInFlight = othersInFlight(f, commit)
	return out
}

// othersInFlight: a delivery run of the repo in flight (any commit), a CI or
// landing run of it at another commit, or an approval of it pending. The
// window is not counted: the caller may be its holder (facts.window says who).
func othersInFlight(f *ContextFacts, commit string) Verdict {
	if f.RunsInFlight.Value == nil {
		return Verdict{Why: "cannot tell: runs are unread: " + f.RunsInFlight.Error}
	}
	if f.PendingApprovals.Value == nil {
		return Verdict{Why: "cannot tell: pending approvals are unread: " + f.PendingApprovals.Error}
	}
	var names []string
	for _, r := range *f.RunsInFlight.Value {
		if r.Delivery || r.Revision != commit {
			names = append(names, "run "+r.Name)
		}
	}
	for _, a := range *f.PendingApprovals.Value {
		ref := a.ID
		if a.Number > 0 {
			ref = fmt.Sprintf("#%d", a.Number)
		}
		names = append(names, "approval "+ref+" ("+a.Action+")")
	}
	if len(names) == 0 {
		no := false
		return Verdict{&no, "no delivery run, no run at another commit and no pending approval of this repo"}
	}
	yes := true
	return Verdict{&yes, strings.Join(names, ", ")}
}
