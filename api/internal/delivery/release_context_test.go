package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

const (
	ctxSTG  = "1111111111111111111111111111111111111111"
	ctxMain = "2222222222222222222222222222222222222222"
	ctxPROD = "3333333333333333333333333333333333333333"
)

var ctxNow = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

func ctxRun(name, pipeline string, labels map[string]string, params map[string]string, done bool) unstructured.Unstructured {
	l := map[string]any{}
	if pipeline != "" {
		l["tekton.dev/pipeline"] = pipeline
	}
	for k, v := range labels {
		l[k] = v
	}
	var ps []any
	for k, v := range params {
		ps = append(ps, map[string]any{"name": k, "value": v})
	}
	status := map[string]any{"startTime": "2026-11-15T11:00:00Z"}
	if done {
		status["completionTime"] = "2026-11-15T11:30:00Z"
		status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": "True"}}
	} else {
		status["conditions"] = []any{map[string]any{"type": "Succeeded", "status": "Unknown"}}
	}
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name, "labels": l},
		"spec":     map[string]any{"params": ps},
		"status":   status,
	}}
}

// fineSources reads every fact without error: platform STG at ctxSTG, PROD
// at ctxPROD, main at ctxMain.
func fineSources() ReleaseContextSources {
	rules := map[string]releases.Rule{
		"bifrost-deliver-platform":      {Lane: "platform", Env: "stg", Deploys: true},
		"bifrost-deliver-platform-prod": {Lane: "platform", Env: "prod", Deploys: true},
		"bifrost-deliver-stg":           {Lane: "trade", Env: "stg", Deploys: true},
		"bifrost-deliver-research":      {Lane: "research", Env: "image", Deploys: false},
	}
	return ReleaseContextSources{
		Now:  func() time.Time { return ctxNow },
		Rule: func(p string) (releases.Rule, bool) { r, ok := rules[p]; return r, ok },
		Current: func(_ context.Context, lane, env string) (releases.Record, error) {
			sha := map[string]string{"stg": ctxSTG, "prod": ctxPROD}[env]
			return releases.Record{Run: lane + "-" + env + "-run", CompletedAt: ctxNow.Add(-time.Hour),
				Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha}, "bifrost-ui": {SHA: "u"}}}, nil
		},
		MainHead: func(context.Context, string) (string, error) { return ctxMain, nil },
		Runs: func(context.Context, string) ([]unstructured.Unstructured, string, error) {
			return []unstructured.Unstructured{
				ctxRun("ci-platform-a", "bifrost-ci-platform", map[string]string{"bifrost.io/repo": "bifrost-platform"}, map[string]string{"revision": ctxMain}, false),
				ctxRun("ci-platform-done", "bifrost-ci-platform", map[string]string{"bifrost.io/repo": "bifrost-platform"}, map[string]string{"revision": ctxMain}, true),
				ctxRun("ci-python-x", "bifrost-ci-python", nil, map[string]string{"repo": "bifrost-trade-api", "revision": ctxMain}, false),
			}, "", nil
		},
		Window: func(context.Context) (windowRecord, bool, error) {
			return windowRecord{Who: "duty", What: "bifrost-platform", Reason: "release", ExpiresAt: ctxNow.Add(3 * time.Minute).Format(time.RFC3339),
				Queue: []queueEntry{{Who: "lane-b", What: "bifrost-platform", Since: "2026-11-15T11:58:00Z", Until: "2026-11-15T12:20:00Z"}}}, true, nil
		},
		Waiting: func() ([]approvals.Approval, error) {
			return []approvals.Approval{
				{ID: "appr_1", Number: 570, Action: "start_pipeline_run", Params: map[string]any{"name": "bifrost-deliver-platform-prod"}, CreatedAt: ctxNow.Add(-10 * time.Minute), Requester: "s1", Summary: "bifrost-deliver-platform-prod @ 22222222"},
				{ID: "appr_2", Number: 571, Action: "start_pipeline_run", Params: map[string]any{"name": "bifrost-deliver-prod"}, CreatedAt: ctxNow, Requester: "s2"},
			}, nil
		},
		ApprovalHistory: func() ([]approvals.Approval, error) {
			return []approvals.Approval{
				{ID: "a", Status: approvals.StatusExecuted, Channel: "phone", DecidedAt: ctxNow.Add(-30 * time.Minute)},
				// An expiry is not a decision of the Owner.
				{ID: "b", Status: approvals.StatusExpired, DecidedAt: ctxNow.Add(-time.Minute)},
			}, nil
		},
		Questions: func() ([]questions.Question, error) {
			return []questions.Question{
				{ID: "q1", Answer: &questions.Answer{By: "owner", At: ctxNow.Add(-20 * time.Minute), Channel: "phone"}},
				// A rule's answer is not the Owner's.
				{ID: "q2", Answer: &questions.Answer{By: "rule", At: ctxNow.Add(-2 * time.Minute), Channel: questions.RuleChannel}},
			}, nil
		},
	}
}

