package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

const planObservationTTL = 60 * time.Second

// PlanSources are controller-owned adapters, never request payloads. History
// must read complete Git history, including reverts. Deployed must check actual
// running digests; health is independently checked against a gate record below.
type PlanSources struct {
	History  func(context.Context, string, string, string, string) (landing.MainHistory, error)
	Deployed func(context.Context, string, string) (string, map[string]any, bool)
}

type planObservation struct {
	V             int      `json:"v"`
	Kind          string   `json:"kind"`
	Root          string   `json:"root"`
	Step          string   `json:"step"`
	Binding       string   `json:"binding"`
	Version       int      `json:"version"`
	Digest        string   `json:"binding_sha256"`
	Source        string   `json:"source_id"`
	SourceVersion int64    `json:"source_version"`
	Status        string   `json:"status"`
	Value         string   `json:"value"`
	Evidence      []string `json:"evidence"`
	LastValue     string   `json:"last_value,omitempty"`
	LastSuccess   string   `json:"last_success,omitempty"`
	Reason        string   `json:"reason"`
	Changed       bool     `json:"changed"`
}

func (n planObservation) validate() error {
	if n.V != 1 || n.Kind != "agentrt.plan_observation/v1" || n.Root == "" || n.Step == "" || n.Binding == "" || n.Version < 1 || len(n.Digest) != 64 || !shaPattern.MatchString(n.Digest) || n.Source != n.Root+":"+n.Digest || n.SourceVersion < 1 || (n.Status != "Fresh" && n.Status != "Unknown") || (n.Value != "true" && n.Value != "false" && n.Value != "unknown") || n.Evidence == nil {
		return invalid("invalid plan observation")
	}
	if n.Status == "Fresh" && n.LastSuccess == "" || n.Value == "true" && len(n.Evidence) == 0 || n.Reason == "" {
		return invalid("observation needs server time, reason and success evidence")
	}
	if n.Status == "Unknown" && n.Value != "unknown" {
		return invalid("unreadable source cannot report a verdict")
	}
	if n.LastSuccess != "" {
		if _, err := time.Parse(time.RFC3339Nano, n.LastSuccess); err != nil {
			return invalid("invalid observation time")
		}
	}
	return nil
}
func observationStream(root string) string     { return "plan-observation:" + root }
func observationKey(b planview.Binding) string { return b.Root + ":" + b.Digest() }

