package agentd_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-409 (STG M-93, 2026-10-20: an implementation of effect none slept 2700
// s and said so in its report; two reviews looked for that line in an empty
// checkout and failed it): end to end on the platform, the review of an
// implementation of effect none is told what the attempt reported, after
// the task's spec, and that the checkout holds none of its work.
func TestAReviewOfEffectNoneSeesTheReport(t *testing.T) {
	spec := `{"text":"# Sleep 2700 s","verify":[["true"]]}`
	pool, task, base := platformSpec(t, store.Task{Effect: "none"}, nil, "application/json", []byte(spec))
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: verifier and reviewer"}}); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('verifier', 'dev', 'job:test', 1), ('reviewer', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version) VALUES
			 ('verifier/exec:deterministic', 'verifier', 'exec:deterministic', 'local', 'active', 'job:test', 1),
			 ('reviewer/exec:codex', 'reviewer', 'exec:codex', 'openai', 'active', 'job:test', 1)`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	d := daemon(t, base, script(`printf '# done\n\nslept 2700 s\n' > "$OUT/report.md"`), "", nil)
	prod := agentd.Executors(os.Args[0], append(os.Environ(), "TESTMAIN_AGENTD_LAUNCHER=1"), agentd.Secrets{})["exec:deterministic"]
	d.Executors["exec:deterministic"] = func(a *agentd.Attempt) (agentd.Spawn, error) {
		sp, err := prod(a)
		sp.Env = append(sp.Env, "TESTMAIN_AGENTD_LAUNCHER=1")
		return sp, err
	}
	var prompt string
	d.Executors["exec:codex"] = func(a *agentd.Attempt) (agentd.Spawn, error) {
		prompt = agentd.Prompt(a)
		return script(`printf '# reviewed\n\nVERDICT: PASS\n' > "$OUT/report.md"`)(a)
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
	run(cursor, "implement")
	check, err := store.StartVerification(ctx, pool, task)
	must(t, err)
	run(agentd.Tool{Executor: "exec:deterministic", Provider: "local"}, "verify")
	if did, cerr := store.Conclude(ctx, pool, check); cerr != nil || did != "review" {
		t.Fatalf("conclude: %q %v", did, cerr)
	}
	rc := run(agentd.Tool{Executor: "exec:codex", Provider: "openai"}, "review")
	if rc.SubjectReport == "" || rc.SubjectEffect != "none" {
		t.Fatalf("the review's claim: %+v", rc)
	}
	for _, want := range []string{"# Sleep 2700 s", "handed in:\n\n```\n# done\n\nslept 2700 s\n```\n",
		"That task changes no files (effect none): this checkout holds none of its work. Judge it by the report above"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the review's prompt lacks %q:\n%s", want, prompt)
		}
	}
}
