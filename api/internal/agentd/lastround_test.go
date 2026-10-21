package agentd_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-405 (PROD M-23: codex handed in the same compile error three rounds
// running): an implementation whose first round failed its deterministic
// check is told, in round 2, what failed: the claim names the check, agentd
// reads its verify.json, and the prompt has the failed command and the line
// of its output after the spec, before the hand-in rules. Round 1 is told
// nothing of the kind.
func TestRetriedImplementationIsToldWhatFailed(t *testing.T) {
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	const clash = "internal/agentd/tmp_writes_test.go:9:2: token redeclared in this block"
	spec := `{"text":"# Test the writes","verify":[["sh","-c","echo ok; echo '` + clash + `' >&2; exit 1"]]}`
	pool, task, base := platformSpec(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head,
		"application/json", []byte(spec))
	// The verifier job, agentd's deterministic executor its candidate.
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: verifier"}}); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('verifier', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
			 VALUES ('verifier/exec:deterministic', 'verifier', 'exec:deterministic', 'local', 'active', 'job:test', 1)`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	var prompts []string
	impl := func(a *agentd.Attempt) (agentd.Spawn, error) {
		prompts = append(prompts, agentd.Prompt(a))
		return script(`echo 'package x' > x.go && printf '# done\n' > "$OUT/report.md"`)(a)
	}
	d := daemon(t, base, impl, bare, env)
	launcher := append(os.Environ(), "TESTMAIN_AGENTD_LAUNCHER=1")
	prod := agentd.Executors(os.Args[0], launcher, agentd.Secrets{})["exec:deterministic"]
	d.Executors["exec:deterministic"] = func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := prod(a)
		sp.Env = append(sp.Env, "TESTMAIN_AGENTD_LAUNCHER=1")
		return sp, err
	}
	run := func(tool agentd.Tool, role string) *agentd.Claimed {
		t.Helper()
		cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{tool})
		if err != nil || cl == nil || cl.Role != role {
			t.Fatalf("claim of the %s: %+v %v", role, cl, err)
		}
		if out, rerr := d.Run(ctx, cl); rerr != nil || !out.Submitted {
			t.Fatalf("run of the %s: %+v %v", role, out, rerr)
		}
		return cl
	}

	if first := run(cursor, "implement"); len(first.FailedChecks) != 0 {
		t.Fatalf("round 1's claim names %+v", first.FailedChecks)
	}
	check, err := store.StartVerification(ctx, pool, task)
	must(t, err)
	if cl := run(agentd.Tool{Executor: "exec:deterministic", Provider: "local"}, "verify"); cl.TaskID != check || len(cl.FailedChecks) != 0 {
		t.Fatalf("the check's claim: %+v", cl)
	}
	if did, cerr := store.Conclude(ctx, pool, check); cerr != nil || did != "rework" {
		t.Fatalf("conclude: %q %v", did, cerr)
	}
	second := run(cursor, "implement")
	if f := second.FailedChecks; second.AttemptNo != 2 || len(f) != 1 || f[0].Method != "deterministic" || f[0].AttemptNo != 1 || f[0].Artifact == "" {
		t.Fatalf("round 2's claim names %+v", second.FailedChecks)
	}

	if len(prompts) != 2 || strings.Contains(prompts[0], "last round") {
		t.Fatalf("%d prompts; round 1's:\n%s", len(prompts), prompts[0])
	}
	p := prompts[1]
	for _, want := range []string{"# Test the writes\n\n---\nThe last round failed its check; fix that first.",
		"The deterministic check of attempt 1 failed. First failed command: sh -c echo ok; echo '" + clash + "' >&2; exit 1 exited 1. Its output ends:",
		"\n```\nok\n" + clash + "\n```\n"} {
		if !strings.Contains(p, want) {
			t.Fatalf("round 2's prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Index(p, clash) > strings.Index(p, "Work only in this directory") {
		t.Fatalf("what failed is not between the spec and the hand-in rules:\n%s", p)
	}
}
