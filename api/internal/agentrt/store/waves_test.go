package store_test

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestWaveProposal(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	head := func(context.Context, string, string) (string, error) { return commitA, nil }
	wave, err := store.OpenWave(ctx, w.rt, wr("owner"), "bifrost-platform", "root", head)
	ok(t, err)
	p := proposal("wave-task", w.impl)
	p.Wave = wave.ID
	out, err := store.ProposeTasks(ctx, w.rt, wr("owner"), "wave-task", "M-1", []store.TaskProposal{p})
	ok(t, err)
	id := taskIDs(out)[p.Ref]
	var got string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'wave' FROM agentrt.event WHERE task_id=$1 AND event_type='task_proposed'`, id).Scan(&got))
	if got != wave.ID {
		t.Fatalf("wave = %s", got)
	}
	for name, edit := range map[string]func(*store.TaskProposal){"urgent": func(p *store.TaskProposal) { p.Urgent = true }, "repo": func(p *store.TaskProposal) { p.Repo = "other" }, "missing": func(p *store.TaskProposal) { p.Wave = "missing" }} {
		bad := p
		edit(&bad)
		if _, err := store.ProposeTasks(ctx, w.rt, wr("owner"), name, "M-1", []store.TaskProposal{bad}); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	w.legacy(t, fmt.Sprintf("UPDATE agentrt.task SET state='accepted' WHERE task_id='%s'", id))
	if err := store.EndWave(ctx, w.rt, wr("owner"), wave.ID, false); err == nil || !strings.Contains(err.Error(), id) {
		t.Fatalf("pending close: %v", err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "note", Actor: "controller", MissionID: "M-1", TaskID: id, Body: map[string]any{"text": "landed", "landed": commitA}})
		return err
	}))
	ok(t, store.EndWave(ctx, w.rt, wr("owner"), wave.ID, false))
	if _, err := store.ProposeTasks(ctx, w.rt, wr("owner"), "closed", "M-1", []store.TaskProposal{p}); err == nil {
		t.Fatal("closed wave accepted task")
	}
	next, err := store.OpenWave(ctx, w.rt, wr("owner"), "bifrost-platform", "root", head)
	ok(t, err)
	if next.N != 2 {
		t.Fatalf("n=%d", next.N)
	}
	ok(t, store.EndWave(ctx, w.rt, wr("owner"), next.ID, true))
	all, err := store.Waves(ctx, w.rt, "bifrost-platform")
	ok(t, err)
	if len(all) != 2 || all[0].State != "closed" || all[1].State != "abandoned" {
		t.Fatalf("fold=%+v", all)
	}
}
