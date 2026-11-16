package relay

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// The relay is tested against the two interfaces it reads and writes (Ledger,
// Out), not against a NATS server: an embedded nats-server would add
// github.com/nats-io/nats-server/v2 and its dependencies to the module for
// tests alone. JSOut, the NATS side of Out, is not exercised here.

func rocket(t *testing.T) registry.Tenant {
	t.Helper()
	all, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	return all[Tenant]
}

func gitRow(id, txid int64, repo, commit string) Row {
	return Row{EventID: id, TxID: txid, Stream: "git:" + repo, Type: "git.main.advanced", Actor: "service:gate-git",
		At: time.Date(2026, 11, 16, 18, 0, 0, 123456000, time.UTC),
		Body: map[string]any{"repo": repo, "commit": commit, "before": "", "subject": repo + "@" + commit,
			"ref": "/api/v1/delivery/context?repo=" + repo, "key": repo, "change": "pushed"}}
}

// FakeLedger holds rows in cursor order and a horizon, as EventsAfter reads.
type fakeLedger struct {
	mu      sync.Mutex
	rows    []Row
	horizon int64
	reads   int
}

func (l *fakeLedger) add(r Row) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows = append(l.rows, r)
	if r.TxID >= l.horizon {
		l.horizon = r.TxID + 1
	}
}

func (l *fakeLedger) After(_ context.Context, after Cursor, limit int, types []string) ([]Row, Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reads++
	var out []Row
	for _, r := range l.rows {
		if after.Less(r.Cursor()) && r.TxID < l.horizon && slices.Contains(types, r.Type) && len(out) < limit {
			out = append(out, r)
		}
	}
	switch {
	case len(out) == limit:
		return out, out[len(out)-1].Cursor(), nil
	case l.horizon > after.TxID:
		return out, Cursor{TxID: l.horizon}, nil
	}
	return out, after, nil
}

func (l *fakeLedger) Horizon(context.Context) (Cursor, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return Cursor{TxID: l.horizon}, nil
}

func (l *fakeLedger) Event(_ context.Context, id int64) (Row, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.rows {
		if r.EventID == id {
			return r, true, nil
		}
	}
	return Row{}, false, nil
}

type published struct {
	subject, id string
	cursor      Cursor
	data        []byte
}

type fakeOut struct {
	msgs []published
	fail error
}

func (o *fakeOut) LastCursor(context.Context) (Cursor, bool, error) {
	if len(o.msgs) == 0 {
		return Cursor{}, false, nil
	}
	return o.msgs[len(o.msgs)-1].cursor, true, nil
}

func (o *fakeOut) Publish(_ context.Context, subject, id string, c Cursor, data []byte) error {
	if o.fail != nil {
		return o.fail
	}
	o.msgs = append(o.msgs, published{subject, id, c, data})
	return nil
}

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("b", 40)
)

// 2026-11-16: bus step 1 (RULES.md §2): the envelope carries the standard
// attributes and the extensions, points at the state and carries no data.
func TestBuildEnvelope(t *testing.T) {
	ten := rocket(t)
	row := gitRow(42, 9001, "bifrost-platform", shaA)
	row.Cause = 7
	e, data, err := Build(row, ten.Channels["git.main.advanced"], Tenant, "stg")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"specversion": "1.0", "id": "ev-42", "source": "bifrost://rocket/stg/gate-git", "type": "git.main.advanced",
		"time": "2026-11-16T18:00:00.123456Z", "subject": "bifrost-platform@" + shaA, "tenant": "rocket", "env": "stg",
		"cursor": "9001.42", "ref": "/api/v1/delivery/context?repo=bifrost-platform", "actor": "service:gate-git",
		"key": "bifrost-platform", "change": "pushed", "causeid": "ev-7",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if _, has := m["data"]; has || len(m) != len(want) {
		t.Errorf("the envelope has other attributes than §2's: %v", m)
	}
	if len(data) > MaxEnvelope || e.Cursor != row.Cursor().String() {
		t.Errorf("%d bytes, cursor %s", len(data), e.Cursor)
	}
	if c, ok := ParseCursor(e.Cursor); !ok || c != (Cursor{9001, 42}) {
		t.Errorf("the cursor %q does not read back", e.Cursor)
	}
	if n, ok := ParseEventID(e.ID); !ok || n != 42 {
		t.Errorf("the id %q does not read back", e.ID)
	}
}

