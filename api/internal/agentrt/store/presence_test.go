package store_test

import (
	"encoding/json"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// presenceWorld is a world with the hosts named, none polled yet, and claude
// and codex candidates of every job (tenant dev).
func presenceWorld(t *testing.T, hosts ...string) *world {
	t.Helper()
	w := newWorld(t)
	for _, h := range hosts {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", h, "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.jobs(t, "active", "*", codex, claude)
	return w
}

// presenceDoc is the part of Progress presence adds.
type presenceDoc struct {
	Hosts []struct {
		HostID          string        `json:"host_id"`
		Online          bool          `json:"online"`
		ReportedOffline bool          `json:"reported_offline"`
		Pruning         bool          `json:"pruning"`
		Claiming        *bool         `json:"claiming"`
		DiskLow         bool          `json:"disk_low"`
		Offered         []store.Offer `json:"offered"`
		OfferedAt       *string       `json:"offered_at"`
	} `json:"hosts"`
	Executors []store.ExecutorPresence `json:"executors"`
	Unserved  []string                 `json:"jobs_without_executor"`
}

func (w *world) presence(t *testing.T) (presenceDoc, []byte) {
	t.Helper()
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.MarshalIndent(out, "", "  ")
	ok(t, err)
	var doc presenceDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc, raw
}

// What a host offers, and whether its free disk is below the floor, is
// recorded once a change: its first claim, and a claim that offers something
// else or reports the disk on the other side of the floor; the order of its
// preference is no change. GET /progress says the latest.
func TestAHostsOffersAreRecordedOnceAChange(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	for i, step := range []struct {
		tools []store.Tool
		low   bool
		want  bool
	}{{[]store.Tool{claude, codex}, false, true}, {[]store.Tool{codex, claude}, false, false}, {[]store.Tool{claude}, false, true},
		{[]store.Tool{claude}, false, false}, {[]store.Tool{claude}, true, true}, {[]store.Tool{claude}, true, false},
		{nil, true, true}, {nil, true, false}} {
		got, err := store.RecordHostOffered(ctx, w.rt, "h1", step.tools, step.low)
		ok(t, err)
		if got != step.want {
			t.Fatalf("step %d: recorded %v", i, got)
		}
	}
	doc, _ := w.presence(t)
	if len(doc.Hosts) != 1 || doc.Hosts[0].Offered == nil || len(doc.Hosts[0].Offered) != 0 || doc.Hosts[0].OfferedAt == nil || !doc.Hosts[0].DiskLow {
		t.Fatalf("hosts %+v", doc.Hosts)
	}
	if n := w.lastSeq(t, store.HostOffersStream("h1")); n != 4 {
		t.Fatalf("%d records of what h1 offers", n)
	}
}