// 2026-11-15: duty asked for one read of the release situation; cards
// #564-#566 were built on a commit STG no longer ran.
func TestReleaseContextAllSourcesFine(t *testing.T) {
	t.Parallel()
	got := AssembleReleaseContext(context.Background(), fineSources(), "bifrost-platform", "")
	f := got.Facts
	if got.Schema != 1 || got.AsOf != "2026-11-15T12:00:00Z" || got.Verdicts != nil || got.Commit != "" {
		t.Fatalf("head: %+v", got)
	}
	if f.STG.Value == nil || f.STG.Value.Commit != ctxSTG || f.STG.Value.Run != "platform-stg-run" || f.STG.Source != "release records, lane platform, env stg" {
		t.Fatalf("stg: %+v", f.STG)
	}
	if f.PROD.Value == nil || f.PROD.Value.Commit != ctxPROD {
		t.Fatalf("prod: %+v", f.PROD)
	}
	if f.Main.Value == nil || f.Main.Value.Commit != ctxMain || f.Main.ReadAt == "" {
		t.Fatalf("main: %+v", f.Main)
	}
	if f.MainAheadOfSTG.Value == nil || !*f.MainAheadOfSTG.Value {
		t.Fatalf("main ahead of stg: %+v", f.MainAheadOfSTG)
	}
	if f.RunsInFlight.Value == nil || len(*f.RunsInFlight.Value) != 1 || (*f.RunsInFlight.Value)[0].Name != "ci-platform-a" {
		t.Fatalf("runs: %+v", f.RunsInFlight)
	}
	if f.PendingApprovals.Value == nil || len(*f.PendingApprovals.Value) != 1 || (*f.PendingApprovals.Value)[0].Number != 570 {
		t.Fatalf("approvals: %+v", f.PendingApprovals)
	}
	if w := f.Window.Value; w == nil || !w.Held || w.Who != "duty" || len(w.Queue) != 1 || w.Queue[0].Who != "lane-b" {
		t.Fatalf("window: %+v", f.Window)
	}
	if o := f.OwnerLastAction.Value; o == nil || o.Kind != "question" || o.At != "2026-11-15T11:40:00Z" {
		t.Fatalf("owner: %+v", f.OwnerLastAction)
	}
	if f.Lessons.Value == nil || len(*f.Lessons.Value) == 0 {
		t.Fatalf("lessons: %+v", f.Lessons)
	}
	for name, e := range map[string]string{"stg": f.STG.Error, "prod": f.PROD.Error, "main": f.Main.Error, "runs": f.RunsInFlight.Error,
		"approvals": f.PendingApprovals.Error, "window": f.Window.Error, "owner": f.OwnerLastAction.Error, "lessons": f.Lessons.Error} {
		if e != "" {
			t.Fatalf("%s: unexpected error %q", name, e)
		}
	}
	// The JSON shape: value null when unread, error omitted when read.
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"stg":{"value":{"commit":"`+ctxSTG) || strings.Contains(string(raw), `"verdicts"`) {
		t.Fatalf("json: %s", raw)
	}
}

// 2026-11-15: one failing source must not fail the read; the old shell
// script stopped at the first kubectl error.
func TestReleaseContextOneSourceFailsOthersStay(t *testing.T) {
	t.Parallel()
	src := fineSources()
	src.MainHead = func(context.Context, string) (string, error) { return "", errors.New("gitea: status 502") }
	src.Waiting = func() ([]approvals.Approval, error) { return nil, errors.New("store unreadable") }
	got := AssembleReleaseContext(context.Background(), src, "bifrost-platform", ctxSTG)
	f := got.Facts
	if f.Main.Value != nil || f.Main.Error != "gitea: status 502" || f.Main.ReadAt == "" {
		t.Fatalf("main: %+v", f.Main)
	}
	if f.MainAheadOfSTG.Value != nil || !strings.Contains(f.MainAheadOfSTG.Error, "main is unread") {
		t.Fatalf("derived: %+v", f.MainAheadOfSTG)
	}
	if f.PendingApprovals.Value != nil || f.PendingApprovals.Error != "store unreadable" {
		t.Fatalf("approvals: %+v", f.PendingApprovals)
	}
	if f.STG.Value == nil || f.Window.Value == nil || f.RunsInFlight.Value == nil {
		t.Fatalf("other facts lost: %+v", f)
	}
	v := got.Verdicts
	if v.MainAheadOfCommit.Value != nil || !strings.Contains(v.MainAheadOfCommit.Why, "502") {
		t.Fatalf("main verdict: %+v", v.MainAheadOfCommit)
	}
	if v.OthersInFlight.Value != nil || !strings.Contains(v.OthersInFlight.Why, "approvals are unread") {
		t.Fatalf("others verdict: %+v", v.OthersInFlight)
	}
	raw, _ := json.Marshal(f.Main)
	if string(raw) != `{"value":null,"read_at":"2026-11-15T12:00:00Z","source":"main branch of bifrost-platform on the platform's Gitea mirror","error":"gitea: status 502"}` {
		t.Fatalf("json: %s", raw)
	}
}

// 2026-11-15: a slow source (Gitea or Tekton hanging) must not hang the route.
func TestReleaseContextSlowSourceTimesOut(t *testing.T) {
	t.Parallel()
	src := fineSources()
	src.Timeout = 50 * time.Millisecond
	src.MainHead = func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		time.Sleep(time.Second)
		return "late", nil
	}
	start := time.Now()
	got := AssembleReleaseContext(context.Background(), src, "bifrost-platform", "")
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("waited %s for a slow source", time.Since(start))
	}
	if got.Facts.Main.Value != nil || !strings.Contains(got.Facts.Main.Error, "no answer within") || got.Facts.STG.Value == nil {
		t.Fatalf("facts: %+v", got.Facts)
	}
}

