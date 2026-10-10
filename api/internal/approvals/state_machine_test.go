package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// casBackend behaves like the ConfigMap backend: Update reads a version,
// runs mutate without holding the lock, and retries on a version change.
type casBackend struct {
	mu        sync.Mutex
	data      map[string][]byte
	ver       map[string]int
	conflicts int
}

func newCAS() *casBackend {
	return &casBackend{data: map[string][]byte{}, ver: map[string]int{}}
}

func (b *casBackend) Read(_ context.Context, key string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), d...), nil
}

func (b *casBackend) Write(_ context.Context, key string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data[key] = append([]byte(nil), data...)
	b.ver[key]++
	return nil
}

func (b *casBackend) Update(_ context.Context, key string, mutate func(old []byte) ([]byte, error)) error {
	for attempt := 0; attempt < 200; attempt++ {
		b.mu.Lock()
		var old []byte
		if d, ok := b.data[key]; ok {
			old = append([]byte(nil), d...)
		}
		v := b.ver[key]
		b.mu.Unlock()
		runtime.Gosched()
		next, err := mutate(old)
		if err != nil {
			return err
		}
		b.mu.Lock()
		if b.ver[key] != v {
			b.conflicts++
			b.mu.Unlock()
			continue
		}
		b.data[key] = append([]byte(nil), next...)
		b.ver[key]++
		b.mu.Unlock()
		return nil
	}
	return errors.New("still conflicting")
}

