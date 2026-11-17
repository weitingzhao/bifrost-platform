package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
)

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
		// Nothing proposes a task any more (store cut 4, 2026-11-14).
		return invalid("external work is not linked to a task any more: register it on its step")
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
func RegisterExternalWork(ctx context.Context, db DB, w Writer, in externalwork.ExternalRegistration) (map[string]any, error) {
	collector, err := collectorWriter(w)
	if err != nil {
		return nil, err
	}
	if err := externalwork.CheckIdentity(collector, in.Vendor, in.Session, in.Epoch, in.Sidebar); err != nil {
		return nil, err
	}
	if collector {
		// The run key is the identity's; a client may leave it out.
		run := externalwork.IdentityRunKey(in.Vendor, in.Session, in.Epoch)
		if in.RunKey != "" && in.RunKey != run {
			return nil, invalid("a collector's source_run_key is <vendor>:<session>:<epoch>, %s", run)
		}
		in.RunKey = run
	}
	if in.Key == "" || in.Root == "" || in.Node == "" || in.RunKey == "" || len(in.RunKey) > 200 || !externalwork.DigestPattern.MatchString(in.Artifact) {
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
		if _, err := externalwork.ExternalDescriptionOf(ctx, tx, in.Artifact); err != nil {
			return nil, err
		}
		if err := lockStream(ctx, tx, "external-work:"+id); err != nil {
			return nil, err
		}
		prev, event, _, err := externalwork.ReadExternal(ctx, tx, id)
		if err == nil {
			if prev.RegistrationDigest != sum {
				return nil, ErrConflict
			}
			return externalwork.ExternalReply(prev, event), nil
		}
		if !isNoRows(err) {
			return nil, err
		}
		now := timeText(eventNow(ctx))
		n := externalwork.ExternalNote{V: 1, Kind: "agentrt.external_work/v1", ID: id, Actor: w.Actor, Root: in.Root, Node: in.Node, RunKey: in.RunKey, Artifact: in.Artifact, Mission: in.Mission, Task: in.Task, Type: "registered", State: "Registered", RequestDigest: sum, RegistrationDigest: sum, ObservedAt: in.ObservedAt, SourceStartedAt: in.ObservedAt, RegisteredAt: now, HeartbeatAt: now}
		if collector {
			n.Host, n.Vendor, n.Session, n.Epoch, n.Sidebar = w.Host, in.Vendor, in.Session, in.Epoch, in.Sidebar
		}
		return externalwork.AppendExternal(ctx, tx, w.Actor, n)
	})
}

// PostExternalEvent appends one event to an external work. An event that
// names its seq is the next one (the work's last seq + 1), or the replay of
// one already recorded under that seq. An event that leaves seq out gets the
// next one under the stream's lock, so two of them posted at once get two
// seqs in turn; its replay is by its idempotency key alone.
func PostExternalEvent(ctx context.Context, db DB, w Writer, id string, in externalwork.ExternalEvent) (map[string]any, error) {
	collector, err := collectorWriter(w)
	if err != nil {
		return nil, err
	}
	if err := externalwork.CheckIdentity(collector, in.Vendor, in.Session, in.Epoch, ""); err != nil {
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
		if s != "" && !externalwork.DigestPattern.MatchString(s) {
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
	req, sum, err := externalwork.ExternalEventDigest(in)
	if err != nil {
		return nil, err
	}
	// A replay under a used key with another identity would meet the key's
	// digest first and answer key_conflict, so a collector could not tell a
	// reused key from a changed identity (TD-516). The identity is the work's
	// for good once registered, so it is compared here, before the key is
	// looked at; the check under the stream lock below stays the authority.
	if collector {
		if err := externalwork.SameIdentityAsWork(ctx, db, w, id, in); err != nil {
			return nil, err
		}
	}
	return idempotentAs(ctx, db, w, "external-work/"+id+"/events", in.Key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		// Read binding before locking; retry a concurrent rebind without taking
		// roots after the stream lock (the registration lock order).
		before, _, _, err := externalwork.ReadExternal(ctx, tx, id)
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
		n, _, _, err := externalwork.ReadExternal(ctx, tx, id)
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
				return nil, fmt.Errorf("%w: this work was registered without one", externalwork.ErrIdentityMismatch)
			case in.Vendor != n.Vendor || in.Session != n.Session || in.Epoch != n.Epoch:
				return nil, externalwork.ErrIdentityMismatch
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
			if _, sum, err = externalwork.ExternalEventDigest(next); err != nil {
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
			var previous externalwork.ExternalNote
			if err = json.Unmarshal(old, &previous); err != nil {
				return nil, err
			}
			if previous.RequestDigest != sum {
				return nil, ErrConflict
			}
			return externalwork.ExternalReply(previous, event), nil
		}
		if seq != n.Seq+1 {
			return nil, fmt.Errorf("%w: expected seq %d", ErrConflict, n.Seq+1)
		}
		if (n.State == "Submitted" || n.State == "Cancelled") && in.Type != "linked" {
			return nil, ErrState
		}
		now := timeText(eventNow(ctx))
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
		return externalwork.AppendExternal(ctx, tx, w.Actor, n)
	})
}

// collectorWriter checks a writer's binding: a host-bound writer is that
// host's collector; a writer that is not host-bound is no collector.
func collectorWriter(w Writer) (bool, error) {
	switch {
	case w.Host != "" && w.Actor != externalwork.CollectorActorPrefix+w.Host:
		return false, invalid("a host-bound writer is its host's collector")
	case w.Host == "" && strings.HasPrefix(w.Actor, externalwork.CollectorActorPrefix):
		return false, invalid("a collector writes bound to its host")
	}
	return w.Host != "", nil
}

func checkObserved(s string) error {
	if s != "" {
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return invalid("observed_at must be RFC3339")
		}
	}
	return nil
}

// maxExternalCommits is the most commits one event names, and the most a
// work's note keeps.
const maxExternalCommits = 100

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