// 2026-11-15: releases.Current answers unsettled while a delivery is in
// flight; the context must say so, not show the previous deployment.
func TestReleaseContextUnsettledSTGIsNullNotOld(t *testing.T) {
	t.Parallel()
	src := fineSources()
	base := src.Current
	src.Current = func(ctx context.Context, lane, env string) (releases.Record, error) {
		if env == "stg" {
			return releases.Record{Run: "old", Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: ctxPROD}}},
				fmt.Errorf("%w: run deliver-x may have changed platform/stg", releases.ErrUnsettled)
		}
		return base(ctx, lane, env)
	}
	got := AssembleReleaseContext(context.Background(), src, "bifrost-platform", ctxSTG)
	if got.Facts.STG.Value != nil || !strings.Contains(got.Facts.STG.Error, "not settled") {
		t.Fatalf("stg: %+v", got.Facts.STG)
	}
	if got.Verdicts.CommitIsSTG.Value != nil || !strings.Contains(got.Verdicts.CommitIsSTG.Why, "not settled") {
		t.Fatalf("verdict: %+v", got.Verdicts.CommitIsSTG)
	}
	if got.Facts.MainAheadOfSTG.Value != nil {
		t.Fatalf("derived from an unread stg: %+v", got.Facts.MainAheadOfSTG)
	}
}