// twoPods returns two services over one CAS backend, like two platform-api
// pods during a rolling update.
func twoPods(t *testing.T) (*Service, *Service, *casBackend) {
	t.Helper()
	root := t.TempDir()
	b := newCAS()
	statefile.Use(b, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	path := filepath.Join(root, "approvals")
	return New(path, nil), New(path, nil), b
}

func TestConcurrentCreatesGetDistinctNumbers(t *testing.T) {
	a, b, backend := twoPods(t)
	const each = 25
	var wg sync.WaitGroup
	var mu sync.Mutex
	numbers := map[int]string{}
	errs := make(chan string, 2*each)
	for i := 0; i < each; i++ {
		for _, svc := range []*Service{a, b} {
			wg.Add(1)
			go func(svc *Service, i int) {
				defer wg.Done()
				res := svc.create(context.Background(), "s", "cordon_node", "because", "", map[string]any{"name": fmt.Sprintf("n-%d", i)})
				if res.Status != http.StatusCreated {
					errs <- fmt.Sprintf("create = %d %v", res.Status, res.Body)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if prev, dup := numbers[res.Approval.Number]; dup {
					errs <- fmt.Sprintf("#%d given to %s and %s", res.Approval.Number, prev, res.Approval.ID)
				}
				numbers[res.Approval.Number] = res.Approval.ID
			}(svc, i)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if len(numbers) != 2*each {
		t.Fatalf("distinct numbers = %d, want %d", len(numbers), 2*each)
	}
	for n := 1; n <= 2*each; n++ {
		if _, ok := numbers[n]; !ok {
			t.Fatalf("#%d missing: numbers are not 1..%d", n, 2*each)
		}
	}
	list, _, _ := a.list("all")
	if len(list) != 2*each {
		t.Fatalf("stored = %d, want %d (a write was lost)", len(list), 2*each)
	}
	if backend.conflicts == 0 {
		t.Log("no write conflicts happened; the run still checked uniqueness")
	}
}

func TestNumbersSurvivePruning(t *testing.T) {
	doc := file{LastNumber: 900}
	if n := doc.nextNumber(); n != 901 {
		t.Fatalf("next = %d, want 901", n)
	}
	doc = file{Approvals: []Approval{{ID: "a", Number: 7}}}
	if n := doc.nextNumber(); n != 8 {
		t.Fatalf("next after a numbered record = %d, want 8", n)
	}
}

func approveOwnerRun(t *testing.T, svc *Service) Approval {
	t.Helper()
	c := svc.create(context.Background(), "s", "owner_run_command", "delete a ConfigMap", "", map[string]any{
		"command": "kubectl -n cicd delete configmap x", "reason": "TD-290 remove retired ConfigMap\nmore",
	})
	if c.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", c.Status, c.Body)
	}
	out := svc.approveWith(context.Background(), c.Approval.ID, approveInput{Channel: "console"})
	if out.Status != http.StatusAccepted || out.Body["status"] != StatusApproved {
		t.Fatalf("approve owner_run_command = %d %v", out.Status, out.Body)
	}
	rec, _ := svc.get(c.Approval.ID)
	return rec
}

func TestOwnerRunStaysApprovedWithHandoff(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := approveOwnerRun(t, svc)
	if rec.Status != StatusApproved || rec.Runner != actions.RunnerOwner {
		t.Fatalf("record = %s runner %s", rec.Status, rec.Runner)
	}
	res, _ := rec.Result.(map[string]any)
	if res["command_sha256"] == "" || res["executed_by_platform"] != false {
		t.Fatalf("handoff result = %#v", rec.Result)
	}
	if rec.Env != "host" || rec.Summary != "TD-290 remove retired ConfigMap" || rec.Number != 1 {
		t.Fatalf("env %q summary %q number %d", rec.Env, rec.Summary, rec.Number)
	}
	if strings.Contains(rec.Summary, "kubectl") || rec.KeyParams["command"] != "" {
		t.Fatal("the command leaked into the summary or key params")
	}
	if rec.Execution == nil || rec.Execution.Deadline.Sub(rec.DecidedAt) != handoffDeadline {
		t.Fatalf("execution = %#v", rec.Execution)
	}
}

func TestConcurrentClaimsOnlyOneWins(t *testing.T) {
	a, b, _ := twoPods(t)
	rec := approveOwnerRun(t, a)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, empty := 0, 0
	for i := 0; i < 20; i++ {
		svc := a
		if i%2 == 1 {
			svc = b
		}
		wg.Add(1)
		go func(svc *Service, i int) {
			defer wg.Done()
			out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: fmt.Sprintf("x-%d", i)})
			mu.Lock()
			defer mu.Unlock()
			switch out.Status {
			case http.StatusOK:
				wins++
			case http.StatusNoContent:
				empty++
			default:
				t.Errorf("claim = %d %v", out.Status, out.Body)
			}
		}(svc, i)
	}
	wg.Wait()
	if wins != 1 || empty != 19 {
		t.Fatalf("wins=%d empty=%d, want exactly one claim", wins, empty)
	}
	got, _ := a.get(rec.ID)
	if got.Status != StatusRunning || got.Execution.Attempts != 1 || got.Execution.LeaseID == "" {
		t.Fatalf("after claim = %s %#v", got.Status, got.Execution)
	}
}

func TestClaimRolesAndRunners(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec := approveOwnerRun(t, svc)
	if out := svc.claim(context.Background(), actuation.RoleExecutor, claimInput{ExecutorID: "mini-50"}); out.Status != http.StatusNoContent {
		t.Fatalf("executor claimed owner work: %d %v", out.Status, out.Body)
	}
	if out := svc.claim(context.Background(), actuation.RoleExecutor, claimInput{ExecutorID: "mini-50", Runners: []string{"owner"}}); out.Status != http.StatusForbidden {
		t.Fatalf("executor asking for owner runner = %d", out.Status)
	}
	if out := svc.claim(context.Background(), actuation.RoleOperator, claimInput{ExecutorID: "x"}); out.Status != http.StatusForbidden {
		t.Fatalf("operator claim = %d", out.Status)
	}
	if out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "owner", ID: fmt.Sprintf("#%d", rec.Number)}); out.Status != http.StatusOK {
		t.Fatalf("admin claim by number = %d %v", out.Status, out.Body)
	}
	if out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "owner", ID: rec.ID}); out.Status != http.StatusConflict {
		t.Fatalf("second claim of the same id = %d", out.Status)
	}
}

