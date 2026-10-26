package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

type ExternalDescription struct {
	V          int      `json:"v"`
	Kind       string   `json:"kind"`
	Task       string   `json:"task"`
	Executor   string   `json:"executor"`
	Provider   string   `json:"provider"`
	Repo       string   `json:"repo"`
	Branch     string   `json:"branch"`
	Acceptance []string `json:"acceptance_refs"`
	Estimate   []int    `json:"estimate_minutes"`
}
type ExternalRegistration struct {
	Key        string `json:"idempotency_key"`
	Root       string `json:"root"`
	Node       string `json:"plan_node_id"`
	RunKey     string `json:"source_run_key"`
	Artifact   string `json:"artifact_id"`
	Mission    string `json:"mission_id,omitempty"`
	Task       string `json:"task_id,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
}
type ExternalEvent struct {
	Key              string   `json:"idempotency_key"`
	Seq              int64    `json:"seq"`
	Type             string   `json:"type"`
	ObservedAt       string   `json:"observed_at,omitempty"`
	Root             string   `json:"root,omitempty"`
	Node             string   `json:"plan_node_id,omitempty"`
	Mission          string   `json:"mission_id,omitempty"`
	Task             string   `json:"task_id,omitempty"`
	ReportPathDigest string   `json:"report_path_digest,omitempty"`
	ReportDigest     string   `json:"report_digest,omitempty"`
	Commits          []string `json:"commit_shas,omitempty"`
	ExitCode         *int     `json:"exit_code,omitempty"`
}
type externalNote struct {
	V                  int      `json:"v"`
	Kind               string   `json:"kind"`
	ID                 string   `json:"external_work_id"`
	Actor              string   `json:"registrant"`
	Root               string   `json:"root"`
	Node               string   `json:"plan_node_id"`
	RunKey             string   `json:"source_run_key"`
	Artifact           string   `json:"artifact_id"`
	Mission            string   `json:"mission_id,omitempty"`
	Task               string   `json:"task_id,omitempty"`
	Seq                int64    `json:"seq"`
	Type               string   `json:"type"`
	State              string   `json:"state"`
	RequestDigest      string   `json:"request_digest"`
	RegistrationDigest string   `json:"registration_digest"`
	ObservedAt         string   `json:"observed_at,omitempty"`
	RegisteredAt       string   `json:"registered_at"`
	SourceStartedAt    string   `json:"source_started_at,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"`
	HeartbeatAt        string   `json:"heartbeat_at"`
	EndedAt            string   `json:"ended_at,omitempty"`
	ReportPathDigest   string   `json:"report_path_digest,omitempty"`
	ReportDigest       string   `json:"report_digest,omitempty"`
	Commits            []string `json:"commit_shas,omitempty"`
	ExitCode           *int     `json:"exit_code,omitempty"`
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

func strictViewValue(v any, out any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return strictViewJSON(raw, out)
}
func strictViewJSON(raw []byte, out any) error {
	if _, err := canon.Parse(raw); err != nil {
		return invalid("invalid JSON: %v", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return invalid("invalid fields: %v", err)
	}
	return nil
}
func (n externalNote) validate() error {
	if n.V != 1 || n.Kind != "agentrt.external_work/v1" || n.ID == "" || n.Actor == "" || n.Root == "" || n.Node == "" || n.RunKey == "" || n.Seq < 0 || !digestPattern.MatchString(n.Artifact) || !digestPattern.MatchString(n.RequestDigest) || !digestPattern.MatchString(n.RegistrationDigest) {
		return invalid("invalid external_work note")
	}
	if !slices.Contains([]string{"registered", "started", "heartbeat", "submitted", "cancelled", "linked"}, n.Type) || !slices.Contains([]string{"Registered", "Running", "Submitted", "Cancelled"}, n.State) {
		return invalid("external work cannot verify completion")
	}
	return nil
}
func externalDescription(ctx context.Context, tx pgx.Tx, id string) (ExternalDescription, error) {
	var d ExternalDescription
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT b.body FROM agentrt.artifact_blob b JOIN agentrt.artifact a USING(artifact_id) WHERE a.artifact_id=$1 AND a.artifact_kind='other'`, id).Scan(&raw)
	if err != nil {
		return d, invalid("external description unavailable: %v", err)
	}
	return parseExternalDescription(raw)
}

