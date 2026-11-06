package controller_test

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// R5:5.7 goal 2 (controller/landing_batch.go): on PROD the platform's
// accepted roots land in one batch: one landing CI, one push of R. Driven
// through the drill pass on a real database, with Git, CI and main faked.

const batchM0 = "3333333333333333333333333333333333333333"

// batchRig is a live platform landing with its fakes.
type batchRig struct {
	pool *pgxpool.Pool
	ctl  *controller.Controller
	src  *controller.DrillSources

	mu      sync.Mutex
	offset  time.Duration
	paths   map[string]string // A -> the file its result changes
	refs    map[string]string // land branches pushed
	ensured []string          // EnsureCI names, in order
	runs    map[string]landing.CIRun
	status  string // the status every run reports
	failure string // the failed TaskRun's log
	pushes  []string
	replays []string
}

// clock moves the event clock (the decision time) on by d.
func (r *batchRig) clock(d time.Duration) {
	r.mu.Lock()
	r.offset += d
	r.mu.Unlock()
}

func (r *batchRig) now() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Now().Add(r.offset)
}

type batchGit struct{ r *batchRig }

func (g batchGit) Open(context.Context, string) (*landinggit.Workspace, error) {
	return nil, errors.New("a batch replays in its own workspace")
}
func (g batchGit) Ref(_ context.Context, ref string) (string, error) {
	g.r.mu.Lock()
	defer g.r.mu.Unlock()
	return g.r.refs[ref], nil
}
func (g batchGit) Delete(context.Context, string, string) error { return nil }

// batchWorkspace replays deterministically: a commit named for what it
// was replayed from.
type batchWorkspace struct{ r *batchRig }

func (w batchWorkspace) Main(context.Context) (string, error) { return batchM0, nil }
func (w batchWorkspace) Fetch(context.Context, string) error  { return nil }
func (w batchWorkspace) Close() error                         { return nil }
func (w batchWorkspace) Push(_ context.Context, ref, sha string) error {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	w.r.refs[ref] = sha
	return nil
}
func (w batchWorkspace) Replay(_ context.Context, in landinggit.ReplayInput) (string, error) {
	sum := sha1.Sum([]byte(strings.Join([]string{in.A, in.M0, in.Operation, in.CommitterDate.UTC().String()}, "|")))
	w.r.mu.Lock()
	w.r.replays = append(w.r.replays, in.Operation+"@"+in.M0)
	w.r.mu.Unlock()
	return hex.EncodeToString(sum[:]), nil
}

type batchPusher struct{ r *batchRig }

func (p batchPusher) PushMain(_ context.Context, r, m0 string, _ time.Time) landinggit.PushOutcome {
	p.r.mu.Lock()
	defer p.r.mu.Unlock()
	p.r.pushes = append(p.r.pushes, r+"<-"+m0)
	return landinggit.PushOK
}
func (p batchPusher) Close() error { return nil }

func batchRule(at time.Time) string {
	return fmt.Sprintf(`rule_id: landing:dev
rule_version: 1
direction: initial
effective_at: %s
expires_at: %s
tenant: dev
repos:
  - repo: bifrost-platform
    allow: ["console/src/components/**/*.tsx", "tools/**/*.go"]
    deny: []
    stg_release: none
limits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 20, max_in_flight_per_repo: 1}
`, at.Add(-24*time.Hour).UTC().Format(time.RFC3339), at.Add(30*24*time.Hour).UTC().Format(time.RFC3339))
}