func claimed(t *testing.T, svc *Service) (Approval, string) {
	t.Helper()
	rec := approveOwnerRun(t, svc)
	out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "owner-mac", ID: rec.ID})
	if out.Status != http.StatusOK {
		t.Fatalf("claim = %d %v", out.Status, out.Body)
	}
	return rec, out.Body["lease_id"].(string)
}

func TestResultFinishesAndRedacts(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	if out := svc.heartbeat(rec.ID, "lease_wrong"); out.Status != http.StatusConflict {
		t.Fatalf("heartbeat with another lease = %d", out.Status)
	}
	if out := svc.heartbeat(rec.ID, lease); out.Status != http.StatusOK {
		t.Fatalf("heartbeat = %d %v", out.Status, out.Body)
	}
	zero := 0
	big := strings.Repeat("x", 3000) + "\npassword=hunter2 and Authorization: Bearer abcdefghijklmnop\n"
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &zero, OutputSHA256: strings.Repeat("a", 64), OutputTail: big, DurationMS: 1200})
	if out.Status != http.StatusOK || out.Body["status"] != StatusExecuted {
		t.Fatalf("result = %d %v", out.Status, out.Body)
	}
	got, _ := svc.get(rec.ID)
	e := got.Execution
	if len(e.OutputTail) > maxTail || strings.Contains(e.OutputTail, "hunter2") || strings.Contains(e.OutputTail, "abcdefghijklmnop") {
		t.Fatalf("tail %d bytes, unredacted: %q", len(e.OutputTail), e.OutputTail[len(e.OutputTail)-120:])
	}
	if *e.ExitCode != 0 || e.DurationMS != 1200 || e.OutputSHA256 == "" {
		t.Fatalf("execution = %#v", e)
	}
	if again := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &zero}); again.Status != http.StatusConflict {
		t.Fatalf("second result = %d", again.Status)
	}
	if got.public().Execution.LeaseID != "" {
		t.Fatal("the API view shows the lease id")
	}
}

func TestNonZeroExitFails(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	two := 2
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &two})
	if out.Body["status"] != StatusFailed {
		t.Fatalf("exit 2 = %v", out.Body)
	}
	got, _ := svc.get(rec.ID)
	if got.Error != "exit 2" {
		t.Fatalf("error = %q", got.Error)
	}
}

func TestUnknownLateResultAcceptedOnce(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	base := time.Now().UTC()
	svc.SetClock(func() time.Time { return base.Add(leaseFor + unknownGrace + time.Minute) })
	got, _ := svc.get(rec.ID)
	if got.Status != StatusUnknown {
		t.Fatalf("lapsed lease = %s, want unknown", got.Status)
	}
	if retryable(got, base.Add(time.Hour)) {
		t.Fatal("an unknown run is retryable")
	}
	if out := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "again"}); out.Status != http.StatusNoContent {
		t.Fatalf("unknown run was claimed again: %d", out.Status)
	}
	if out := svc.heartbeat(rec.ID, lease); out.Status != http.StatusConflict {
		t.Fatalf("heartbeat after unknown = %d", out.Status)
	}
	zero := 0
	if out := svc.result(rec.ID, resultInput{LeaseID: "lease_other", ExitCode: &zero}); out.Status != http.StatusConflict {
		t.Fatalf("late result with another lease = %d", out.Status)
	}
	out := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &zero})
	if out.Status != http.StatusOK || out.Body["status"] != StatusExecuted || out.Body["late_result"] != true {
		t.Fatalf("late result = %d %v", out.Status, out.Body)
	}
	if again := svc.result(rec.ID, resultInput{LeaseID: lease, ExitCode: &zero}); again.Status != http.StatusConflict {
		t.Fatalf("second late result = %d", again.Status)
	}
}