// ValidateExternalDescription applies the registration's closed schema at
// reporter upload, before the general artifact uploader selects a protocol.
func ValidateExternalDescription(raw []byte) error {
	_, err := parseExternalDescription(raw)
	return err
}

func parseExternalDescription(raw []byte) (ExternalDescription, error) {
	var d ExternalDescription
	if len(raw) > maxArtifactBytes {
		return d, invalid("external description is at most 1 MiB")
	}
	v, err := canon.Parse(raw)
	if err != nil {
		return d, invalid("invalid JSON: %v", err)
	}
	fields, ok := v.(map[string]any)
	if !ok {
		return d, invalid("external description must be an object")
	}
	// Check the original key set before struct decoding: encoding/json also
	// matches case/Unicode-folded aliases, which could hide invalid values
	// behind a later field while the original bytes are stored unchanged.
	for key := range fields {
		switch key {
		case "v", "kind", "task", "executor", "provider", "repo", "branch", "acceptance_refs", "estimate_minutes":
		default:
			return d, invalid("invalid external description field: %q", key)
		}
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, invalid("invalid fields: %v", err)
	}
	if d.V != 1 || d.Kind != "agentrt.external_work/v1" || len(d.Acceptance) == 0 || len(d.Estimate) != 2 || d.Estimate[0] < 0 || d.Estimate[1] < d.Estimate[0] {
		return d, invalid("invalid external description")
	}
	for _, s := range append([]string{d.Task, d.Executor, d.Provider, d.Repo, d.Branch}, d.Acceptance...) {
		if strings.TrimSpace(s) == "" || len(s) > 2000 {
			return d, invalid("external description needs bounded nonempty text")
		}
	}
	return d, nil
}
func externalBinding(ctx context.Context, tx pgx.Tx, root, node, mission, task string) error {
	// Callers hold the root lock. Like view adoption, lock every node before
	// reading the dev boundary: task proposals hold their step FOR SHARE.
	// This also protects a linked event claiming a previously unclaimed root.
	rows, err := tx.Query(ctx, viewTree+`SELECT n.plan_node_id FROM tree JOIN agentrt.plan_node n USING(plan_node_id) ORDER BY n.plan_node_id FOR UPDATE OF n`, root)
	if err != nil {
		return err
	}
	if _, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return err
	}
	r, err := rootOf(ctx, tx, node)
	if err != nil {
		return err
	}
	if r != root {
		return invalid("node belongs to another root")
	}
	// Registration is allowed for legacy dev trees, but never business trees.
	if err = devTree(ctx, tx, root); err != nil {
		return err
	}
	if task != "" {
		var tm, tn string
		err = tx.QueryRow(ctx, `SELECT mission_id,COALESCE(plan_node_id,'') FROM agentrt.task WHERE task_id=$1 FOR SHARE`, task).Scan(&tm, &tn)
		if err != nil {
			return err
		}
		if tn != node || mission != "" && tm != mission {
			return invalid("task belongs to another step or mission")
		}
		mission = tm
	}
	if mission != "" {
		var tenant string
		err = tx.QueryRow(ctx, `SELECT tenant FROM agentrt.mission WHERE mission_id=$1 FOR SHARE`, mission).Scan(&tenant)
		if err != nil {
			return err
		}
		if tenant != "dev" {
			return invalid("external work only binds dev")
		}
		var bound bool
		err = tx.QueryRow(ctx, `SELECT COALESCE(mission_id=$2, $3) FROM agentrt.plan_node WHERE plan_node_id=$1`, node, mission, task != "").Scan(&bound)
		if err != nil || !bound {
			return invalid("mission is not bound to this step")
		}
	}
	return nil
}
func readExternal(ctx context.Context, tx pgx.Tx, id string) (externalNote, int64, time.Time, error) {
	var n externalNote
	var raw []byte
	var event int64
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT body_json->'external_work',event_id,at FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' AND body_json ? 'external_work' ORDER BY seq DESC LIMIT 1`, "external-work:"+id).Scan(&raw, &event, &at)
	if err == nil {
		err = json.Unmarshal(raw, &n)
	}
	return n, event, at, err
}
func externalReply(n externalNote, event int64) map[string]any {
	return map[string]any{"external_work_id": n.ID, "event_id": event, "seq": n.Seq, "state": n.State, "plan_url": "#work?section=plan&root=" + url.QueryEscape(n.Root) + "&node=" + url.QueryEscape(n.Node)}
}
func appendExternal(ctx context.Context, tx pgx.Tx, actor string, n externalNote) (map[string]any, error) {
	body, err := jsonObject(n)
	if err != nil {
		return nil, err
	}
	ev, err := Append(ctx, tx, Event{Stream: "external-work:" + n.ID, Type: "note", Actor: actor, PlanNode: n.Node, Body: map[string]any{"text": "External work " + n.Type, "external_work": body}})
	if err != nil {
		return nil, err
	}
	return externalReply(n, ev.EventID), nil
}
func checkObserved(s string) error {
	if s != "" {
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return invalid("observed_at must be RFC3339")
		}
	}
	return nil
}
func RegisterExternalWork(ctx context.Context, db DB, w Writer, in ExternalRegistration) (map[string]any, error) {
	if in.Key == "" || in.Root == "" || in.Node == "" || in.RunKey == "" || len(in.RunKey) > 200 || !digestPattern.MatchString(in.Artifact) {
		return nil, invalid("registration requires key, root, node, source_run_key and artifact digest")
	}
	if err := checkObserved(in.ObservedAt); err != nil {
		return nil, err
	}
	req, _ := jsonObject(in)
	delete(req, "idempotency_key")
	sum, err := canon.Sum(req)
	sum = "sha256:" + sum
	if err != nil {
		return nil, err
	}
	identity, _ := canon.Sum(map[string]any{"actor": w.Actor, "run": in.RunKey})
	id := "ew_" + strings.TrimPrefix(identity, "sha256:")
	return idempotentAs(ctx, db, w, "external-work", in.Key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockTree(ctx, tx, in.Root); err != nil {
			return nil, err
		}
		if err := externalBinding(ctx, tx, in.Root, in.Node, in.Mission, in.Task); err != nil {
			return nil, err
		}
		if _, err := externalDescription(ctx, tx, in.Artifact); err != nil {
			return nil, err
		}
		if err := lockStream(ctx, tx, "external-work:"+id); err != nil {
			return nil, err
		}
		prev, event, _, err := readExternal(ctx, tx, id)
		if err == nil {
			if prev.RegistrationDigest != sum {
				return nil, ErrConflict
			}
			return externalReply(prev, event), nil
		}
		if !isNoRows(err) {
			return nil, err
		}
		now := timeText(eventClock())
		n := externalNote{V: 1, Kind: "agentrt.external_work/v1", ID: id, Actor: w.Actor, Root: in.Root, Node: in.Node, RunKey: in.RunKey, Artifact: in.Artifact, Mission: in.Mission, Task: in.Task, Type: "registered", State: "Registered", RequestDigest: sum, RegistrationDigest: sum, ObservedAt: in.ObservedAt, SourceStartedAt: in.ObservedAt, RegisteredAt: now, HeartbeatAt: now}
		return appendExternal(ctx, tx, w.Actor, n)
	})
}
func PostExternalEvent(ctx context.Context, db DB, w Writer, id string, in ExternalEvent) (map[string]any, error) {
	if in.Key == "" || in.Seq < 1 || !slices.Contains([]string{"started", "heartbeat", "submitted", "cancelled", "linked"}, in.Type) {
		return nil, invalid("invalid external event type, key or seq")
	}
	if err := checkObserved(in.ObservedAt); err != nil {
		return nil, err
	}
	if in.Type != "linked" && (in.Root != "" || in.Node != "" || in.Mission != "" || in.Task != "") {
		return nil, invalid("only linked changes bindings")
	}
	if in.Type == "linked" && (in.Root == "" || in.Node == "") {
		return nil, invalid("linked requires root and node")
	}
	if in.Type != "submitted" && (in.ReportPathDigest != "" || in.ReportDigest != "" || len(in.Commits) > 0 || in.ExitCode != nil) {
		return nil, invalid("only submitted carries results")
	}
	for _, s := range []string{in.ReportPathDigest, in.ReportDigest} {
		if s != "" && !digestPattern.MatchString(s) {
			return nil, invalid("invalid report digest")
		}
	}
	if len(in.Commits) > 100 {
		return nil, invalid("too many commits")
	}
	for _, s := range in.Commits {
		if !shaPattern.MatchString(s) {
			return nil, invalid("invalid commit SHA")
		}
	}
	req, _ := jsonObject(in)
	delete(req, "idempotency_key")
	sum, err := canon.Sum(req)
	sum = "sha256:" + sum
	if err != nil {
		return nil, err
	}
	return idempotentAs(ctx, db, w, "external-work/"+id+"/events", in.Key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		// Read binding before locking; retry a concurrent rebind without taking
		// roots after the stream lock (the registration lock order).
		before, _, _, err := readExternal(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if before.Actor != w.Actor {
			return nil, ErrNotYours
		}
		roots := []string{before.Root}
		if in.Root != "" {
			roots = append(roots, in.Root)
		}
		slices.Sort(roots)
		for _, r := range slices.Compact(roots) {
			if err = lockTree(ctx, tx, r); err != nil {
				return nil, err
			}
		}
		node, root, mission, task := before.Node, before.Root, before.Mission, before.Task
		if in.Type == "linked" {
			node, root, mission, task = in.Node, in.Root, in.Mission, in.Task
		}
		if err = externalBinding(ctx, tx, root, node, mission, task); err != nil {
			return nil, err
		}
		if err = lockStream(ctx, tx, "external-work:"+id); err != nil {
			return nil, err
		}
		n, _, _, err := readExternal(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if n.Root != before.Root || n.Node != before.Node {
			return nil, ErrConflict
		}
		if in.Seq <= n.Seq {
			var old []byte
			var event int64
			err = tx.QueryRow(ctx, `SELECT body_json->'external_work',event_id FROM agentrt.event WHERE event_stream_id=$1 AND (body_json->'external_work'->>'seq')::bigint=$2`, "external-work:"+id, in.Seq).Scan(&old, &event)
			if err != nil {
				return nil, ErrConflict
			}
			var previous externalNote
			if err = json.Unmarshal(old, &previous); err != nil {
				return nil, err
			}
			if previous.RequestDigest != sum {
				return nil, ErrConflict
			}
			return externalReply(previous, event), nil
		}
		if in.Seq != n.Seq+1 {
			return nil, fmt.Errorf("%w: expected seq %d", ErrConflict, n.Seq+1)
		}
		if (n.State == "Submitted" || n.State == "Cancelled") && in.Type != "linked" {
			return nil, ErrState
		}
		now := timeText(eventClock())
		n.Seq, n.Type, n.RequestDigest, n.ObservedAt = in.Seq, in.Type, sum, in.ObservedAt
		switch in.Type {
		case "started":
			if n.State != "Registered" {
				return nil, ErrState
			}
			n.State, n.StartedAt, n.HeartbeatAt = "Running", now, now
		case "heartbeat":
			if n.State != "Running" {
				return nil, ErrState
			}
			n.HeartbeatAt = now
		case "submitted":
			if n.State != "Running" {
				return nil, ErrState
			}
			n.State, n.EndedAt = "Submitted", now
			n.ReportPathDigest, n.ReportDigest, n.Commits, n.ExitCode = in.ReportPathDigest, in.ReportDigest, in.Commits, in.ExitCode
		case "cancelled":
			n.State, n.EndedAt = "Cancelled", now
		case "linked":
			n.Root, n.Node, n.Mission, n.Task = root, node, mission, task
		}
		return appendExternal(ctx, tx, w.Actor, n)
	})
}

// ReadExternalWork supports crash recovery. It is read-only and exposes only
// the same bounded metadata shown by the plan snapshot.
func ReadExternalWork(ctx context.Context, db DB, id string) (map[string]any, error) {
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n, event, _, err := readExternal(ctx, tx, id)
		if err == nil {
			out = externalReply(n, event)
		}
		return err
	})
	return out, err
}
