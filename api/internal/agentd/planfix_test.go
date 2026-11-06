package agentd_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const (
	planNoBlock = "# Plan\n\nOne task, in the platform.\n"
	planBlock   = "```json\n" + `{"tasks": [{"ref": "t1", "title": "One", "repo": "bifrost-platform", "text": "Do it."}]}` + "\n```\n"
)

// followed is the script executor whose spawn can go on in its session: the
// follow-up runs reply with PROMPT set to what it is asked, and the number
// of follow-ups goes into asked.
func followed(body, reply string, asked *atomic.Int32) agentd.Executor {
	return func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := script(body)(a)
		sp.FollowUp = func(prompt string) (agentd.Spawn, error) {
			asked.Add(1)
			return agentd.Spawn{Argv: []string{"sh", "-c", reply}, Dir: a.Dir,
				Env: append(os.Environ(), "OUT="+a.Out, "PROMPT="+prompt, "REPLY="+filepath.Join(a.Out, agentd.PlanReply),
					"TESTMAIN_AGENTD_LAUNCHER=1")}, nil
		}
		return sp, err
	}
}

// planRun runs one attempt of a task of role with the executor; it returns
// the outcome, the attempt's output directory and the task.
func planRun(t *testing.T, role string, ex agentd.Executor, during func(pool *pgxpool.Pool, task, out string)) (agentd.Outcome, string, string) {
	t.Helper()
	pool, task, base := platformWith(t, store.Task{Effect: "none", Role: role}, nil)
	d := daemon(t, base, ex, "", nil)
	d.KeepWorkspaces = true // the test reads what the attempt left after its end
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil || cl.Role != role {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	outDir := agentd.WorkspaceDir(d.Root, cl.TaskID, cl.AttemptNo) + ".out"
	if during != nil {
		go during(pool, task, outDir)
	}
	out, err := d.Run(ctx, cl)
	if err != nil {
		t.Fatalf("run = %+v %v", out, err)
	}
	var attempts int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE task_id = $1`, task).Scan(&attempts))
	if attempts != 1 {
		t.Fatalf("%d attempts", attempts)
	}
	return out, outDir, task
}

func readOut(t *testing.T, out, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, name))
	must(t, err)
	return string(b)
}

// A plan whose report has no block, and whose session appends one when
// asked, hands in one report ending in the block, in the same attempt.
func TestPlanFollowUpAppendsTheBlock(t *testing.T) {
	var asked atomic.Int32
	ex := followed(`printf '`+planNoBlock+`' > "$OUT/report.md"`,
		`printf '%s' "$PROMPT" > "$OUT/prompt.txt"; cat > "$REPLY" <<'EOF'
`+planBlock+`EOF`, &asked)
	out, outDir, _ := planRun(t, "plan", ex, nil)
	if out.EndHow != "exit" || out.Conclusion != "done" || !out.Submitted || asked.Load() != 1 {
		t.Fatalf("run = %+v, asked %d", out, asked.Load())
	}
	report := readOut(t, outDir, "report.md")
	if report != planNoBlock+"\n\n"+planBlock || store.PlanBlock([]byte(report)) != nil {
		t.Fatalf("report.md:\n%s", report)
	}
	if p := readOut(t, outDir, "prompt.txt"); !strings.Contains(p, "the report has no ```json block") || !strings.Contains(p, "only the fenced ```json block") {
		t.Fatalf("the follow-up was asked:\n%s", p)
	}
	if log := readOut(t, outDir, "log.txt"); !strings.Contains(log, "asked once for it: fixed") {
		t.Fatalf("log.txt:\n%s", log)
	}
}

// A follow-up whose reply still has no block leaves the first report.
func TestPlanFollowUpWithoutABlock(t *testing.T) {
	var asked atomic.Int32
	ex := followed(`printf '`+planNoBlock+`' > "$OUT/report.md"`, `echo 'Sorry, here it is: {"tasks": []}' > "$REPLY"`, &asked)
	out, outDir, _ := planRun(t, "plan", ex, nil)
	if out.EndHow != "exit" || out.Conclusion != "done" || asked.Load() != 1 {
		t.Fatalf("run = %+v, asked %d", out, asked.Load())
	}
	if report := readOut(t, outDir, "report.md"); report != planNoBlock {
		t.Fatalf("report.md:\n%s", report)
	}
	if log := readOut(t, outDir, "log.txt"); !strings.Contains(log, "asked once for it: not fixed") {
		t.Fatalf("log.txt:\n%s", log)
	}
}

// No follow-up for a report whose block reads, for an implementation, for a
// failed exit, nor for an executor that cannot go on in its session.
func TestPlanFollowUpNotAsked(t *testing.T) {
	for name, c := range map[string]struct {
		role, body  string
		nilFollowUp bool
		conclusion  string
	}{
		"a block that reads": {role: "plan", body: `printf '` + planNoBlock + `\n` + planBlock + `' > "$OUT/report.md"`, conclusion: "done"},
		"an implementation":  {role: "implement", body: `printf '` + planNoBlock + `' > "$OUT/report.md"`, conclusion: "done"},
		"a failed exit":      {role: "plan", body: `printf '` + planNoBlock + `' > "$OUT/report.md"; exit 3`, conclusion: "failed"},
		"no follow-up":       {role: "plan", body: `printf '` + planNoBlock + `' > "$OUT/report.md"`, nilFollowUp: true, conclusion: "done"},
	} {
		t.Run(name, func(t *testing.T) {
			var asked atomic.Int32
			ex := followed(c.body, `echo asked > "$OUT/asked"`, &asked)
			if c.nilFollowUp {
				ex = script(c.body)
			}
			out, outDir, _ := planRun(t, c.role, ex, nil)
			if out.Conclusion != c.conclusion || asked.Load() != 0 {
				t.Fatalf("run = %+v, asked %d", out, asked.Load())
			}
			if _, err := os.Stat(filepath.Join(outDir, "asked")); err == nil {
				t.Fatal("a follow-up ran")
			}
			if log := readOut(t, outDir, "log.txt"); strings.Contains(log, "asked once") {
				t.Fatalf("log.txt:\n%s", log)
			}
		})
	}
}

// A stop that comes during the follow-up ends the attempt cancelled, as it
// would the first run, and the report is the first.
func TestPlanFollowUpStopped(t *testing.T) {
	var asked atomic.Int32
	ex := followed(`printf '`+planNoBlock+`' > "$OUT/report.md"`, `touch "$OUT/following"; exec sleep 30`, &asked)
	done := make(chan struct{})
	start := time.Now()
	out, outDir, _ := planRun(t, "plan", ex, func(pool *pgxpool.Pool, task, outDir string) {
		defer close(done)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(filepath.Join(outDir, "following")); err == nil {
				break
			}
		}
		if err := store.InTx(ctx, pool, func(tx pgx.Tx) error {
			return store.SetControl(ctx, tx, "owner", store.NewControlID(), "task", task, "stop", "a stop during the follow-up")
		}); err != nil {
			t.Error(err)
		}
	})
	<-done
	if out.EndHow != "cancelled" || out.Conclusion != "none" || asked.Load() != 1 || time.Since(start) > 20*time.Second {
		t.Fatalf("run = %+v, asked %d, after %s", out, asked.Load(), time.Since(start))
	}
	if report := readOut(t, outDir, "report.md"); report != planNoBlock {
		t.Fatalf("report.md:\n%s", report)
	}
}
