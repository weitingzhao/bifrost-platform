package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// connEndpoint carries which endpoint a connection was sent to.
type connEndpoint struct{}

// service is a Service in front of API instances: each new connection goes to
// the next endpoint in turn (kube-proxy's round robin), and an endpoint
// answers /health with agentrt_ready as ready says at that moment. It counts
// the connections it took.
func service(t *testing.T, endpoints int, ready func(endpoint int) bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var conns atomic.Int64
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "agentrt_ready": ready(r.Context().Value(connEndpoint{}).(int))})
	}))
	s.Config.ConnContext = func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, connEndpoint{}, int(conns.Add(1)-1)%endpoints)
	}
	s.Start()
	t.Cleanup(s.Close)
	return s, &conns
}

func probeOf(url string) func(context.Context) error {
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	return func(ctx context.Context) error { return serviceTakesCalls(ctx, client, url, probeConnections) }
}

// Review 529 A-2: a pass asks the Service probeConnections times at once,
// each on a connection of its own; one ready endpoint among unready ones is
// enough; only every probe failing is no, said with each reason once.
func TestTheProbeAsksOnFreshConnections(t *testing.T) {
	s, _ := service(t, 2, func(e int) bool { return e == 0 })
	if err := probeOf(s.URL + "/health")(context.Background()); err != nil {
		t.Fatalf("one endpoint of two ready: %v", err)
	}
	none, _ := service(t, 2, func(int) bool { return false })
	err := probeOf(none.URL + "/health")(context.Background())
	if err == nil || !strings.Contains(err.Error(), "none of 5 probes") || strings.Count(err.Error(), "not ready") != 1 {
		t.Fatalf("no endpoint ready: %v", err)
	}
	var fresh atomic.Int64
	counted := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"agentrt_ready": false})
	}))
	counted.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			fresh.Add(1)
		}
	}
	counted.Start()
	defer counted.Close()
	_ = probeOf(counted.URL + "/health")(context.Background())
	if n := fresh.Load(); n != probeConnections {
		t.Fatalf("%d connections for a pass's %d probes", n, probeConnections)
	}
}

// presenceDB is a runtime database with host h1 and the chief of staff's one
// candidate, exec:claude, which h1 offers.
func presenceDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), db.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx := context.Background()
	if err = store.RegisterHost(ctx, pool, "owner", "h1", "dev"); err != nil {
		t.Fatal(err)
	}
	if err = store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the chief of staff's job"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('chief_of_staff', 'dev', 'job:test', 1)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, model_pattern,
		  status, rule_id, rule_version) VALUES ('chief_of_staff/exec:claude', 'chief_of_staff', 'exec:claude', 'anthropic', '*', 'active', 'job:test', 1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordHostOffered(ctx, pool, "h1", []store.Tool{{Executor: "exec:claude", Provider: "anthropic", Model: "opus"}}, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

// Review 529 A-2, through a Service of two API instances, h1 having really
// stopped at t0 and no other host claiming: with one instance ready and the
// other never, or with the two ready by turns, h1 is told within 35 s of its
// last poll; with neither ready the platform is out, nothing is told of h1,
// and past two minutes the Owner is told host presence cannot be judged.
func TestPresenceThroughAPartlyReadyService(t *testing.T) {
	var pass atomic.Int64
	for _, c := range []struct {
		name  string
		ready func(endpoint int) bool
		out   bool
	}{
		{"one ready, one never", func(e int) bool { return e == 0 }, false},
		{"ready by turns", func(e int) bool { return int(pass.Load())%2 == e }, false},
		{"neither ready", func(int) bool { return false }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			pool := presenceDB(t)
			s, _ := service(t, 2, c.ready)
			var t0 time.Time
			if err := pool.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&t0); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(context.Background(), `UPDATE agentrt.host SET last_poll_at = $1 WHERE host_id = 'h1'`, t0); err != nil {
				t.Fatal(err)
			}
			now := t0
			ctl := &controller.Controller{DB: pool, Now: func() time.Time { return now }, Started: t0, PlatformUp: probeOf(s.URL + "/health")}
			var told time.Time
			for now = t0; now.Before(t0.Add(3 * time.Minute)); now = now.Add(controller.PresenceEvery) {
				pass.Add(1)
				n, err := ctl.Presence(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if n > 0 && told.IsZero() {
					told = now
				}
			}
			var unjudged int
			if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'policy_alert'`,
				store.PresenceStream).Scan(&unjudged); err != nil {
				t.Fatal(err)
			}
			switch {
			case c.out && (!told.IsZero() || unjudged != 1):
				t.Fatalf("no instance ready: told at %v, %d cannot-be-judged alerts", told.Sub(t0), unjudged)
			case !c.out && (told.IsZero() || told.Sub(t0) <= store.HostLostAfter || told.Sub(t0) > 35*time.Second || unjudged != 0):
				t.Fatalf("h1 told %v after its last poll; %d cannot-be-judged alerts", told.Sub(t0), unjudged)
			}
		})
	}
}
