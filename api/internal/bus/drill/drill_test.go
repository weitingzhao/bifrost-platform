package drill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor" // the seed (the controller actor), in the test only
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
)

var ctx = context.Background()

func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }

// 2026-11-18, the Owner's "first make problems visible": 12 channels never fire
// in normal life and stayed "unproven". Each row that names a drill key in
// problems.yaml must have exactly that drill, and a drill with no row would be
// evidence nobody reads.
func TestEveryDrillKeyOfTheRegisterHasADrillAndBack(t *testing.T) {
	raw, err := os.ReadFile("../nextgen/problems.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Problems []struct {
			ID    string `yaml:"id"`
			Drill string `yaml:"drill"`
		} `yaml:"problems"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	keyed := map[string]bool{}
	for _, p := range doc.Problems {
		if p.Drill != "" {
			keyed[p.ID] = true
			if p.Drill != Stream(p.ID) {
				t.Errorf("%s: drill key %q, the drill writes %q", p.ID, p.Drill, Stream(p.ID))
			}
		}
	}
	have := map[string]bool{}
	for _, d := range drills() {
		have[d.id] = true
		if !keyed[d.id] {
			t.Errorf("%s: drilled but its row names no drill key", d.id)
		}
	}
	for id := range keyed {
		if !have[id] {
			t.Errorf("%s: names a drill key but nothing is drilled", id)
		}
	}
	if len(have) != 12 {
		t.Errorf("%d drills, the register has 12 channels that never fire", len(have))
	}
}

// Same origin: a drill is the quiet value of its type on a subject that says
// "drill", so even a reader that saw one could not take it for a problem.
func TestDrillsAreQuietAndNamedAsDrills(t *testing.T) {
	for _, d := range drills() {
		if !strings.Contains(str(d.body, "subject"), "drill") {
			t.Errorf("%s: subject %q does not say drill", d.id, str(d.body, "subject"))
		}
		switch d.typ {
		case alerts.Type:
			if str(d.body, "status") != "resolved" || str(d.body, "namespace") != "drill" {
				t.Errorf("%s: alert %v is not resolved in namespace drill", d.id, d.body)
			}
		case "env.health.changed", "plugin.status.changed":
			if str(d.body, "change") != "ok" || str(d.body, "source") != "drill" {
				t.Errorf("%s: state %v is not ok from source drill", d.id, d.body)
			}
		case "loop.health.changed":
			if str(d.body, "change") != "healthy" {
				t.Errorf("%s: loop %v is not healthy", d.id, d.body)
			}
		case "data.job.finished":
			if str(d.body, "change") != "succeeded" {
				t.Errorf("%s: job %v did not succeed", d.id, d.body)
			}
		default:
			t.Errorf("%s: type %s is not one the drill is allowed to write", d.id, d.typ)
		}
	}
}

// Same origin, the hard boundary: a drill must wake nobody and push nothing.
// Every subscription row (wake, notify, action) is run against every drill event
// as the dispatcher would, and the alert-letters rule against the alert drills.
func TestNoSubscriptionRowOrLetterIsWokenByADrill(t *testing.T) {
	tenants, err := registry.LoadSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, tn := range tenants {
		for _, row := range tn.Rows {
			rows++
			for _, d := range drills() {
				ev := subscribe.Event{Type: d.typ, Subject: str(d.body, "subject"), Change: str(d.body, "change")}
				if subscribe.Matches(row, ev) {
					t.Errorf("row %s would be woken by the %s drill (%s %s)", row.ID, d.id, ev.Type, ev.Change)
				}
			}
		}
	}
	if rows == 0 {
		t.Fatal("no subscription rows read: the check proves nothing")
	}
	for _, d := range drills() {
		if d.typ != alerts.Type {
			continue
		}
		a := alerts.Alert{Name: str(d.body, "alertname"), Namespace: str(d.body, "namespace"), Severity: str(d.body, "severity"), Status: str(d.body, "status")}
		if key := alerts.RuleOf("alert-firing-to-duty", "bifrost-platform-prod,bifrost-prod").LetterKey("ev-1", a, time.Now(), 7); key != "" {
			t.Errorf("%s: the alert letters would write a letter %q for a drill", d.id, key)
		}
	}
	// Positive control: the same check does fire for a real firing alert and a real failing state.
	for _, ev := range []subscribe.Event{{Type: alerts.Type, Change: "firing"}, {Type: "env.health.changed", Change: "fail"}} {
		hit := false
		for _, tn := range tenants {
			for _, row := range tn.Rows {
				hit = hit || subscribe.Matches(row, ev)
			}
		}
		if !hit {
			t.Errorf("positive control: no row wakes for a real %s %s, so the check above cannot fail", ev.Type, ev.Change)
		}
	}
}

// Same origin: the drills land on their own streams once a day, never on the
// real channel's stream, and the bus's reading of the ledger leaves them out.
func TestPassWritesOnceADayOnDrillStreamsOnly(t *testing.T) {
	db := agentrttest.Migrated(t)
	pool := db.Pool(t, db.Runtime)
	base := time.Now().UTC()
	at := func(d time.Duration) context.Context {
		return eventlog.WithEventClock(ctx, func() time.Time { return base.Add(d) })
	}
	if n, err := Pass(at(0), pool); err != nil || n != 0 {
		t.Fatalf("a ledger without the controller writes nothing: n=%d err=%v", n, err)
	}
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	want := len(drills())
	if n, err := Pass(at(0), pool); err != nil || n != want {
		t.Fatalf("first pass: n=%d err=%v, want %d", n, err, want)
	}
	if n, err := Pass(at(Every-time.Hour), pool); err != nil || n != 0 {
		t.Fatalf("inside a day: n=%d err=%v", n, err)
	}
	if n, err := Pass(at(Every+time.Hour), pool); err != nil || n != want {
		t.Fatalf("a day later: n=%d err=%v, want %d", n, err, want)
	}
	var off int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id NOT LIKE 'drill:%'
	  AND event_type IN ('alert.rule.fired','env.health.changed','plugin.status.changed','loop.health.changed','data.job.finished')`).Scan(&off); err != nil || off != 0 {
		t.Fatalf("%d drill-type events outside drill: streams (err %v)", off, err)
	}
	var all int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id LIKE 'drill:%'`).Scan(&all); err != nil || all != 2*want {
		t.Fatalf("%d events on drill streams, want %d (err %v)", all, 2*want, err)
	}
	// The filter the relay, the gate and the observer read through leaves them out
	// (LatestEvents shares its SQL with EventsAfter and is not held back by other
	// transactions' horizon, which made a first try on EventsAfter flaky under
	// load); the same read without the filter shows them (positive control).
	read := func(f eventlog.EventFilter) int {
		n := 0
		if err := eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
			doc, _, err := eventlog.LatestEvents(ctx, tx, 0, 500, f)
			n = strings.Count(string(doc), StreamPrefix)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := read(eventlog.EventFilter{}); n == 0 {
		t.Fatal("positive control: an unfiltered read shows no drill event, so the hidden read proves nothing")
	}
	if n := read(eventlog.EventFilter{HiddenPrefixes: []string{StreamPrefix}}); n != 0 {
		t.Fatalf("%d drill streams in the bus's reading", n)
	}
}

// Same origin: no ordinary caller may write a drill, and the bus's one ledger
// read must hide them. Only the server's wiring imports this package, and every
// ledger read it hands the relay and the gate is the one that hides the streams.
func TestOnlyTheWiringUsesTheDrillAndTheBusReadHidesIt(t *testing.T) {
	root := "../.."
	var wired []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		inDrill := strings.HasPrefix(filepath.ToSlash(p), "../../bus/drill/")
		if !inDrill && strings.Contains(string(src), `internal/bus/drill"`) {
			wired = append(wired, filepath.ToSlash(p))
		}
		if filepath.Base(filepath.Dir(p)) == "server" {
			for _, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, "EventsAfter:") && !strings.Contains(line, "busEventsAfter") && !strings.Contains(line, "relayEventsAfter") {
					t.Errorf("%s: a ledger read for the bus that is not busEventsAfter: %s", p, strings.TrimSpace(line))
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range wired {
		if !strings.HasPrefix(p, "../../server/") {
			t.Errorf("%s imports bus/drill; only the server's wiring may", p)
		}
	}
	if len(wired) == 0 {
		t.Fatal("nothing wires the drill")
	}
	src, err := os.ReadFile("../../server/bus_wire.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "HiddenPrefixes: []string{drill.StreamPrefix}") {
		t.Error("busEventsAfter must hide drill.StreamPrefix streams from the relay, the gate and the observer")
	}
}