// 2026-11-15: research ships through image builds and a pin; no rule
// deploys it to stg, which must read as an error, not as an empty commit.
func TestReleaseContextRepoWithoutDeployLane(t *testing.T) {
	t.Parallel()
	got := AssembleReleaseContext(context.Background(), fineSources(), "bifrost-research", "")
	if got.Facts.STG.Value != nil || !strings.Contains(got.Facts.STG.Error, "no release rule deploys bifrost-research to stg") || got.Facts.STG.ReadAt == "" {
		t.Fatalf("stg: %+v", got.Facts.STG)
	}
}

// 2026-11-15: verdicts the duty seat asked for before building a release
// card (#564-#566 pinned a commit STG no longer ran).
func TestReleaseContextVerdicts(t *testing.T) {
	t.Parallel()
	src := fineSources()
	none := AssembleReleaseContext(context.Background(), src, "bifrost-platform", "")
	if none.Verdicts != nil {
		t.Fatalf("verdicts without a commit: %+v", none.Verdicts)
	}
	got := AssembleReleaseContext(context.Background(), src, "bifrost-platform", ctxSTG).Verdicts
	if got.CommitIsSTG.Value == nil || !*got.CommitIsSTG.Value {
		t.Fatalf("commit_is_stg: %+v", got.CommitIsSTG)
	}
	if got.MainAheadOfCommit.Value == nil || !*got.MainAheadOfCommit.Value {
		t.Fatalf("main_ahead_of_commit: %+v", got.MainAheadOfCommit)
	}
	// ci-platform-a runs at main (another commit) and #570 is pending.
	if got.OthersInFlight.Value == nil || !*got.OthersInFlight.Value || !strings.Contains(got.OthersInFlight.Why, "#570") || !strings.Contains(got.OthersInFlight.Why, "ci-platform-a") {
		t.Fatalf("others_in_flight: %+v", got.OthersInFlight)
	}
	atMain := AssembleReleaseContext(context.Background(), src, "bifrost-platform", ctxMain).Verdicts
	if atMain.CommitIsSTG.Value == nil || *atMain.CommitIsSTG.Value || atMain.MainAheadOfCommit.Value == nil || *atMain.MainAheadOfCommit.Value {
		t.Fatalf("at main: %+v", atMain)
	}
	// Nothing else in flight: the CI run of the same commit is not someone else.
	src.Waiting = func() ([]approvals.Approval, error) { return nil, nil }
	quiet := AssembleReleaseContext(context.Background(), src, "bifrost-platform", ctxMain).Verdicts
	if quiet.OthersInFlight.Value == nil || *quiet.OthersInFlight.Value {
		t.Fatalf("quiet: %+v", quiet.OthersInFlight)
	}
}