// 2026-11-16: bus step 1 (RULES.md §2): an envelope over 2 KiB, or one
// without the subject, ref or key its type promises, is refused, not cut.
func TestBuildRefuses(t *testing.T) {
	ch := rocket(t).Channels["git.main.advanced"]
	big := gitRow(1, 1, "r", shaA)
	big.Body["subject"] = strings.Repeat("x", 2100)
	noKey := gitRow(2, 1, "r", shaA)
	delete(noKey.Body, "key")
	noRef := gitRow(3, 1, "r", shaA)
	noRef.Body["ref"] = "https://elsewhere"
	for name, row := range map[string]Row{"over 2 KiB": big, "no key": noKey, "no platform ref": noRef} {
		if _, _, err := Build(row, ch, Tenant, "stg"); err == nil {
			t.Errorf("%s: built", name)
		}
	}
	if _, _, err := Build(gitRow(4, 1, "r", shaA), ch, Tenant, "STG"); err == nil {
		t.Error("an environment that is not a subject segment: built")
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): the relay resumes from the cursor of
// the stream's last message, the record of what was published.
func TestRelayResumesFromTheStreamsLastMessage(t *testing.T) {
	l := &fakeLedger{}
	l.add(gitRow(10, 100, "bifrost-platform", shaA))
	l.add(gitRow(11, 101, "bifrost-platform", shaB))
	out := &fakeOut{msgs: []published{{cursor: Cursor{100, 10}}}}
	r := &Relay{Ledger: l, Out: out, Tenant: rocket(t), Env: "stg"}
	if err := r.Resume(context.Background()); err != nil || r.Cursor() != (Cursor{100, 10}) {
		t.Fatalf("resumed at %s, %v", r.Cursor(), err)
	}
	n, err := r.Pass(context.Background())
	if err != nil || n != 1 || len(out.msgs) != 2 {
		t.Fatalf("published %d, %v, %d messages", n, err, len(out.msgs))
	}
	got := out.msgs[1]
	if got.id != "ev-11" || got.cursor != (Cursor{101, 11}) || got.subject != "ev.rocket.stg.git.main.advanced" {
		t.Fatalf("published %+v", got)
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): an empty stream starts at the
// ledger's horizon; history is not replayed.
func TestRelayOnAnEmptyStreamStartsAtTheHorizon(t *testing.T) {
	l := &fakeLedger{}
	l.add(gitRow(10, 100, "bifrost-platform", shaA))
	out := &fakeOut{}
	r := &Relay{Ledger: l, Out: out, Tenant: rocket(t), Env: "stg"}
	if err := r.Resume(context.Background()); err != nil || r.Cursor() != (Cursor{TxID: 101}) {
		t.Fatalf("resumed at %s, %v", r.Cursor(), err)
	}
	if n, err := r.Pass(context.Background()); err != nil || n != 0 {
		t.Fatalf("history replayed: %d, %v", n, err)
	}
	l.add(gitRow(12, 102, "bifrost-platform", shaB))
	if n, err := r.Pass(context.Background()); err != nil || n != 1 || out.msgs[0].id != "ev-12" {
		t.Fatalf("the new event: %d, %v, %+v", n, err, out.msgs)
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): the cursor moves only after the
// stream acknowledged; a refused publish leaves it where it was.
func TestRelayAdvancesOnlyAfterTheAck(t *testing.T) {
	l := &fakeLedger{}
	out := &fakeOut{}
	r := &Relay{Ledger: l, Out: out, Tenant: rocket(t), Env: "stg"}
	if err := r.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := r.Cursor()
	l.add(gitRow(20, 200, "bifrost-platform", shaA))
	out.fail = errors.New("no PubAck")
	if _, err := r.Pass(context.Background()); err == nil || r.Cursor() != before {
		t.Fatalf("after a failed publish: cursor %s (was %s), %v", r.Cursor(), before, err)
	}
	out.fail = nil
	if n, err := r.Pass(context.Background()); err != nil || n != 1 || !before.Less(r.Cursor()) {
		t.Fatalf("after the retry: %d, %v, cursor %s", n, err, r.Cursor())
	}
}

// 2026-11-16: bus step 1 (RULES.md §7): the stream behind an event the relay
// passed is a drift; the relay goes back to publish again. An event not yet
// published is not one (positive control of the drift).
func TestRelayReconcileFindsALostMessage(t *testing.T) {
	l := &fakeLedger{}
	out := &fakeOut{}
	r := &Relay{Ledger: l, Out: out, Tenant: rocket(t), Env: "stg"}
	if err := r.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.add(gitRow(30, 300, "bifrost-platform", shaA))
	if drift, err := r.Reconcile(context.Background()); drift || err != nil {
		t.Fatalf("an event not yet published is a drift: %v %v", drift, err)
	}
	if _, err := r.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.add(gitRow(31, 301, "bifrost-platform", shaB))
	if _, err := r.Pass(context.Background()); err != nil || len(out.msgs) != 2 {
		t.Fatal(err, len(out.msgs))
	}
	out.msgs = out.msgs[:1] // NATS lost ev-31
	if drift, err := r.Reconcile(context.Background()); !drift || err != nil || r.Cursor() != (Cursor{300, 30}) {
		t.Fatalf("drift %v %v, cursor %s", drift, err, r.Cursor())
	}
	if n, err := r.Pass(context.Background()); err != nil || n != 1 || out.msgs[1].id != "ev-31" {
		t.Fatalf("published again: %d %v", n, err)
	}
}