func TestHostRefusalBeforeStartRequeues(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	rec, lease := claimed(t, svc)
	notStarted := false
	out := svc.result(rec.ID, resultInput{LeaseID: lease, Started: &notStarted, Refusal: "kube API unreachable"})
	if out.Status != http.StatusOK || out.Body["status"] != StatusApproved {
		t.Fatalf("refusal = %d %v", out.Status, out.Body)
	}
	got, _ := svc.get(rec.ID)
	if got.Execution.LastRefusal != "kube API unreachable" || got.Execution.LeaseID != "" || got.Execution.NextAttemptAt.IsZero() {
		t.Fatalf("requeued = %#v", got.Execution)
	}
	if again := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "o"}); again.Status != http.StatusNoContent {
		t.Fatalf("claimed before next_attempt_at: %d", again.Status)
	}
	later := got.Execution.NextAttemptAt.Add(time.Second)
	svc.SetClock(func() time.Time { return later })
	again := svc.claim(context.Background(), actuation.RoleAdmin, claimInput{ExecutorID: "o"})
	if again.Status != http.StatusOK {
		t.Fatalf("claim after backoff = %d %v", again.Status, again.Body)
	}
	if n := again.Body["approval"].(Approval).Execution.Attempts; n != 2 {
		t.Fatalf("attempts = %d, want 2", n)
	}
}

// TD-267 ratchet: a transient refusal leaves the approval approved (the
// Owner's click stands) and it runs on retry; a permanent error still fails.
func TestTransientRefusalKeepsTheApproval(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	calls := 0
	refuse := true
	actions.RegisterExecutor("trigger_cnpg_backup", func(context.Context, map[string]any) (any, error) {
		calls++
		if refuse {
			return nil, actions.Transient("REFUSED: release window held by someone else (who=x what=bifrost-trade-core)")
		}
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "trigger_cnpg_backup", "backup", "", nil)
	out := svc.approve(context.Background(), c.Approval.ID, "chat")
	if out.Status != http.StatusAccepted || out.Body["status"] != StatusApproved || out.Body["last_refusal"] == "" {
		t.Fatalf("approve during a held window = %d %v", out.Status, out.Body)
	}
	if again := svc.approve(context.Background(), c.Approval.ID, "phone"); again.Status != http.StatusConflict {
		t.Fatalf("a second click was possible: %d", again.Status)
	}
	got, _ := svc.get(c.Approval.ID)
	if got.Status != StatusApproved || got.Execution.Attempts != 1 {
		t.Fatalf("stored = %s %#v", got.Status, got.Execution)
	}
	svc.retryDue(context.Background())
	if calls != 1 {
		t.Fatalf("retried before next_attempt_at: calls=%d", calls)
	}
	refuse = false
	next := got.Execution.NextAttemptAt.Add(time.Second)
	svc.SetClock(func() time.Time { return next })
	svc.retryDue(context.Background())
	got, _ = svc.get(c.Approval.ID)
	if got.Status != StatusExecuted || calls != 2 || got.Execution.Attempts != 2 {
		t.Fatalf("after retry = %s calls=%d %#v", got.Status, calls, got.Execution)
	}

	actions.RegisterExecutor("sweep_failed_backups", func(context.Context, map[string]any) (any, error) {
		return nil, errors.New("invalid params")
	})
	p := svc.create(context.Background(), "s", "sweep_failed_backups", "sweep", "", nil)
	perm := svc.approve(context.Background(), p.Approval.ID, "chat")
	if perm.Status != http.StatusOK || perm.Body["status"] != StatusFailed {
		t.Fatalf("permanent error = %d %v", perm.Status, perm.Body)
	}
}

func TestApprovedExpiresAtTheExecutionDeadline(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("trigger_cnpg_backup", func(context.Context, map[string]any) (any, error) {
		return nil, actions.Transient("REFUSED: no release window for x")
	})
	c := svc.create(context.Background(), "s", "trigger_cnpg_backup", "backup", "", nil)
	svc.approve(context.Background(), c.Approval.ID, "chat")
	got, _ := svc.get(c.Approval.ID)
	after := got.Execution.Deadline.Add(time.Second)
	svc.SetClock(func() time.Time { return after })
	got, _ = svc.get(c.Approval.ID)
	if got.Status != StatusExpired || !strings.Contains(got.Error, "no release window") {
		t.Fatalf("past deadline = %s %q", got.Status, got.Error)
	}
}

func TestConfirmNumberForTierD(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	actions.RegisterExecutor("repair_cnpg_wal_store", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	})
	c := svc.create(context.Background(), "s", "repair_cnpg_wal_store", "repair", "", nil)
	wrong := c.Approval.Number + 1
	if out := svc.approveWith(context.Background(), c.Approval.ID, approveInput{Channel: "console", Confirm: &wrong}); out.Status != http.StatusConflict {
		t.Fatalf("wrong confirm_number = %d %v", out.Status, out.Body)
	}
	svc.requireConfirm = true
	if out := svc.approveWith(context.Background(), c.Approval.ID, approveInput{Channel: "phone"}); out.Status != http.StatusBadRequest {
		t.Fatalf("missing confirm_number when required = %d", out.Status)
	}
	right := c.Approval.Number
	if out := svc.approveWith(context.Background(), c.Approval.ID, approveInput{Channel: "phone", Confirm: &right}); out.Body["status"] != StatusExecuted {
		t.Fatalf("right confirm_number = %d %v", out.Status, out.Body)
	}
}