// 2026-11-15: each list must hold only the asked repo's entries.
func TestReleaseContextRepoFilters(t *testing.T) {
	t.Parallel()
	src := fineSources()
	src.Runs = func(context.Context, string) ([]unstructured.Unstructured, string, error) {
		return []unstructured.Unstructured{
			ctxRun("deliver-stg-1", "bifrost-deliver-stg", nil, map[string]string{"revision": "main"}, false),
			ctxRun("ci-api-1", "bifrost-ci-python", nil, map[string]string{"repo": "bifrost-trade-api", "revision": ctxMain}, false),
			ctxRun("inline-1", "inline-1", nil, map[string]string{"repo-url": "http://gitea/bifrost/bifrost-trade-api.git"}, false),
			ctxRun("ci-platform-1", "bifrost-ci-platform", map[string]string{"bifrost.io/repo": "bifrost-platform"}, nil, false),
		}, "", nil
	}
	src.Waiting = func() ([]approvals.Approval, error) {
		return []approvals.Approval{
			{ID: "p1", Action: "start_pipeline_run", Params: map[string]any{"name": "bifrost-deliver-prod"}},
			{ID: "p2", Action: "release_window_release", Params: map[string]any{"what": "bifrost-ui,bifrost-trade-api"}},
			{ID: "p3", Action: "start_pipeline_run", Params: map[string]any{"name": "bifrost-deliver-platform-prod"}},
			{ID: "p4", Action: "gitops_sync_app", Params: map[string]any{"name": "platform-prod"}},
		}, nil
	}
	got := AssembleReleaseContext(context.Background(), src, "bifrost-trade-api", "").Facts
	var runs []string
	for _, r := range *got.RunsInFlight.Value {
		runs = append(runs, r.Name)
	}
	sort.Strings(runs)
	if strings.Join(runs, ",") != "ci-api-1,deliver-stg-1,inline-1" {
		t.Fatalf("runs: %v", runs)
	}
	var ids []string
	for _, a := range *got.PendingApprovals.Value {
		ids = append(ids, a.ID)
	}
	if strings.Join(ids, ",") != "p1,p2" {
		t.Fatalf("approvals: %v", ids)
	}
	for _, l := range *got.Lessons.Value {
		if len(l.Repos) != 0 {
			t.Fatalf("a platform lesson reached trade-api: %+v", l)
		}
	}
	platform := AssembleReleaseContext(context.Background(), src, "bifrost-platform", "").Facts
	hasPlatformLesson := false
	for _, l := range *platform.Lessons.Value {
		hasPlatformLesson = hasPlatformLesson || l.ID == "L-564"
	}
	if !hasPlatformLesson || len(*platform.Lessons.Value) <= len(*got.Lessons.Value) {
		t.Fatalf("platform lessons: %+v", platform.Lessons)
	}
}

