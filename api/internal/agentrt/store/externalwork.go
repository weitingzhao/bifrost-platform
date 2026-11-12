package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// The session identity of a collector's work (R5:5.5-12 §1.4): required
	// of a collector, refused from anyone else, never changed. Empty, they
	// leave the request and its digest as they were.
	Vendor  string `json:"vendor,omitempty"`
	Session string `json:"session,omitempty"`
	Epoch   int64  `json:"epoch,omitempty"`
	Sidebar string `json:"sidebar,omitempty"`
}

// ExternalEvent is one event of an external work. Seq is its place in the
// work, from 1: left out (or 0), the server gives it the next one and the
// answer's seq says which. Commits are the commits a heartbeat or the
// submitted event names: the work's note keeps every one named so far, in
// the order first named.
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
	// A collector's event names the identity its work was registered with,
	// every event, linked included (§1.4).
	Vendor  string `json:"vendor,omitempty"`
	Session string `json:"session,omitempty"`
	Epoch   int64  `json:"epoch,omitempty"`
}
type externalNote struct {
	V     int    `json:"v"`
	Kind  string `json:"kind"`
	ID    string `json:"external_work_id"`
	Actor string `json:"registrant"`
	// A collector's work only (§1.4): the host the server writes from the
	// collector's token, and the identity it was registered with. A note
	// without them is any other registrant's, or a collector's from before.
	Host               string   `json:"host,omitempty"`
	Vendor             string   `json:"vendor,omitempty"`
	Session            string   `json:"session,omitempty"`
	Epoch              int64    `json:"epoch,omitempty"`
	Sidebar            string   `json:"sidebar,omitempty"`
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

// CollectorActorPrefix starts a collector's actor, thread:collector@<host>
// (actuation.CollectorActorPrefix; agentrtapi's tests hold the two equal).
const CollectorActorPrefix = "thread:collector@"

// ErrIdentityRequired: a collector's write lacks its session identity, or
// names one of the wrong shape (400 identity_required). ErrIdentityMismatch:
// an event names another identity than its work's (409 identity_mismatch).
var (
	ErrIdentityRequired = errors.New("store: a collector's external work names its session identity")
	ErrIdentityMismatch = errors.New("store: the session identity is not this work's")
)

// identityVendors are the vendors a session identity names; identityShape
// is the shape of its session and sidebar: a vendor's session id, or the
// run key of a wrapper (§1.4: the server checks the shape, not the meaning).
var (
	identityVendors = []string{"claude", "cursor", "codex"}
	identityShape   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// collectorWriter checks a writer's binding: a host-bound writer is that
// host's collector; a writer that is not host-bound is no collector.
func collectorWriter(w Writer) (bool, error) {
	switch {
	case w.Host != "" && w.Actor != CollectorActorPrefix+w.Host:
		return false, invalid("a host-bound writer is its host's collector")
	case w.Host == "" && strings.HasPrefix(w.Actor, CollectorActorPrefix):
		return false, invalid("a collector writes bound to its host")
	}
	return w.Host != "", nil
}

// checkIdentity holds the identity a write names to its writer: a
// collector names a whole one of the right shape (ErrIdentityRequired);
// anyone else names none.
func checkIdentity(collector bool, vendor, session string, epoch int64, sidebar string) error {
	if !collector {
		if vendor != "" || session != "" || epoch != 0 || sidebar != "" {
			return invalid("vendor, session, epoch and sidebar are a collector's")
		}
		return nil
	}
	switch {
	case !slices.Contains(identityVendors, vendor):
		return fmt.Errorf("%w: vendor is claude, cursor or codex", ErrIdentityRequired)
	case !identityShape.MatchString(session):
		return fmt.Errorf("%w: session is 1-128 of [A-Za-z0-9._:-]", ErrIdentityRequired)
	case epoch < 1:
		return fmt.Errorf("%w: epoch is a positive integer", ErrIdentityRequired)
	case sidebar != "" && !identityShape.MatchString(sidebar):
		return fmt.Errorf("%w: sidebar is 1-128 of [A-Za-z0-9._:-]", ErrIdentityRequired)
	}
	return nil
}

// IdentityRunKey is the source run key of a collector's work: fixed by its
// identity, <vendor>:<session>:<epoch> (§1.4), so its work id is too.
func IdentityRunKey(vendor, session string, epoch int64) string {
	return fmt.Sprintf("%s:%s:%d", vendor, session, epoch)
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

// errNotDescription is the one answer for an artifact a registration names
// that is not an external work description: absent, of another kind, or a
// body that does not parse as one. It says nothing of the body, which may be
// another writer's (R5:5.5-12 §1.3: a collector names a digest it may not
// read), so no field name or value of it reaches the caller.
var errNotDescription = invalid("the artifact is not an external work description")

func externalDescription(ctx context.Context, tx pgx.Tx, id string) (ExternalDescription, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT b.body FROM agentrt.artifact_blob b JOIN agentrt.artifact a USING(artifact_id) WHERE a.artifact_id=$1 AND a.artifact_kind='other'`, id).Scan(&raw)
	switch {
	case isNoRows(err):
		return ExternalDescription{}, errNotDescription
	case err != nil:
		return ExternalDescription{}, err
	}
	d, err := parseExternalDescription(raw)
	if err != nil {
		return ExternalDescription{}, errNotDescription
	}
	return d, nil
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
	out := map[string]any{"external_work_id": n.ID, "event_id": event, "seq": n.Seq, "state": n.State, "plan_url": "#work?section=plan&root=" + url.QueryEscape(n.Root) + "&node=" + url.QueryEscape(n.Node)}
	// When it was registered (R5:5.5-12 §3.3): every note of the work
	// carries its registration's time, so a replay and a recovery read give
	// the first answer's.
	if n.RegisteredAt != "" {
		out["registered_at"] = n.RegisteredAt
	}
	return out
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
	collector, err := collectorWriter(w)
	if err != nil {
		return nil, err
	}
	if err := checkIdentity(collector, in.Vendor, in.Session, in.Epoch, in.Sidebar); err != nil {
		return nil, err
	}
	if collector {
		// The run key is the identity's; a client may leave it out.
		run := IdentityRunKey(in.Vendor, in.Session, in.Epoch)
		if in.RunKey != "" && in.RunKey != run {
			return nil, invalid("a collector's source_run_key is <vendor>:<session>:<epoch>, %s", run)
		}
		in.RunKey = run
	}
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
		if collector {
			n.Host, n.Vendor, n.Session, n.Epoch, n.Sidebar = w.Host, in.Vendor, in.Session, in.Epoch, in.Sidebar
		}
		return appendExternal(ctx, tx, w.Actor, n)
	})
}

// sameIdentityAsWork compares an event's identity with its work's, read
// without the stream lock: once registered, a work's identity never
// changes. A writer whose session is not the one held, and a work that is
// missing or another writer's, are left to the usual path, which answers
// them as it always has; only the writer's own work is compared.
func sameIdentityAsWork(ctx context.Context, db DB, w Writer, id string, in ExternalEvent) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		if holdsSessionTx(ctx, tx, w) != nil {
			return nil
		}
		n, _, _, err := readExternal(ctx, tx, id)
		switch {
		case isNoRows(err):
			return nil
		case err != nil:
			return err
		case n.Actor != w.Actor:
			return nil
		case n.Vendor == "":
			return fmt.Errorf("%w: this work was registered without one", ErrIdentityMismatch)
		case in.Vendor != n.Vendor || in.Session != n.Session || in.Epoch != n.Epoch:
			return ErrIdentityMismatch
		}
		return nil
	})
}

// maxExternalCommits is the most commits one event names, and the most a
// work's note keeps.
const maxExternalCommits = 100

// externalEventDigest is an event's request and its digest: the event as
// posted, without its idempotency key.
func externalEventDigest(in ExternalEvent) (map[string]any, string, error) {
	req, err := jsonObject(in)
	if err != nil {
		return nil, "", err
	}
	delete(req, "idempotency_key")
	sum, err := canon.Sum(req)
	return req, "sha256:" + sum, err
}

// addExternalCommits is have and then every commit of add it does not hold
// yet: what a work named so far, in the order first named.
func addExternalCommits(have, add []string) ([]string, error) {
	out := slices.Clone(have)
	for _, c := range add {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) > maxExternalCommits {
		return nil, invalid("too many commits")
	}
	return out, nil
}

// PostExternalEvent appends one event to an external work. An event that
// names its seq is the next one (the work's last seq + 1), or the replay of
// one already recorded under that seq. An event that leaves seq out gets the
// next one under the stream's lock, so two of them posted at once get two
// seqs in turn; its replay is by its idempotency key alone.
func PostExternalEvent(ctx context.Context, db DB, w Writer, id string, in ExternalEvent) (map[string]any, error) {
	collector, err := collectorWriter(w)
	if err != nil {
		return nil, err
	}
	if err := checkIdentity(collector, in.Vendor, in.Session, in.Epoch, ""); err != nil {
		return nil, err
	}
	if in.Key == "" || in.Seq < 0 || !slices.Contains([]string{"started", "heartbeat", "submitted", "cancelled", "linked"}, in.Type) {
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
	if in.Type != "submitted" && (in.ReportPathDigest != "" || in.ReportDigest != "" || in.ExitCode != nil) {
		return nil, invalid("only submitted carries results")
	}
	if len(in.Commits) > 0 && in.Type != "submitted" && in.Type != "heartbeat" {
		return nil, invalid("only heartbeat and submitted carry commits")
	}
	for _, s := range []string{in.ReportPathDigest, in.ReportDigest} {
		if s != "" && !digestPattern.MatchString(s) {
			return nil, invalid("invalid report digest")
		}
	}
	if len(in.Commits) > maxExternalCommits {
		return nil, invalid("too many commits")
	}
	for _, s := range in.Commits {
		if !shaPattern.MatchString(s) {
			return nil, invalid("invalid commit SHA")
		}
	}
	req, sum, err := externalEventDigest(in)
	if err != nil {
		return nil, err
	}
	// A replay under a used key with another identity would meet the key's
	// digest first and answer key_conflict, so a collector could not tell a
	// reused key from a changed identity (TD-516). The identity is the work's
	// for good once registered, so it is compared here, before the key is
	// looked at; the check under the stream lock below stays the authority.
	if collector {
		if err := sameIdentityAsWork(ctx, db, w, id, in); err != nil {
			return nil, err
		}
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
		// The identity is the work's for good, read under the stream's lock in
		// this transaction: every event names it whole, before its sequence is
		// looked at, so a replay under another identity is refused too.
		if collector {
			switch {
			case n.Vendor == "":
				return nil, fmt.Errorf("%w: this work was registered without one", ErrIdentityMismatch)
			case in.Vendor != n.Vendor || in.Session != n.Session || in.Epoch != n.Epoch:
				return nil, ErrIdentityMismatch
			}
		}
		// No seq named: the next one, read under the stream's lock. The note
		// records the event as if it had named that seq, so a later replay
		// naming it is answered like any other.
		seq, sum := in.Seq, sum
		if seq == 0 {
			seq = n.Seq + 1
			next := in
			next.Seq = seq
			if _, sum, err = externalEventDigest(next); err != nil {
				return nil, err
			}
		}
		if seq <= n.Seq {
			var old []byte
			var event int64
			err = tx.QueryRow(ctx, `SELECT body_json->'external_work',event_id FROM agentrt.event WHERE event_stream_id=$1 AND (body_json->'external_work'->>'seq')::bigint=$2`, "external-work:"+id, seq).Scan(&old, &event)
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
		if seq != n.Seq+1 {
			return nil, fmt.Errorf("%w: expected seq %d", ErrConflict, n.Seq+1)
		}
		if (n.State == "Submitted" || n.State == "Cancelled") && in.Type != "linked" {
			return nil, ErrState
		}
		now := timeText(eventClock())
		n.Seq, n.Type, n.RequestDigest, n.ObservedAt = seq, in.Type, sum, in.ObservedAt
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
			if n.Commits, err = addExternalCommits(n.Commits, in.Commits); err != nil {
				return nil, err
			}
		case "submitted":
			if n.State != "Running" {
				return nil, ErrState
			}
			n.State, n.EndedAt = "Submitted", now
			n.ReportPathDigest, n.ReportDigest, n.ExitCode = in.ReportPathDigest, in.ReportDigest, in.ExitCode
			if n.Commits, err = addExternalCommits(n.Commits, in.Commits); err != nil {
				return nil, err
			}
		case "cancelled":
			n.State, n.EndedAt = "Cancelled", now
		case "linked":
			n.Root, n.Node, n.Mission, n.Task = root, node, mission, task
		}
		return appendExternal(ctx, tx, w.Actor, n)
	})
}

// ErrNotOwnRecord: the reader sees only the records it wrote (a collector,
// R5:5.5-12 §1.3: its external work, the descriptions it uploaded first),
// and this is not one. Another writer's record and a record that does not
// exist answer the same, so the answer says nothing of other writers'.
var ErrNotOwnRecord = errors.New("store: this record is not the caller's")

// ReadOwnExternalWork is ReadExternalWork for a reader held to its own
// records: the work's registrant must be actor (the registrant never
// changes, linked included), or the answer is ErrNotOwnRecord.
func ReadOwnExternalWork(ctx context.Context, db DB, id, actor string) (map[string]any, error) {
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n, event, _, err := readExternal(ctx, tx, id)
		switch {
		case isNoRows(err), err == nil && n.Actor != actor:
			return ErrNotOwnRecord
		case err != nil:
			return err
		}
		out = externalReply(n, event)
		return nil
	})
	return out, err
}

// ExternalIdentity is one external work's latest record as a session's
// binding reads it (R5:5.5-12 §3.1): a collector's work, with the host and
// session identity it was registered with (a record naming an identity but
// no host has Host "").
type ExternalIdentity struct {
	ID, Root, Node, State, Host, Vendor, Session string
	Epoch                                        int64
}

// ExternalIdentities lists every external work whose record names a session
// identity, its latest note each: the plan view's query shape (one read-only
// snapshot, the latest note of each stream).
func ExternalIdentities(ctx context.Context, db DB) ([]ExternalIdentity, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := tx.Query(ctx, `SELECT body FROM (SELECT DISTINCT ON(event_stream_id) body_json->'external_work' AS body FROM agentrt.event
	  WHERE event_type='note' AND body_json ? 'external_work' AND event_stream_id LIKE 'external-work:%' ORDER BY event_stream_id,seq DESC) x
	  WHERE body ? 'vendor' ORDER BY body->>'external_work_id'`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ExternalIdentity, error) {
		var raw []byte
		var n externalNote
		if err := r.Scan(&raw); err != nil {
			return ExternalIdentity{}, err
		}
		if err := json.Unmarshal(raw, &n); err != nil {
			return ExternalIdentity{}, err
		}
		return ExternalIdentity{ID: n.ID, Root: n.Root, Node: n.Node, State: n.State, Host: n.Host, Vendor: n.Vendor, Session: n.Session, Epoch: n.Epoch}, nil
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