// Owner 2026-10-10 (ADR §5): tier D is approved on Console only. A chat
// approval is refused and leaves the request pending; phone and console are
// unchanged, and chat still approves below tier D.
func TestTierDChatApprovalIsRefused(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	ok := func(context.Context, map[string]any) (any, error) { return map[string]any{"ok": true}, nil }
	actions.RegisterExecutor("repair_cnpg_wal_store", ok)
	actions.RegisterExecutor("trigger_cnpg_backup", ok)

	d := svc.create(context.Background(), "s", "repair_cnpg_wal_store", "repair", "", nil)
	out := svc.approveWith(context.Background(), d.Approval.ID, approveInput{Channel: "chat"})
	if out.Status != http.StatusForbidden {
		t.Fatalf("tier D over chat = %d %v", out.Status, out.Body)
	}
	msg, _ := out.Body["error"].(string)
	want := fmt.Sprintf("#%d", d.Approval.Number)
	if !strings.Contains(msg, "tier D is approved on Console only") || !strings.Contains(msg, want) || !strings.Contains(msg, d.Approval.ID) {
		t.Fatalf("refusal message = %q", msg)
	}
	if out.Body["console_url"] != "http://ops.bifrost.lan/#approvals?id="+d.Approval.ID || out.Body["number"] != d.Approval.Number {
		t.Fatalf("refusal body = %v", out.Body)
	}
	if got, _ := svc.get(d.Approval.ID); got.Status != StatusPending || got.Channel != "" || !got.DecidedAt.IsZero() {
		t.Fatalf("after the refusal = %s channel=%q", got.Status, got.Channel)
	}
	if out := svc.approveWith(context.Background(), d.Approval.ID, approveInput{Channel: "console"}); out.Body["status"] != StatusExecuted {
		t.Fatalf("tier D over console = %d %v", out.Status, out.Body)
	}

	p := svc.create(context.Background(), "s", "repair_cnpg_wal_store", "repair", "", nil)
	if out := svc.approveWith(context.Background(), p.Approval.ID, approveInput{Channel: "phone"}); out.Body["status"] != StatusExecuted {
		t.Fatalf("tier D over phone = %d %v", out.Status, out.Body)
	}

	c := svc.create(context.Background(), "s", "trigger_cnpg_backup", "backup", "", nil)
	if out := svc.approveWith(context.Background(), c.Approval.ID, approveInput{Channel: "chat"}); out.Body["status"] != StatusExecuted {
		t.Fatalf("tier C over chat = %d %v", out.Status, out.Body)
	}

	r := svc.create(context.Background(), "s", "repair_cnpg_wal_store", "repair", "", nil)
	if out := svc.reject(r.Approval.ID, "not now"); out.Status != http.StatusOK || out.Body["status"] != StatusRejected {
		t.Fatalf("tier D reject = %d %v", out.Status, out.Body)
	}
}

