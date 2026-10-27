package store_test

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Seed real platform verification provenance, including an older attempt and
// an independent reviewer. Adopt version 2 in the existing wire format.
func pvVerificationWorld(t *testing.T, method, initial string) (*world, planview.Binding) {
	t.Helper()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{BaseCommit: commitB})
	if got := w.failRound(t, task, "h1", "h2"); got != "rework" {
		t.Fatalf("first attempt: %s", got)
	}
	w.passRound(t, task, "h3", "h4", "h5")
	d := pvDocument(t, 2)
	atom := "review_valid"
	if method == "deterministic" {
		atom = "external_verified"
	}
	b := pvBinding(d, s1, atom, "verification")
	b.Task, b.Mission, b.Repo, b.Result = task, "M-1", "bifrost-platform", commitA
	ok(t, w.rt.QueryRow(ctx, `SELECT a.attempt_id,t.round_no,v.verification_id
 FROM agentrt.task t JOIN agentrt.attempt a USING(task_id) JOIN agentrt.verification v ON v.task_id=t.task_id AND v.attempt_id=a.attempt_id
 WHERE t.task_id=$1 AND v.method=$2 ORDER BY a.attempt_no DESC,v.created_at DESC,v.verification_id DESC LIMIT 1`, task, method).Scan(&b.Attempt, &b.Round, &b.Verification))
	if initial != "pass" {
		b.Verification = pvReverify(t, w, b, method, initial)
	}
	if method == "deterministic" {
		who := wr("thread:external")
		reg, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "latest-verification", Root: root, Node: s1, RunKey: "work636", Artifact: externalSpec(t, w), Mission: b.Mission, Task: task})
		ok(t, err)
		b.ExternalWork = reg["external_work_id"].(string)
		b.Report = w.artifact(t, "report", "latest verification result")
		_, err = store.PostExternalEvent(ctx, w.rt, who, b.ExternalWork, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
		ok(t, err)
		_, err = store.PostExternalEvent(ctx, w.rt, who, b.ExternalWork, store.ExternalEvent{Key: "submit", Seq: 2, Type: "submitted", ReportDigest: b.Report, Commits: []string{b.Result}})
		ok(t, err)
		b.ExternalSeq = 2
	}
	adoptView(t, w, viewDoc(t, w, 1), 0)
	adoptView(t, w, pvArtifact(t, w, d, b), 1)
	return w, b
}