// 2026-11-15: an unknown repo or a short commit is the caller's mistake.
func TestReleaseContextHandlerRejectsUnknownRepo(t *testing.T) {
	t.Parallel()
	h := &Handler{svc: NewService(&config.ClusterEntry{ID: "test"})}
	serve := h.HandleReleaseContext(fineSources())
	for _, q := range []string{"", "repo=nope", "repo=bifrost-platform&commit=abc", "repo=bifrost-platform&commit=" + strings.Repeat("AB", 20)} {
		rec := httptest.NewRecorder()
		serve(rec, httptest.NewRequest(http.MethodGet, "/api/v1/delivery/context?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: %d %s", q, rec.Code, rec.Body)
		}
	}
	// Positive control: a known repo answers 200.
	rec := httptest.NewRecorder()
	serve(rec, httptest.NewRequest(http.MethodGet, "/api/v1/delivery/context?repo=bifrost-platform&commit="+ctxSTG, nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"commit_is_stg":{"value":true`) {
		t.Fatalf("known repo: %d %s", rec.Code, rec.Body)
	}
}

// Origin: 2026-11-14, a full unpaged list of every run OOM-killed the STG
// workers at 512Mi; the context's run read goes page by page.
func TestReleaseContextRunsArePaged(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	calls := 0
	dyn.PrependReactor("list", "pipelineruns", func(k8stesting.Action) (bool, runtime.Object, error) {
		page := &unstructured.UnstructuredList{}
		run := ctxRun("first", "bifrost-ci-platform", map[string]string{"bifrost.io/repo": "bifrost-platform"}, nil, false)
		if calls == 0 {
			page.SetContinue("next")
		} else {
			run = ctxRun("second", "bifrost-deliver-platform", nil, nil, false)
		}
		calls++
		run.Object["status"].(map[string]any)["pipelineSpec"] = map[string]any{"tasks": []any{}}
		page.Items = []unstructured.Unstructured{run}
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	items, next, err := s.pipelineRunPage(context.Background(), "")
	if err != nil || next != "next" || len(items) != 1 {
		t.Fatalf("first page: %d items, next %q, err %v", len(items), next, err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(items[0].Object, "status", "pipelineSpec"); found {
		t.Fatal("the page kept status.pipelineSpec")
	}
	calls = 0
	got, err := runsInFlight(context.Background(), ReleaseContextSources{Runs: s.pipelineRunPage}, "bifrost-platform")
	if err != nil || calls != 2 || len(got) != 2 || got[1].Name != "second" || !got[1].Delivery {
		t.Fatalf("runs: %+v calls=%d err=%v", got, calls, err)
	}
	// The cursor each page gave is the one the next page asks with.
	var asked []string
	pages := func(_ context.Context, cont string) ([]unstructured.Unstructured, string, error) {
		asked = append(asked, cont)
		return nil, map[string]string{"": "c1", "c1": "c2"}[cont], nil
	}
	if _, err := runsInFlight(context.Background(), ReleaseContextSources{Runs: pages}, "bifrost-platform"); err != nil || strings.Join(asked, "|") != "|c1|c2" {
		t.Fatalf("asked %q err %v", asked, err)
	}
	// A cursor that repeats is an error, not an endless loop.
	loop := func(context.Context, string) ([]unstructured.Unstructured, string, error) { return nil, "same", nil }
	if _, err := runsInFlight(context.Background(), ReleaseContextSources{Runs: loop}, "bifrost-platform"); err == nil {
		t.Fatal("a repeated cursor read as a full list")
	}
}

// 2026-11-15: the window is reported as stored, ended records included
// (who held it last), and an ended window is not held.
func TestReleaseContextWindowEnded(t *testing.T) {
	t.Parallel()
	ended := windowView(windowRecord{Who: "duty", What: "bifrost-platform", ExpiresAt: "2026-11-15T11:00:00Z", EndedAt: "2026-11-15T11:00:00Z"}, true, ctxNow)
	if ended.Held || ended.Who != "duty" || ended.EndedAt == "" || ended.Queue == nil {
		t.Fatalf("ended: %+v", ended)
	}
	if none := windowView(windowRecord{}, false, ctxNow); none.Held || none.Who != "" || none.Queue == nil {
		t.Fatalf("none: %+v", none)
	}
}

// 2026-11-15: owner_last_action must not be guessed from one store alone.
func TestReleaseContextOwnerLastActionNeedsBothStores(t *testing.T) {
	t.Parallel()
	src := fineSources()
	src.Questions = func() ([]questions.Question, error) { return nil, errors.New("disk") }
	got := AssembleReleaseContext(context.Background(), src, "bifrost-platform", "").Facts.OwnerLastAction
	if got.Value != nil || !strings.Contains(got.Error, "question records unreadable") {
		t.Fatalf("owner: %+v", got)
	}
	src = fineSources()
	src.ApprovalHistory = func() ([]approvals.Approval, error) {
		return []approvals.Approval{{ID: "r", Status: approvals.StatusRejected, RejectReason: "no", DecidedAt: ctxNow.Add(-time.Minute)}}, nil
	}
	got = AssembleReleaseContext(context.Background(), src, "bifrost-platform", "").Facts.OwnerLastAction
	if got.Value == nil || got.Value.Kind != "approval" || got.Value.At != "2026-11-15T11:59:00Z" {
		t.Fatalf("reject: %+v", got)
	}
}

// 2026-11-15: the lessons file is the list of lessons not yet machine
// checks; a malformed entry would silently drop out of every read.
func TestReleaseLessonsFileIsValid(t *testing.T) {
	t.Parallel()
	all, err := releaseLessons()
	if err != nil || len(all) == 0 {
		t.Fatalf("lessons: %v (%d)", err, len(all))
	}
	known := knownRepos()
	seen := map[string]bool{}
	for _, l := range all {
		if l.ID == "" || l.What == "" || l.Check == "" || l.Source == "" || l.Repos == nil || l.Cards == nil {
			t.Fatalf("lesson missing a field: %+v", l)
		}
		if seen[l.ID] {
			t.Fatalf("duplicate id %s", l.ID)
		}
		seen[l.ID] = true
		for _, r := range l.Repos {
			if !known[r] {
				t.Fatalf("%s names unknown repo %q", l.ID, r)
			}
		}
		for _, c := range l.Cards {
			if !strings.HasPrefix(c, "#") {
				t.Fatalf("%s card %q is not #<number>", l.ID, c)
			}
		}
	}
}