func TestListOpenAndNumberLookup(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	pending := svc.create(context.Background(), "s", "cordon_node", "c", "", map[string]any{"name": "n9"})
	rec := approveOwnerRun(t, svc)
	open, _, _ := svc.list("open")
	if len(open) != 2 {
		t.Fatalf("open = %d, want pending + approved", len(open))
	}
	only, _, _ := svc.list("pending")
	if len(only) != 1 || only[0].ID != pending.Approval.ID {
		t.Fatalf("pending = %#v", only)
	}
	for _, ref := range []string{fmt.Sprint(rec.Number), fmt.Sprintf("#%d", rec.Number)} {
		if got, ok := svc.get(ref); !ok || got.ID != rec.ID {
			t.Fatalf("get(%q) = %v %v", ref, got.ID, ok)
		}
	}
	if _, _, body := svc.list("bogus"); body == nil {
		t.Fatal("unknown status filter accepted")
	}
}

func TestRunnerParamAndWorkLabels(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	res := svc.createWith(context.Background(), createInput{
		Requester: "s", Action: "owner_run_command", Reason: "r",
		Params: map[string]any{"command": "true", "reason": "r", "runner": "system"},
		Thread: "W-48 S0-0a", WorkID: "W-48",
	})
	if res.Status != http.StatusCreated || res.Approval.Runner != actions.RunnerHost || res.Approval.Params["runner"] != "host" {
		t.Fatalf("runner system = %d %#v", res.Status, res.Approval)
	}
	if res.Approval.WorkID != "W-48" || res.Approval.RequesterThread != "W-48 S0-0a" {
		t.Fatalf("labels = %#v", res.Approval)
	}
	bad := svc.createWith(context.Background(), createInput{Requester: "s", Action: "cordon_node", Reason: "r", Params: map[string]any{"name": "n"}, WorkID: "anything"})
	if bad.Status != http.StatusBadRequest {
		t.Fatalf("bad work_id = %d", bad.Status)
	}
	badRunner := svc.create(context.Background(), "s", "owner_run_command", "r", "", map[string]any{"command": "true", "reason": "r", "runner": "nobody"})
	if badRunner.Status != http.StatusBadRequest {
		t.Fatalf("bad runner = %d", badRunner.Status)
	}
}

