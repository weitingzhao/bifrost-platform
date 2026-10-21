package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
)

// A gate task's result (gates.go; CUT §3): ConcludeWith hands a submitted
// verify task with no subject here. Only a member of its batch is used. The
// gate passes when its attempt concluded done and its verify.json says
// pass, with the very commands of the gate's spec, in order, each exiting 0
// and none timed out or withheld; anything else is a failure. The result is
// a gate verification of the task's own attempt (verifier NULL: an attempt
// does not verify itself), bound to the batch's environment, commit,
// deployment and spec, which GatePassed reads; a failure also leaves a note
// with the runner's verdict line, which the architecture table quotes. The
// task is then accepted: its gate ran. An attempt handed in with no report
// records nothing (there is no evidence) and its task is cancelled. That the
// report is this attempt's own run rests on agentd, which hands in what the
// run wrote (review td397-4 C2): verify.json names no attempt, commit or
// spec, and the lease's fence binds the hand-over to the attempt, not the
// bytes to a run.

// verifyReport is verify.json as agentd's deterministic executor writes it
// (agentd/spec.go Verification).
type verifyReport struct {
	Pass     bool `json:"pass"`
	Commands []struct {
		Argv     []string `json:"argv"`
		ExitCode *int     `json:"exit_code"` // absent or null is no exit (review td397-4 A2)
		TimedOut bool     `json:"timed_out"`
		Tail     string   `json:"tail"`
	} `json:"commands"`
	Reason string `json:"reason"`
}

func concludeGate(ctx context.Context, tx pgx.Tx, taskID string) (string, error) {
	va, conclusion, report, _, err := submittedAttempt(ctx, tx, taskID, "FOR UPDATE")
	if errors.Is(err, errNoSubmitted) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	t, err := readTask(ctx, tx, taskID, true)
	if err != nil || t.state != "submitted" {
		return "", err
	}
	b, ok, err := readGateBatch(ctx, tx, t.mission, "")
	if err != nil || !ok {
		return "", err
	}
	e, member, err := gateMemberOf(ctx, tx, b, taskID)
	if err != nil || !member {
		return "", err
	}
	if report == "" {
		if err = moveTask(ctx, tx, "controller", t, "blocked", "the gate could not run: its attempt handed in no report"); err != nil {
			return "", err
		}
		t.state = "blocked"
		return "cancelled", moveTask(ctx, tx, "controller", t, "cancelled", "the gate could not run: its attempt handed in no report")
	}
	verdict, why, err := gateVerdict(ctx, tx, e.Spec, report, conclusion)
	if err != nil {
		return "", err
	}
	refDoc, err := canon.Encode(b.run.Ref)
	if err != nil {
		return "", err
	}
	vid := NewVerificationID()
	if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "verification_recorded", Actor: "controller",
		MissionID: t.mission, TaskID: t.id, AttemptID: va,
		Body: map[string]any{"verification_id": vid, "method": "gate", "verdict": verdict, "check_ref": e.Test, "env": b.run.Env}}); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, verdict, evidence_artifact_id,
	  subject_commit, accepts_attempt, check_ref, env, deploy_ref_json, spec_sha256)
	  VALUES ($1, $2, $3, 'gate', $4, $5, $6, false, $7, $8, $9::jsonb, $10)`,
		vid, t.id, va, verdict, report, b.run.Commit, e.Test, b.run.Env, string(refDoc), e.Spec); err != nil {
		return "", fmt.Errorf("store: record gate verification: %w", err)
	}
	if verdict != "pass" {
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "note", Actor: "controller", MissionID: t.mission,
			TaskID: t.id, Body: map[string]any{"text": GateFailNote(e.Test, b.run.Env, b.run.Commit, why)}}); err != nil {
			return "", err
		}
	}
	return "accepted", moveTask(ctx, tx, "controller", t, "accepted", "gate result used: "+verdict)
}

// gateFailPrefix starts the note a failed gate leaves; the architecture
// table reads the rest of it.
const gateFailPrefix = "gate failed: "

// GateFailNote is the note a failed gate leaves on its task.
func GateFailNote(test, env, commit, why string) string {
	return fmt.Sprintf("%s%s on %s at %s: %s", gateFailPrefix, test, env, short(commit), oneLineRunes(why, 300))
}

// gateVerdict is the gate's verdict from its attempt's report, and why when
// it is not a pass: the runner's verdict line when the report has one.
func gateVerdict(ctx context.Context, tx pgx.Tx, spec, report, conclusion string) (string, string, error) {
	var specBody, body []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, "sha256:"+spec).Scan(&specBody); err != nil {
		return "", "", err
	}
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, report).Scan(&body); err != nil {
		return "", "", err
	}
	cmds, err := gaterun.ParseSpec(specBody)
	if err != nil {
		return "fail", "its spec is not a gate spec: " + err.Error(), nil
	}
	var v verifyReport
	if err = json.Unmarshal(body, &v); err != nil {
		return "fail", "its verify.json does not read: " + err.Error(), nil
	}
	line := ""
	for _, c := range v.Commands {
		if l := verdictLine(c.Tail); l != "" {
			line = l
		}
	}
	fail := func(why string) (string, string, error) {
		if line != "" && !strings.Contains(line, " PASS ") {
			why = line
		}
		return "fail", why, nil
	}
	if len(v.Commands) != len(cmds) {
		return fail(fmt.Sprintf("it ran %d commands, its spec has %d", len(v.Commands), len(cmds)))
	}
	for i, c := range v.Commands {
		switch {
		case !slices.Equal(c.Argv, cmds[i].Argv()):
			return fail(fmt.Sprintf("command %d is not its spec's (or its trace was withheld)", i+1))
		case c.TimedOut:
			return fail(fmt.Sprintf("command %d timed out", i+1))
		case c.ExitCode == nil:
			return fail(fmt.Sprintf("command %d has no exit code", i+1))
		case *c.ExitCode != 0:
			return fail(fmt.Sprintf("command %d exited %d", i+1, *c.ExitCode))
		}
	}
	if !v.Pass || conclusion != "done" {
		return fail("its check did not pass: " + oneLineRunes(v.Reason, 200))
	}
	return "pass", "", nil
}

// verdictLine is the runner's last verdict line in an output's tail, or "".
func verdictLine(tail string) string {
	lines := strings.Split(strings.TrimRight(tail, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], gaterun.VerdictPrefix) {
			return strings.TrimPrefix(lines[i], gaterun.VerdictPrefix)
		}
	}
	return ""
}
