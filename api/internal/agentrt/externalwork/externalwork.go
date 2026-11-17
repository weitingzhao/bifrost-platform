// Package externalwork is what the runtime keeps of work done outside it
// (lineage domain): the description a registration names, the note that is
// the work's record, the session identity of a collector's work, and the
// reads of a work by its id, the two writes: registering a work and posting
// its events (writes.go), and the landing of a recorder log (recorder.go). It
// imports the bases, the artifact store, workregistration and recorderimport,
// and not the store or the plan's package: the writes lock and read the plan
// tree through ports (ports.go) that store binds.
package externalwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
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

// Keep validation methods local while the record shape belongs to its domain.
type ExternalNote workregistration.Note

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

// CheckIdentity holds the identity a write names to its writer: a
// collector names a whole one of the right shape (ErrIdentityRequired);
// anyone else names none.
func CheckIdentity(collector bool, vendor, session string, epoch int64, sidebar string) error {
	if !collector {
		if vendor != "" || session != "" || epoch != 0 || sidebar != "" {
			return eventlog.Invalid("vendor, session, epoch and sidebar are a collector's")
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

// identityRunKey is the source run key of a collector's work: fixed by its
// identity, <vendor>:<session>:<epoch> (§1.4), so its work id is too.
func identityRunKey(vendor, session string, epoch int64) string {
	return fmt.Sprintf("%s:%s:%d", vendor, session, epoch)
}

var DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Validate holds a note to the record's shape.
func (n ExternalNote) Validate() error {
	if n.V != 1 || n.Kind != "agentrt.external_work/v1" || n.ID == "" || n.Actor == "" || n.Root == "" || n.Node == "" || n.RunKey == "" || n.Seq < 0 || !DigestPattern.MatchString(n.Artifact) || !DigestPattern.MatchString(n.RequestDigest) || !DigestPattern.MatchString(n.RegistrationDigest) {
		return eventlog.Invalid("invalid external_work note")
	}
	if !slices.Contains([]string{"registered", "started", "heartbeat", "submitted", "cancelled", "linked"}, n.Type) || !slices.Contains([]string{"Registered", "Running", "Submitted", "Cancelled"}, n.State) {
		return eventlog.Invalid("external work cannot verify completion")
	}
	return nil
}

// errNotDescription is the one answer for an artifact a registration names
// that is not an external work description: absent, of another kind, or a
// body that does not parse as one. It says nothing of the body, which may be
// another writer's (R5:5.5-12 §1.3: a collector names a digest it may not
// read), so no field name or value of it reaches the caller.
var errNotDescription = eventlog.Invalid("the artifact is not an external work description")

func ExternalDescriptionOf(ctx context.Context, tx pgx.Tx, id string) (ExternalDescription, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT b.body FROM agentrt.artifact_blob b JOIN agentrt.artifact a USING(artifact_id) WHERE a.artifact_id=$1 AND a.artifact_kind='other'`, id).Scan(&raw)
	switch {
	case isNoRows(err):
		return ExternalDescription{}, errNotDescription
	case err != nil:
		return ExternalDescription{}, err
	}
	d, err := ParseExternalDescription(raw)
	if err != nil {
		return ExternalDescription{}, errNotDescription
	}
	return d, nil
}

// ValidateExternalDescription applies the registration's closed schema at
// reporter upload, before the general artifact uploader selects a protocol.
func ValidateExternalDescription(raw []byte) error {
	_, err := ParseExternalDescription(raw)
	return err
}

func ParseExternalDescription(raw []byte) (ExternalDescription, error) {
	var d ExternalDescription
	if len(raw) > artifact.MaxArtifactBytes {
		return d, eventlog.Invalid("external description is at most 1 MiB")
	}
	v, err := canon.Parse(raw)
	if err != nil {
		return d, eventlog.Invalid("invalid JSON: %v", err)
	}
	fields, ok := v.(map[string]any)
	if !ok {
		return d, eventlog.Invalid("external description must be an object")
	}
	// Check the original key set before struct decoding: encoding/json also
	// matches case/Unicode-folded aliases, which could hide invalid values
	// behind a later field while the original bytes are stored unchanged.
	for key := range fields {
		switch key {
		case "v", "kind", "task", "executor", "provider", "repo", "branch", "acceptance_refs", "estimate_minutes":
		default:
			return d, eventlog.Invalid("invalid external description field: %q", key)
		}
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, eventlog.Invalid("invalid fields: %v", err)
	}
	if d.V != 1 || d.Kind != "agentrt.external_work/v1" || len(d.Acceptance) == 0 || len(d.Estimate) != 2 || d.Estimate[0] < 0 || d.Estimate[1] < d.Estimate[0] {
		return d, eventlog.Invalid("invalid external description")
	}
	for _, s := range append([]string{d.Task, d.Executor, d.Provider, d.Repo, d.Branch}, d.Acceptance...) {
		if strings.TrimSpace(s) == "" || len(s) > 2000 {
			return d, eventlog.Invalid("external description needs bounded nonempty text")
		}
	}
	return d, nil
}
func ReadExternal(ctx context.Context, tx pgx.Tx, id string) (ExternalNote, int64, time.Time, error) {
	var n ExternalNote
	var raw []byte
	var event int64
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT body_json->'external_work',event_id,at FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note' AND body_json ? 'external_work' ORDER BY seq DESC LIMIT 1`, "external-work:"+id).Scan(&raw, &event, &at)
	if err == nil {
		err = json.Unmarshal(raw, &n)
	}
	return n, event, at, err
}
func ExternalReply(n ExternalNote, event int64) map[string]any {
	return workregistration.Reply(workregistration.Note(n), event)
}
func AppendExternal(ctx context.Context, tx pgx.Tx, actor string, n ExternalNote) (map[string]any, error) {
	body, err := jsonObject(n)
	if err != nil {
		return nil, err
	}
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "external-work:" + n.ID, Type: "note", Actor: actor, PlanNode: n.Node, Body: map[string]any{"text": "External work " + n.Type, "external_work": body}})
	if err != nil {
		return nil, err
	}
	return ExternalReply(n, ev.EventID), nil
}

