package agentd_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// prepareRun is specRun with the platform's database and the claim.
func prepareRun(t *testing.T, spec, body string) (*pgxpool.Pool, *agentd.Claimed, agentd.Outcome, error) {
	t.Helper()
	env := gitEnvT(t.TempDir())
	bare, baseCommit := bareRemote(t, env)
	head := func(_ context.Context, _, ref string) (string, error) {
		out, err := exec.Command("git", "ls-remote", bare, ref).Output()
		first, _, _ := strings.Cut(string(out), "\t")
		return strings.TrimSpace(first), err
	}
	pool, _, base := platformSpec(t, store.Task{Effect: "branch", Repo: "bifrost-platform", BaseCommit: baseCommit}, head, "application/json", []byte(spec))
	d := daemon(t, base, script(body), bare, env)
	cl, err := d.Client.Claim(ctx, 1, []agentd.Tool{cursor})
	if err != nil || cl == nil {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	out, err := d.Run(ctx, cl)
	return pool, cl, out, err
}

// handedIn is what the attempt's checkpoints named, by artifact kind: each
// body, in the order they came.
func handedIn(t *testing.T, pool *pgxpool.Pool, attempt string) map[string][]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT a.artifact_kind, convert_from(b.body, 'UTF8') FROM agentrt.event e
	  CROSS JOIN LATERAL jsonb_array_elements_text(e.body_json->'artifacts') AS id(artifact_id)
	  JOIN agentrt.artifact a ON a.artifact_id = id.artifact_id JOIN agentrt.artifact_blob b ON b.artifact_id = a.artifact_id
	  WHERE e.attempt_id = $1 AND e.event_type = 'attempt_checkpointed' ORDER BY e.event_id`, attempt)
	must(t, err)
	defer rows.Close()
	got := map[string][]string{}
	for rows.Next() {
		var kind, body string
		must(t, rows.Scan(&kind, &body))
		got[kind] = append(got[kind], body)
	}
	must(t, rows.Err())
	return got
}

// TD-394 (PROD M-22: nine attempts ended in their setup, the cause only in
// agentd's journal on 06): an attempt whose setup fails hands in, as its
// last checkpoint, a report naming the failed command, its exit code and the
// end of its output, and the log's tail; it still ends failed with exit 3
// and the executor never runs.
func TestAFailedSetupHandsInWhatFailed(t *testing.T) {
	const line = "npm ERR! code E403 403 Forbidden - GET https://registry.npmjs.org/left-pad"
	spec := `{"text":"t","setup":[["sh","-c","echo installed"],["sh","-c","echo '` + line + `' >&2; exit 9"]]}`
	pool, cl, out, err := prepareRun(t, spec, `touch "$OUT/executor-ran"; printf '# done\n' > "$OUT/report.md"`)
	if err != nil || out.Conclusion != "failed" || out.ExitCode != 3 {
		t.Fatalf("run = %+v %v", out, err)
	}
	got := handedIn(t, pool, cl.AttemptID)
	if len(got["report"]) != 1 || len(got["log_tail"]) != 1 {
		t.Fatalf("handed in %v", got)
	}
	report := got["report"][0]
	for _, want := range []string{"# The attempt failed before its executor started", "The task's setup failed, so the executor did not start.",
		"First failed command: sh -c echo '" + line + "' >&2; exit 9 exited 9. Its output ends:", "```\n" + line + "\n```"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "echo installed") {
		t.Fatalf("the report names a command that passed:\n%s", report)
	}
	if !strings.Contains(got["log_tail"][0], line) || !strings.Contains(got["log_tail"][0], "the executor did not start") {
		t.Fatalf("the log tail:\n%s", got["log_tail"][0])
	}
}

// An attempt whose executor fails on its own hands in no such report: its
// own report and log go as they did.
func TestAnExecutorFailureIsNotAPreparationFailure(t *testing.T) {
	pool, cl, out, err := prepareRun(t, `{"text":"t","setup":[["sh","-c","echo installed"]]}`, `echo 'package x' > x.go; exit 3`)
	if err != nil || out.Conclusion != "failed" || out.ExitCode != 3 {
		t.Fatalf("run = %+v %v", out, err)
	}
	for _, body := range handedIn(t, pool, cl.AttemptID)["report"] {
		if strings.Contains(body, "before its executor started") {
			t.Fatalf("an executor's own exit 3 was handed in as its setup's failure:\n%s", body)
		}
	}
}

// A sibling that cannot be had ends the attempt before its executor is
// launched: the report says why, as agentd's error does.
func TestASiblingThatCannotBeHadHandsInWhy(t *testing.T) {
	pool, cl, _, err := prepareRun(t, `{"text":"t","siblings":[{"repo":"ui","ref":"no-such-branch"}]}`, `touch "$OUT/executor-ran"`)
	if err == nil || !strings.Contains(err.Error(), "sibling ui") {
		t.Fatalf("run err = %v", err)
	}
	got := handedIn(t, pool, cl.AttemptID)
	if len(got["report"]) != 1 {
		t.Fatalf("handed in %v", got)
	}
	for _, want := range []string{"# The attempt failed before its executor started", "agentd could not prepare the attempt", "sibling ui at no-such-branch"} {
		if !strings.Contains(got["report"][0], want) {
			t.Fatalf("the report lacks %q:\n%s", want, got["report"][0])
		}
	}
}
