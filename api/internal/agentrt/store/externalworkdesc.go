package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
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
type externalNote workregistration.Note

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
	if len(raw) > artifact.MaxArtifactBytes {
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
	return workregistration.Reply(workregistration.Note(n), event)
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