// collect is serial per binding: the source is read after taking the lock.
// A poll is a new source version even when its business value is unchanged.
// Replaying a version is idempotent, and a late older version cannot win.
func CollectPlanObservations(ctx context.Context, db DB, src PlanSources) (int, error) {
	var bindings []planview.Binding
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		s, e := readPlanFacts(ctx, tx)
		if e != nil {
			return e
		}
		for _, d := range s.Docs {
			bs, e := d.Bindings()
			if e != nil {
				return e
			}
			bindings = append(bindings, bs...)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	count := 0
	var errs []error
	for _, b := range bindings {
		err = InTx(ctx, db, func(tx pgx.Tx) error {
			// Step first, matching adoption/signoff; poll cannot publish for a
			// binding concurrently replaced by a new plan decision.
			if _, e := tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id=$1 FOR SHARE`, b.Step); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742120,hashtext($1))`, observationKey(b)); e != nil {
				return e
			}
			s, e := readPlanFacts(ctx, tx)
			if e != nil {
				return e
			}
			current, ok := s.find(b.Step, b.Atom, b.ID, b.Version)
			if !ok || current.Digest() != b.Digest() {
				return nil
			}
			old := s.Observations[b.Digest()]
			n := planObservation{V: 1, Kind: "agentrt.plan_observation/v1", Root: b.Root, Step: b.Step, Binding: b.ID, Version: b.Version, Digest: b.Digest(), Source: observationKey(b), SourceVersion: old.SourceVersion + 1, Status: "Fresh", Value: "unknown", Evidence: []string{}, Reason: "No trusted verification yet"}
			if e = readPlanSource(ctx, tx, src, b, &n); e != nil {
				n.Status = "Unknown"
				n.Value = "unknown"
				n.Reason = "Trusted source unavailable"
			}
			var now time.Time
			if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				return e
			}
			if n.Status == "Fresh" {
				n.LastValue = n.Value
				n.LastSuccess = timeText(now)
			} else {
				n.LastValue = old.LastValue
				n.LastSuccess = old.LastSuccess
				n.Evidence = old.Evidence
				if n.Evidence == nil {
					n.Evidence = []string{}
				}
			}
			n.Changed = n.Status != old.Status || n.Value != old.Value || n.Reason != old.Reason || fmt.Sprint(n.Evidence) != fmt.Sprint(old.Evidence)
			did, e := recordPlanObservation(ctx, tx, n)
			if did {
				count++
			}
			return e
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return count, errors.Join(errs...)
}

func recordPlanObservation(ctx context.Context, tx pgx.Tx, n planObservation) (bool, error) {
	if err := n.validate(); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742120,hashtext($1))`, n.Source); err != nil {
		return false, err
	}
	body, err := jsonObject(n)
	if err != nil {
		return false, err
	}
	var old []byte
	err = tx.QueryRow(ctx, `SELECT body_json->'plan_observation' FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'plan_observation'->>'source_id'=$2 AND (body_json->'plan_observation'->>'source_version')::bigint=$3`, observationStream(n.Root), n.Source, n.SourceVersion).Scan(&old)
	if err == nil {
		parsed, e := canon.Parse(old)
		if e != nil {
			return false, e
		}
		had, _ := canon.Encode(parsed)
		want, _ := canon.Encode(body)
		if string(had) != string(want) {
			return false, fmt.Errorf("%w: source version changed", ErrConflict)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	_, err = Append(ctx, tx, Event{Stream: observationStream(n.Root), Type: "note", Actor: "controller", PlanNode: n.Step, Body: map[string]any{"text": "Plan verification observed", "plan_observation": body}})
	return err == nil, err
}

func readPlanSource(ctx context.Context, tx pgx.Tx, src PlanSources, b planview.Binding, n *planObservation) error {
	if b.ExternalWork != "" {
		x, e := bindingExternal(ctx, tx, b.ExternalWork)
		if e != nil {
			return e
		}
		if !externalMatches(x, b) {
			n.Reason = "External submission changed"
			return nil
		}
	}
	if b.Task != "" {
		var current bool
		e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) JOIN agentrt.attempt a USING(task_id) WHERE t.task_id=$1 AND a.attempt_id=$2 AND t.mission_id=$3 AND t.plan_node_id=$4 AND m.tenant='dev' AND t.repo=$5 AND a.pushed_commit=$6 AND t.round_no=$7 AND a.attempt_no=(SELECT max(attempt_no) FROM agentrt.attempt WHERE task_id=t.task_id) AND t.state NOT IN ('rework','cancelled','stopped','dropped'))`, b.Task, b.Attempt, b.Mission, b.Step, b.Repo, b.Result, b.Round).Scan(&current)
		if e != nil {
			return e
		}
		if !current {
			n.Reason = "Runtime result changed"
			return nil
		}
	}
	switch b.Source {
	case "owner_signoff":
		var id int64
		e := tx.QueryRow(ctx, `SELECT event_id FROM agentrt.event WHERE event_type='owner_signoff' AND plan_node_id=$1 AND actor_id='owner' AND body_json->>'ref'=$2 ORDER BY seq DESC LIMIT 1`, b.Step, b.SignoffRef()).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		n.Value = "true"
		n.Evidence = append([]string{fmt.Sprintf("event:%d", id)}, b.Evidence...)
		n.Reason = "Owner signed this acceptance version and evidence"
	case "verification":
		var verdict, method, evidence string
		var independent bool
		e := tx.QueryRow(ctx, `SELECT v.verdict,v.method,v.evidence_artifact_id,
    COALESCE(va.executor_actor_id<>a.executor_actor_id AND vt.subject_task_id=t.task_id AND vt.role='review' AND vt.mission_id=t.mission_id AND va.model_provider IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agentrt.attempt sa WHERE sa.task_id=t.task_id AND (sa.executor_actor_id=va.executor_actor_id OR sa.model_provider IS NULL OR sa.model_provider=va.model_provider)) AND convert_from(vb.body,'UTF8')::jsonb @> jsonb_build_object('round',t.round_no,'attempt',a.attempt_id,'commit',a.pushed_commit,'subject_task',t.task_id),false)
    FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id=v.attempt_id AND a.task_id=v.task_id JOIN agentrt.task t ON t.task_id=a.task_id
    LEFT JOIN agentrt.attempt va ON va.attempt_id=v.verifier_attempt_id LEFT JOIN agentrt.task vt ON vt.task_id=va.task_id LEFT JOIN agentrt.artifact_blob vb ON vb.artifact_id=vt.spec_artifact_id
    WHERE v.verification_id=$1 AND v.task_id=$2 AND v.attempt_id=$3 AND a.pushed_commit=$4 AND v.subject_commit=$4`, b.Verification, b.Task, b.Attempt, b.Result).Scan(&verdict, &method, &evidence, &independent)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if b.Atom == "review_valid" && (method != "review" || !independent) {
			n.Reason = "No independent review of this result"
			return nil
		}
		if b.Atom == "external_verified" && method != "deterministic" && (method != "review" || !independent) {
			n.Reason = "Not an allowed external verifier"
			return nil
		}
		n.Evidence = []string{b.Verification, evidence}
		n.Reason = "Exact task/attempt verification"
		if verdict == "pass" {
			n.Value = "true"
		} else if verdict == "fail" {
			n.Value = "false"
		}
	case "git", "deployment":
		if src.History == nil {
			return fmt.Errorf("history unavailable")
		}
		snapshot := ""
		var deployRef map[string]any
		if b.Source == "deployment" {
			if src.Deployed == nil {
				return fmt.Errorf("deployment unavailable")
			}
			var ok bool
			snapshot, deployRef, ok = src.Deployed(ctx, b.Application, b.Environment)
			if !ok || len(snapshot) != 40 || len(deployRef) == 0 {
				return fmt.Errorf("deployment unavailable")
			}
		}
		h, e := src.History(ctx, b.Repo, b.Task, b.Result, snapshot)
		if e != nil {
			return e
		}
		switch h.State {
		case landing.Unknown:
			return fmt.Errorf("incomplete history")
		case landing.Never:
			n.Value = "false"
			n.Reason = "Not landed"
			return nil
		case landing.Reverted:
			n.Value = "false"
			n.Reason = "Result reverted"
			n.Evidence = []string{h.Commit}
			return nil
		case landing.Effective:
			if h.Commit == "" {
				return fmt.Errorf("missing main result")
			}
		default:
			return fmt.Errorf("unknown history state")
		}
		n.Evidence = []string{b.Repo + "@" + h.Commit}
		n.Value = "true"
		n.Reason = "Result effective on main"
		if b.Source == "deployment" {
			// A green delivery run alone is insufficient. A platform gate must
			// attest health for the same actual SHA, digest set and specification.
			raw, e := canon.Encode(deployRef)
			if e != nil {
				return e
			}
			var evidence, id, verdict string
			e = tx.QueryRow(ctx, `SELECT v.verification_id,v.evidence_artifact_id,v.verdict FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id=v.attempt_id AND a.task_id=v.task_id JOIN agentrt.task t ON t.task_id=v.task_id JOIN agentrt.mission m USING(mission_id) WHERE v.method='gate' AND m.tenant='dev' AND t.repo=$1 AND v.env=$2 AND v.subject_commit=$3 AND v.deploy_ref_json=$4::jsonb AND v.check_ref=$5 AND v.spec_sha256=$6 ORDER BY v.created_at DESC,v.verification_id DESC LIMIT 1`, b.Repo, b.Environment, snapshot, string(raw), b.Check, b.Spec).Scan(&id, &evidence, &verdict)
			n.Value = "unknown"
			n.Reason = "Waiting for health verification of actual deployment"
			if errors.Is(e, pgx.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			n.Value = "unknown"
			n.Reason = "Health verification has no verdict"
			if verdict == "pass" {
				n.Value = "true"
				n.Reason = "Deployment and health verified"
			} else if verdict == "fail" {
				n.Value = "false"
				n.Reason = "Deployment health verification failed"
			}
			n.Evidence = append(n.Evidence, snapshot, string(raw), id, evidence)
		}
	}
	return nil
}