// newBatchRig is a PROD live platform landing (rule, mode, enabled), mission
// M-3 active after it, grants enabled when asked, and the controller wired
// to the fakes.
func newBatchRig(t *testing.T, grants bool) *batchRig {
	t.Helper()
	pool := setup(t)
	r := &batchRig{pool: pool, paths: map[string]string{}, refs: map[string]string{}, runs: map[string]landing.CIRun{}, status: "Running"}
	t.Cleanup(store.SetEventClock(r.now))
	key := rptest.NewKey(t)
	sign := func(text string) {
		t.Helper()
		id, _, _ := strings.Cut(strings.TrimPrefix(text, "rule_id: "), "\n")
		_, err := store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, id, []byte(text), key.Sign(t, []byte(text), rules.Namespace), key.Fingerprint)
		must(t, err)
	}
	sign(batchRule(time.Now()))
	if grants {
		_, err := store.EnsureSeats(ctx, pool, []string{"seat:coordinator"})
		must(t, err)
		must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
		must(t, store.RegisterHost(ctx, pool, "owner", "coordinator-mbp", store.HostExternal))
		sign(fmt.Sprintf(`rule_id: standing-envelope:dev
rule_version: 1
direction: initial
effective_at: %s
expires_at: %s
tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude, exec:codex]
hosts: [h1, coordinator-mbp]
external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform]
    roles: [implement]
`, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().Add(30*24*time.Hour).UTC().Format(time.RFC3339)))
	}
	_, err := store.EnsureLandingMode(ctx, pool, "bifrost-platform")
	must(t, err)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.ObserveLandingEnabled(ctx, tx, "prod", "dev", "bifrost-platform", time.Now())
		return err
	}))
	time.Sleep(10 * time.Millisecond)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "active", "test")
	}))
	r.src = &controller.DrillSources{
		Facts: store.LandingSource{
			Result: func(_ context.Context, _, _, _, _, a string) (bool, []rules.LandingChange, error) {
				r.mu.Lock()
				p := r.paths[a]
				r.mu.Unlock()
				return true, []rules.LandingChange{{OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "x"}}, nil
			},
			History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
				return landing.MainHistory{State: landing.Never}, nil
			},
		},
		Main:          func(context.Context, string) (string, error) { return batchM0, nil },
		WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
		CI: func(_ context.Context, _ string, rev string, after time.Time) (landing.CIRun, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if rev == batchM0 {
				return landing.CIRun{Run: "main", R: batchM0, Status: "Succeeded", CreatedAt: time.Now().Add(-time.Hour)}, nil
			}
			var runs []landing.CIRun
			for _, run := range r.runs {
				run.Status = r.status
				runs = append(runs, run)
			}
			return landing.LatestCI(runs, rev, after), nil
		},
		EnsureCI: func(_ context.Context, _ string, name, rev string) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.ensured = append(r.ensured, name)
			if _, ok := r.runs[name]; !ok {
				r.runs[name] = landing.CIRun{Run: name, R: rev, CreatedAt: time.Now().Add(r.offset).Truncate(time.Second)}
			}
			return nil
		},
		CIFailure: func(_ context.Context, run string) (landing.CIFailure, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			return landing.CIFailure{Pipeline: "bifrost-ci-platform", Run: run, TaskRuns: []landing.CIFailedTask{
				{Name: run + "-go-test", PipelineTask: "go-test", Step: "test", Message: `"step-test" exited with code 1`, LogTail: r.failure}}}, nil
		},
		OpenBatch: func(context.Context, string, string) (controller.BatchWorkspace, error) {
			return batchWorkspace{r}, nil
		},
	}
	r.ctl = &controller.Controller{DB: pool, Env: "prod", Live: true, DrillGit: map[string]controller.DrillGit{"bifrost-platform": batchGit{r}},
		Drill: r.src, OpenLive: func(context.Context, string, string) (controller.LiveWorkspace, error) { return batchPusher{r}, nil }}
	return r
}

