package planstore

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// TypeChanged is the plan's bus topic (bus/registry/rocket.yaml): a plan's
// stream took an event, so what GET /agentrt/plan?root= answers may have
// changed. It carries no plan facts; a reader reads the plan when told
// (2026-11-19: the Console read /agentrt/plan 30,831 times in 24 hours while
// the plan last changed on 11-12).
const TypeChanged = "plan.tree.changed"

// ChangedStream is the stream of one root's announcements.
func ChangedStream(root string) string { return "plan-changed:" + root }

// ChangedRef is where a reader reads the plan the announcement is of.
func ChangedRef(root string) string {
	return "/api/v1/agentrt/plan?" + url.Values{"root": {root}}.Encode()
}

// appendPlan appends ev and, when ev goes to a plan's stream, announces that
// plan's change once per transaction. Every write to a plan's stream goes
// through here, so no change goes unannounced.
//
// The announcement is appended before ev, so ev stays the transaction's
// event (eventlog.Append names the last one appended, and the state rows a
// caller writes after must name ev). The plan's stream is locked first, as
// Append would lock it, so every transaction takes the two streams in the
// same order: the plan's, then its announcements'.
func appendPlan(ctx context.Context, tx pgx.Tx, ev eventlog.Event) (eventlog.Appended, error) {
	if root, ok := strings.CutPrefix(ev.Stream, "plan:"); ok && root != "" {
		if err := announce(ctx, tx, root, ev.Actor); err != nil {
			return eventlog.Appended{}, err
		}
	}
	return eventlog.Append(ctx, tx, ev)
}

// announcedKey is the transaction-local setting that says root's change is
// announced in this transaction (a setting name takes letters, digits and
// underscores, and a root may hold others).
func announcedKey(root string) string {
	sum := md5.Sum([]byte(root))
	return "agentrt_plan.announced_" + hex.EncodeToString(sum[:])
}

func announce(ctx context.Context, tx pgx.Tx, root, actor string) error {
	key := announcedKey(root)
	var done string
	if err := tx.QueryRow(ctx, `SELECT coalesce(current_setting($1, true), '')`, key).Scan(&done); err != nil {
		return err
	}
	if done == "1" {
		return nil
	}
	if err := eventlog.LockStream(ctx, tx, PlanStream(root)); err != nil {
		return err
	}
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: ChangedStream(root), Type: TypeChanged, Actor: actor, Body: changedBody(root)}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT set_config($1, '1', true)`, key)
	return err
}

func changedBody(root string) map[string]any {
	return map[string]any{"root": root, "subject": root, "key": root, "ref": ChangedRef(root), "change": "changed"}
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{TypeChanged: {
			field("root", eventlog.Str, false), field("subject", eventlog.Str, false), field("key", eventlog.Str, false),
			field("ref", eventlog.Str, false), field("change", eventlog.Str, false),
		}},
		After: map[string]func(map[string]any) error{TypeChanged: func(body map[string]any) error {
			root, _ := body["root"].(string)
			want := changedBody(root)
			for k, v := range want {
				if body[k] != v || root == "" {
					return eventlog.Invalid("plan.tree.changed: subject, key, ref and change are made of the root")
				}
			}
			return nil
		}},
	})
}