// SameIdentityAsWork compares an event's identity with its work's, read
// without the stream lock: once registered, a work's identity never
// changes. A writer whose session is not the one held, and a work that is
// missing or another writer's, are left to the usual path, which answers
// them as it always has; only the writer's own work is compared.
func SameIdentityAsWork(ctx context.Context, db eventlog.DB, w actor.Writer, id string, in ExternalEvent) error {
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if actor.HoldsSessionTx(ctx, tx, w) != nil {
			return nil
		}
		n, _, _, err := ReadExternal(ctx, tx, id)
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

// ExternalEventDigest is an event's request and its digest: the event as
// posted, without its idempotency key.
func ExternalEventDigest(in ExternalEvent) (map[string]any, string, error) {
	req, err := jsonObject(in)
	if err != nil {
		return nil, "", err
	}
	delete(req, "idempotency_key")
	sum, err := canon.Sum(req)
	return req, "sha256:" + sum, err
}

// ErrNotOwnRecord: the reader sees only the records it wrote (a collector,
// R5:5.5-12 §1.3: its external work, the descriptions it uploaded first),
// and this is not one. Another writer's record and a record that does not
// exist answer the same, so the answer says nothing of other writers'.
var ErrNotOwnRecord = errors.New("store: this record is not the caller's")

// ReadOwnExternalWork is ReadExternalWork for a reader held to its own
// records: the work's registrant must be actor (the registrant never
// changes, linked included), or the answer is ErrNotOwnRecord.
func ReadOwnExternalWork(ctx context.Context, db eventlog.DB, id, actor string) (map[string]any, error) {
	var out map[string]any
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		n, event, _, err := ReadExternal(ctx, tx, id)
		switch {
		case isNoRows(err), err == nil && n.Actor != actor:
			return ErrNotOwnRecord
		case err != nil:
			return err
		}
		out = ExternalReply(n, event)
		return nil
	})
	return out, err
}

// ReadExternalWork supports crash recovery. It is read-only and exposes only
// the same bounded metadata shown by the plan snapshot.
func ReadExternalWork(ctx context.Context, db eventlog.DB, id string) (map[string]any, error) {
	var out map[string]any
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		n, event, _, err := ReadExternal(ctx, tx, id)
		if err == nil {
			out = ExternalReply(n, event)
		}
		return err
	})
	return out, err
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func jsonObject(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	out, err := canon.Parse(b)
	if err != nil {
		return nil, err
	}
	return out.(map[string]any), nil
}