// Each later verdict has its own evidence and an explicit later database time;
// no sleeps or wall-clock ordering assumptions are needed.
func pvReverify(t *testing.T, w *world, b planview.Binding, method, verdict string, reviewer ...string) string {
	t.Helper()
	id := store.NewVerificationID()
	override := ""
	if len(reviewer) > 0 {
		override = reviewer[0]
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		evidence, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte(id+":"+verdict))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification
 (verification_id,task_id,attempt_id,method,verifier_attempt_id,verdict,evidence_artifact_id,subject_commit,created_at)
 SELECT $1,$2,$3,$4,COALESCE(NULLIF($9,''),verifier_attempt_id),$5,$6,$7,(SELECT max(created_at)+interval '1 second' FROM agentrt.verification)
 FROM agentrt.verification WHERE verification_id=$8`, id, b.Task, b.Attempt, method, verdict, evidence, b.Result, b.Verification, override)
		return err
	}))
	return id
}

func pvAssertVerification(t *testing.T, w *world, b planview.Binding, value, evidenceID string) {
	t.Helper()
	pvPoll(t, w, store.PlanSources{})
	var observed struct {
		Value    string
		Evidence []string
	}
	var raw []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation' FROM agentrt.event
 WHERE event_stream_id=$1 AND body_json->'plan_observation'->>'binding_sha256'=$2 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+b.Root, b.Digest()).Scan(&raw))
	ok(t, json.Unmarshal(raw, &observed))
	if observed.Value != value || len(observed.Evidence) != 2 || observed.Evidence[0] != evidenceID {
		t.Fatalf("reading = %s; want %s from %s", raw, value, evidenceID)
	}
	var evidence string
	ok(t, w.rt.QueryRow(ctx, `SELECT evidence_artifact_id FROM agentrt.verification WHERE verification_id=$1`, evidenceID).Scan(&evidence))
	if observed.Evidence[1] != evidence {
		t.Fatalf("stale verification evidence: %v; want %s", observed.Evidence, evidence)
	}
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	for _, n := range view["nodes"].([]map[string]any) {
		if n["plan_node_id"] == item && n["current_validity"] != value {
			t.Fatalf("view validity = %v; want %s", n["current_validity"], value)
		}
	}
	if view["plan_version"] != 2 || w.count(t, `SELECT count(*) FROM agentrt.event WHERE body_json ? 'plan_view'`) != 2 {
		t.Fatal("re-verification required re-adoption")
	}
	items := view["items"].([]planview.Item)
	adopted, err := planview.ParseBinding(items[0].Bindings[0])
	ok(t, err)
	if adopted.Digest() != b.Digest() {
		t.Fatal("reading changed the adopted binding identity")
	}
}

func TestPV2VerificationLatestVerdict(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"review", "deterministic"} {
		for _, initial := range []string{"pass", "fail"} {
			t.Run(method+"/"+initial, func(t *testing.T) {
				w, b := pvVerificationWorld(t, method, initial)
				first := map[string]string{"pass": "true", "fail": "false"}[initial]
				pvAssertVerification(t, w, b, first, b.Verification)
				if initial == "pass" {
					pvEvaluate(t, w, s1, "done")
					fail := pvReverify(t, w, b, method, "fail")
					pvAssertVerification(t, w, b, "false", fail)
					pvEvaluate(t, w, s1, "active")
				}
				pass := pvReverify(t, w, b, method, "pass")
				pvAssertVerification(t, w, b, "true", pass)
				pvEvaluate(t, w, s1, "done")
				none := pvReverify(t, w, b, method, "none")
				pvAssertVerification(t, w, b, "unknown", none)
				pvEvaluate(t, w, s1, "active")
			})
		}
	}
}

func TestPV2VerificationLatestKeepsScope(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"attempt", "method", "task", "result"} {
		t.Run(scope, func(t *testing.T) {
			w, b := pvVerificationWorld(t, "review", "fail")
			pass := pvReverify(t, w, b, "review", "pass")
			other, method := b, "review"
			switch scope {
			case "attempt":
				ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id=$1 ORDER BY attempt_no LIMIT 1`, b.Task).Scan(&other.Attempt))
			case "method":
				method = "deterministic"
			case "task":
				other.Task = w.ready(t, store.Task{BaseCommit: commitB})
				claim := w.run(t, "h6", cursor, "done")
				if claim.TaskID != other.Task {
					t.Fatalf("claimed %s, want %s", claim.TaskID, other.Task)
				}
				ok(t, w.rt.QueryRow(ctx, `SELECT attempt_id FROM agentrt.attempt WHERE task_id=$1`, other.Task).Scan(&other.Attempt))
			case "result":
				other.Result = commitB
			}
			pvReverify(t, w, other, method, "fail")
			pvAssertVerification(t, w, b, "true", pass)
			pvEvaluate(t, w, s1, "done")
		})
	}
}

func TestPV2VerificationLatestStillRequiresIndependentReview(t *testing.T) {
	t.Parallel()
	w, b := pvVerificationWorld(t, "review", "pass")
	pvAssertVerification(t, w, b, "true", b.Verification)
	pvEvaluate(t, w, s1, "done")
	// A newer untrusted review must not fall back to the old trusted pass.
	var verifier string
	ok(t, w.rt.QueryRow(ctx, `SELECT verifier_attempt_id FROM agentrt.verification WHERE task_id=$1 AND attempt_id=$2 AND method='deterministic'`, b.Task, b.Attempt).Scan(&verifier))
	pvReverify(t, w, b, "review", "pass", verifier)
	pvPoll(t, w, store.PlanSources{})
	var value string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation'->>'value' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+root).Scan(&value))
	if value != "unknown" {
		t.Fatalf("untrusted latest review = %s", value)
	}
	pvEvaluate(t, w, s1, "active")
}