// accepted is a root of M-3 accepted on submission (its landing CI is its
// check), whose result changes path.
func (r *batchRig) accepted(t *testing.T, path string) string {
	t.Helper()
	var spec, evidence string
	must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if spec, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", implSpec(t)); err != nil {
			return err
		}
		evidence, err = store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte("pass "+path+"\n"))
		return err
	}))
	out, err := store.ProposeTasks(ctx, r.pool, store.Writer{Actor: "thread:s1", Token: "operator"}, "batch "+path, "M-3", []store.TaskProposal{
		{Ref: "a", OffPlanReason: "batch landing", Role: "implement", Class: "go", Title: path, SpecArtifact: spec, Effect: "branch",
			Repo: "bifrost-platform", BaseCommit: strings.Repeat("b", 40)}})
	must(t, err)
	task := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	sum := sha1.Sum([]byte(task))
	a := hex.EncodeToString(sum[:])
	r.mu.Lock()
	r.paths[a] = path
	r.mu.Unlock()
	attempt := store.NewAttemptID()
	must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var from string
		if err := tx.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, task).Scan(&from); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id, task_id, attempt_no, executor_actor_id, lease_epoch,
		    lease_expires_at, heartbeat_at, state, conclusion, pushed_ref, pushed_commit, computed_at, persisted_at, report_artifact_id)
		  VALUES ($1, $2, 1, 'controller', 1, now(), now(), 'finished', 'done', $3, $4, now(), now(), $5)`,
			attempt, task, store.AttemptRef(task, 1), a, evidence); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict,
		    evidence_artifact_id, subject_commit, accepts_attempt) VALUES ($1, $2, $3, 'landing_ci', 'pass', $4, $5, true)`,
			store.NewVerificationID(), task, attempt, evidence, a); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.task SET state = 'accepted' WHERE task_id = $1`, task); err != nil {
			return err
		}
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-3"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-3", TaskID: task, Body: map[string]any{"from": from, "to": "accepted", "reason": "accepted on submission"}})
		return err
	}))
	return task
}

func (r *batchRig) pass(t *testing.T) {
	t.Helper()
	pass, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	must(t, r.ctl.LandingDrillPass(pass))
}

func (r *batchRig) state(t *testing.T) landing.State {
	t.Helper()
	var s landing.State
	must(t, store.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		s, err = store.LandingState(ctx, tx, "landing:bifrost-platform")
		return err
	}))
	return s
}

// latest is each root's latest operation.
func (r *batchRig) latest(t *testing.T, roots []string) []landing.Operation {
	t.Helper()
	s := r.state(t)
	var out []landing.Operation
	for _, root := range roots {
		out = append(out, s.Operations[s.LatestOperation(root)])
	}
	return out
}

func (r *batchRig) batches(t *testing.T) []store.LandingBatch {
	t.Helper()
	var out []store.LandingBatch
	s := r.state(t)
	seen := map[string]bool{}
	for id := range s.Operations {
		b, ok, err := store.LandingBatchOf(ctx, r.pool, "bifrost-platform", id)
		must(t, err)
		if ok && !seen[b.Batch] {
			seen[b.Batch] = true
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b store.LandingBatch) int { return int(a.Seq - b.Seq) })
	return out
}

func (r *batchRig) taskState(t *testing.T, task string) string {
	t.Helper()
	var s string
	must(t, r.pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, task).Scan(&s))
	return s
}

// formed runs the pass that forms a batch of roots and pushes its land
// branches, then the one that kicks its CI.
func (r *batchRig) formed(t *testing.T, roots []string) store.LandingBatch {
	t.Helper()
	r.pass(t)
	bs := r.batches(t)
	if len(bs) == 0 {
		t.Fatal("no batch formed")
	}
	b := bs[len(bs)-1]
	ops := r.latest(t, roots)
	for i, o := range ops {
		if o.Phase != "branch_pushed" || o.R != b.R || o.M0 != batchM0 || b.Members[i] != r.state(t).LatestOperation(roots[i]) {
			t.Fatalf("member %d: %s R %s M0 %s; batch %+v", i, o.Phase, o.R, o.M0, b)
		}
		if r.refs[o.Ref] != b.R {
			t.Fatal("land branch not pushed", o.Ref)
		}
	}
	r.clock(31 * time.Second)
	r.pass(t)
	return b
}

// Three accepted roots: one batch, one EnsureCI (the lead's), one push of
// R to main, and landed for all three.
func TestBatchThreeRootsOneCIOnePush(t *testing.T) {
	r := newBatchRig(t, false)
	var roots []string
	for i := range 3 {
		roots = append(roots, r.accepted(t, fmt.Sprintf("console/src/components/Batch%d.tsx", i)))
	}
	b := r.formed(t, roots)
	if len(b.Members) != 3 || b.Lead != b.Members[0] {
		t.Fatal("batch", b)
	}
	// Each member replayed on the one before, main for the first.
	if len(r.replays) != 3 || r.replays[0] != b.Members[0]+"@"+batchM0 {
		t.Fatal("replays", r.replays)
	}
	if !slices.Equal(r.ensured, []string{landing.CIName(b.Lead, 1)}) {
		t.Fatal("CI kicked", r.ensured)
	}
	r.clock(time.Minute)
	r.status = "Succeeded"
	r.pass(t)
	if !slices.Equal(r.pushes, []string{b.R + "<-" + batchM0}) {
		t.Fatal("pushes", r.pushes)
	}
	if !slices.Equal(r.ensured, []string{landing.CIName(b.Lead, 1)}) {
		t.Fatal("CI kicked again", r.ensured)
	}
	for _, o := range r.latest(t, roots) {
		if o.Phase != "main_result" || o.Outcome != "ok" || o.CI.String("run") != landing.CIName(b.Lead, 1) {
			t.Fatal("member", o.Phase, o.Outcome, o.CI)
		}
	}
	r.clock(time.Minute)
	_, err := store.ReconcileMain(ctx, r.pool, "bifrost-platform", func(context.Context, string, string, string, string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: b.R, HasR: true, Validity: "effective"}, nil
	})
	must(t, err)
	for i, o := range r.latest(t, roots) {
		if o.Phase != "landed" {
			t.Fatal("not landed", roots[i], o.Phase)
		}
	}
	r.pass(t)
	if len(r.pushes) != 1 || len(r.ensured) != 1 {
		t.Fatal("more after landing", r.pushes, r.ensured)
	}
}

// red takes a formed batch's CI red: the kick's run fails, its rerun
// fails, and the lead ends the batch; the same pass claims what is free.
func (r *batchRig) red(t *testing.T, b store.LandingBatch, log string) {
	t.Helper()
	r.failure = log
	r.status = "Failed"
	r.clock(time.Minute)
	r.pass(t)
	if !slices.Contains(r.ensured, landing.CIName(b.Lead, 2)) {
		t.Fatal("no rerun", r.ensured)
	}
	r.clock(time.Minute)
	r.pass(t)
}

// A red run whose failing package maps to one member sends that one back
// for its next round; the other two are retried and form the next batch.
func TestBatchRedBlamesOneAndRebatchesTheOthers(t *testing.T) {
	r := newBatchRig(t, false)
	var roots []string
	for _, p := range []string{"tools/blame/a/a.go", "tools/blame/b/b.go", "tools/blame/c/c.go"} {
		roots = append(roots, r.accepted(t, p))
	}
	b := r.formed(t, roots)
	r.red(t, b, "--- FAIL: TestB (0.01s)\n    b_test.go:9: wrong\nFAIL\nFAIL\tgithub.com/weitingzhao/bifrost-platform/tools/blame/b\t0.02s\nok  \tgithub.com/weitingzhao/bifrost-platform/tools/blame/a\t0.01s\n")
	s := r.state(t)
	for i, op := range b.Members {
		if o := s.Operations[op]; o.Phase != "failed" || o.Outcome != "ci_red" || o.Retried != (i != 1) {
			t.Fatalf("member %d: %s %s retried %v", i, o.Phase, o.Outcome, o.Retried)
		}
	}
	if r.taskState(t, roots[1]) != "ready" || r.taskState(t, roots[0]) != "accepted" {
		t.Fatal("rework", r.taskState(t, roots[1]), r.taskState(t, roots[0]))
	}
	bs := r.batches(t)
	if len(bs) != 2 {
		t.Fatal("not re-batched", len(bs))
	}
	next := bs[1]
	if !slices.Equal(next.Members, []string{s.LatestOperation(roots[0]), s.LatestOperation(roots[2])}) || next.After != "" {
		t.Fatalf("the next batch: %+v", next)
	}
	if s.LatestOperation(roots[1]) != b.Members[1] {
		t.Fatal("the blamed root was claimed again")
	}
	if len(r.pushes) != 0 {
		t.Fatal("pushed a red batch", r.pushes)
	}
}

// A red run mapping to no member splits the batch: every member is
// retried, and the first half forms the next batch, naming the red one.
func TestBatchRedMappingToNoneSplits(t *testing.T) {
	r := newBatchRig(t, false)
	var roots []string
	for _, p := range []string{"tools/split/a/a.go", "tools/split/b/b.go", "tools/split/c/c.go"} {
		roots = append(roots, r.accepted(t, p))
	}
	b := r.formed(t, roots)
	r.red(t, b, "--- FAIL: TestOther (0.01s)\nFAIL\nFAIL\tgithub.com/weitingzhao/bifrost-platform/api/internal/elsewhere\t0.02s\n")
	s := r.state(t)
	for i, op := range b.Members {
		if o := s.Operations[op]; o.Phase != "failed" || o.Outcome != "ci_red" || !o.Retried || r.taskState(t, roots[i]) != "accepted" {
			t.Fatalf("member %d: %s %s retried %v", i, o.Phase, o.Outcome, o.Retried)
		}
	}
	bs := r.batches(t)
	if len(bs) != 2 {
		t.Fatal("no half formed", len(bs))
	}
	if half := bs[1]; !slices.Equal(half.Members, []string{s.LatestOperation(roots[0]), s.LatestOperation(roots[1])}) || half.After != b.Batch {
		t.Fatalf("the first half: %+v", half)
	}
	if s.LatestOperation(roots[2]) != b.Members[2] {
		t.Fatal("the second half did not wait")
	}
}

// Of three waiting roots one touches a permission path (a core change,
// grants enabled): it leads, and only its operation records awaiting_grant;
// nothing is pushed.
func TestBatchOnlyTheLeadAwaitsAGrant(t *testing.T) {
	r := newBatchRig(t, true)
	roots := []string{
		r.accepted(t, "console/src/components/GrantA.tsx"),
		r.accepted(t, "api/internal/agentrt/store/batch_grant.go"),
		r.accepted(t, "console/src/components/GrantC.tsx"),
	}
	b := r.formed(t, []string{roots[1], roots[0], roots[2]})
	if s := r.state(t); b.Lead != s.LatestOperation(roots[1]) {
		t.Fatal("the core root does not lead", b)
	}
	r.clock(time.Minute)
	r.status = "Succeeded"
	r.pass(t)
	var waiting []string
	for id, o := range r.state(t).Operations {
		if o.Phase == "awaiting_grant" || !o.AwaitingAt.IsZero() {
			waiting = append(waiting, id)
		}
	}
	if !slices.Equal(waiting, []string{b.Lead}) {
		t.Fatal("awaiting_grant", waiting)
	}
	for _, o := range r.latest(t, []string{roots[0], roots[2]}) {
		if o.Phase != "branch_pushed" {
			t.Fatal("a member moved", o.Phase)
		}
	}
	if len(r.pushes) != 0 {
		t.Fatal("pushed without a grant", r.pushes)
	}
}