func TestRedactBackstop(t *testing.T) {
	in := "token: abc123def\nDB_PASSWORD=s3cr3t\npostgres://app:pw@db:5432/x\nghp_0123456789abcdefghijklmnop\nok line"
	out := Redact(in)
	for _, leak := range []string{"abc123def", "s3cr3t", ":pw@", "ghp_0123456789"} {
		if strings.Contains(out, leak) {
			t.Fatalf("redacted output still has %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "ok line") {
		t.Fatal("redaction removed an ordinary line")
	}
}

// The local file backend (bdev platform-api) serializes with flock.
func TestConcurrentCreatesOnTheFileBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals")
	a, b := New(path, nil), New(path, nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int]bool{}
	for i := 0; i < 20; i++ {
		svc := a
		if i%2 == 1 {
			svc = b
		}
		wg.Add(1)
		go func(svc *Service, i int) {
			defer wg.Done()
			res := svc.create(context.Background(), "s", "cordon_node", "c", "", map[string]any{"name": fmt.Sprintf("f-%d", i)})
			mu.Lock()
			defer mu.Unlock()
			if res.Status != http.StatusCreated || seen[res.Approval.Number] {
				t.Errorf("create %d: status %d number %d", i, res.Status, res.Approval.Number)
			}
			seen[res.Approval.Number] = true
		}(svc, i)
	}
	wg.Wait()
	if list, _, _ := a.list("all"); len(list) != 20 || len(seen) != 20 {
		t.Fatalf("stored %d, numbers %d, want 20", len(list), len(seen))
	}
}

func TestExecutorRoleRoutes(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.yaml")
	if err := os.WriteFile(authPath, []byte(`
tokens:
  - {name: viewer, role: viewer, token: viewer-test-token}
  - {name: operator, role: operator, token: operator-test-token}
  - {name: admin, role: admin, token: admin-test-token}
  - {name: mini-50, role: executor, token: executor-test-token}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := actuation.LoadAuth(authPath)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(filepath.Join(dir, "approvals"), actuation.NewAuditLog(filepath.Join(dir, "audit.json")))
	r := chi.NewRouter()
	Mount(r, auth, svc)
	do := func(method, path, body, token string) *httptest.ResponseRecorder {
		var rdr *strings.Reader
		if body == "" {
			rdr = strings.NewReader("")
		} else {
			rdr = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rdr)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	created := do(http.MethodPost, "/approvals", `{"action":"owner_run_command","params":{"command":"true","reason":"probe","runner":"host"},"reason":"probe","work_id":"W-48"}`, "operator-test-token")
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"number":1`) || !strings.Contains(created.Body.String(), `"runner":"host"`) {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/approvals", ""},
		{http.MethodGet, "/approvals/1", ""},
		{http.MethodPost, "/approvals", `{"action":"cordon_node","params":{"name":"n"},"reason":"r"}`},
		{http.MethodPost, "/approvals/1/approve", `{"channel":"chat"}`},
		{http.MethodPost, "/approvals/1/reject", `{"reason":"r"}`},
	} {
		if rec := do(c.method, c.path, c.body, "executor-test-token"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("executor %s %s = %d", c.method, c.path, rec.Code)
		}
	}
	for _, tok := range []string{"viewer-test-token", "operator-test-token"} {
		if rec := do(http.MethodPost, "/approvals/claim", `{"executor_id":"x"}`, tok); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s claim = %d", tok, rec.Code)
		}
	}
	approved := do(http.MethodPost, "/approvals/%231/approve", `{"channel":"console","confirm_number":"#1"}`, "admin-test-token")
	if approved.Code != http.StatusAccepted || !strings.Contains(approved.Body.String(), `"status":"approved"`) {
		t.Fatalf("approve #1 = %d %s", approved.Code, approved.Body.String())
	}
	claim := do(http.MethodPost, "/approvals/claim", `{"executor_id":"mini-50","runners":["host"]}`, "executor-test-token")
	if claim.Code != http.StatusOK || !strings.Contains(claim.Body.String(), `"lease_id":"lease_`) {
		t.Fatalf("executor claim = %d %s", claim.Code, claim.Body.String())
	}
	var lease struct {
		LeaseID string `json:"lease_id"`
	}
	if err := json.Unmarshal(claim.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if rec := do(http.MethodPost, "/approvals/claim", `{"executor_id":"mini-50"}`, "executor-test-token"); rec.Code != http.StatusNoContent {
		t.Fatalf("empty claim = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/approvals/1/heartbeat", `{"lease_id":"`+lease.LeaseID+`"}`, "executor-test-token"); rec.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d %s", rec.Code, rec.Body.String())
	}
	view := do(http.MethodGet, "/approvals/1", "", "viewer-test-token")
	if view.Code != http.StatusOK || strings.Contains(view.Body.String(), lease.LeaseID) || !strings.Contains(view.Body.String(), `"status":"running"`) {
		t.Fatalf("viewer get = %d %s", view.Code, view.Body.String())
	}
	result := do(http.MethodPost, "/approvals/1/result", `{"lease_id":"`+lease.LeaseID+`","exit_code":0,"duration_ms":5,"output_sha256":"`+strings.Repeat("b", 64)+`"}`, "executor-test-token")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"status":"executed"`) {
		t.Fatalf("result = %d %s", result.Code, result.Body.String())
	}
	open := do(http.MethodGet, "/approvals?status=open", "", "viewer-test-token")
	if !strings.Contains(open.Body.String(), `"approvals":[]`) {
		t.Fatalf("open after execution = %s", open.Body.String())
	}
	audit, _ := os.ReadFile(filepath.Join(dir, "audit.json"))
	for _, want := range []string{"approval.queue", "approval.claim", "approval.run.finish", "executor=mini-50"} {
		if !strings.Contains(string(audit), want) {
			t.Fatalf("audit missing %s", want)
		}
	}
}
