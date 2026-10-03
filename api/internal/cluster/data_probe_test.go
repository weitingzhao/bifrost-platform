package cluster

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// The application in these tests is invented: an order desk whose probe reports "orders" and
// "refunds" activity. Nothing here knows the Bifrost Trade schema, which is the point.

const spaIndex = "<!DOCTYPE html><html><body><div id=\"root\"></div></body></html>"

func orderDeskProbe(lastOrders, lastRefunds string, rows int) string {
	return fmt.Sprintf(`{
  "generated_at": "2031-03-04T14:30:00Z",
  "activity": [
    {"source": "orders", "last_ts": %q},
    {"source": "refunds", "last_ts": %q},
    {"source": "invoices", "last_ts": null, "detail": "missing"}
  ],
  "sample": {"label": "orders", "rows": %d},
  "clone_groups": [
    {"name": "orders", "tables": ["orders", "order_lines", "line_notes", "refunds"], "note": "Orders with everything that references them."},
    {"name": "products", "tables": ["products", "order_lines", "line_notes"]},
    {"name": "broken", "tables": ["products; drop"]},
    {"name": "", "tables": ["orders"]}
  ]
}`, lastOrders, lastRefunds, rows)
}

// probeApp serves body as the data probe on the path the Bifrost gateways route
// (/api/ops/ops/data-probe) and the SPA's index.html everywhere else, like Traefik + nginx do.
func probeApp(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ops/ops/data-probe" && body != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(spaIndex))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// gatewayEntry points the three env gateways at the given URLs and clears the env overrides,
// so a developer's PLATFORM_*_GATEWAY_URL never sends a unit test to a real cluster.
func gatewayEntry(t *testing.T, dev, stg, prod string) *config.ClusterEntry {
	t.Helper()
	for _, k := range []string{"PLATFORM_DEV_GATEWAY_URL", "PLATFORM_STG_GATEWAY_URL", "PLATFORM_PROD_GATEWAY_URL"} {
		t.Setenv(k, "")
	}
	return &config.ClusterEntry{
		DevSmoke:  config.StgSmokeConfig{GatewayURL: dev},
		StgSmoke:  config.StgSmokeConfig{GatewayURL: stg},
		ProdSmoke: config.StgSmokeConfig{GatewayURL: prod},
	}
}

func cloneStoreEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PLATFORM_DATA_CLONE_JOBS_DIR", t.TempDir())
	t.Setenv("PLATFORM_DATA_CLONE_LAST", filepath.Join(t.TempDir(), "last.json"))
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULE", filepath.Join(t.TempDir(), "sched.json"))
}

// noDatabaseReads fails the test if anything is run in the Postgres pod: freshness reads
// only what the application publishes.
func noDatabaseReads(t *testing.T) PodExecFunc {
	return func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		t.Errorf("unexpected pod exec: %s", strings.Join(command, " "))
		return "", fmt.Errorf("no database reads in this test")
	}
}

func TestParseDataProbeRejectsAnSPAIndex(t *testing.T) {
	if _, err := parseDataProbe([]byte(spaIndex), "text/html"); err == nil || !strings.Contains(err.Error(), "not a data-probe response (text/html)") {
		t.Fatalf("want SPA rejection, got %v", err)
	}
	if _, err := parseDataProbe([]byte(`{"status":"ok"}`), "application/json"); err == nil || !strings.Contains(err.Error(), "no activity") {
		t.Fatalf("want rejection of other JSON, got %v", err)
	}
	p, err := parseDataProbe([]byte(orderDeskProbe("2031-03-04T10:00:00Z", "2031-03-01T10:00:00.123456Z", 12)), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if p.Sample == nil || p.Sample.Rows == nil || *p.Sample.Rows != 12 || p.Sample.Label != "orders" {
		t.Fatalf("sample: %+v", p.Sample)
	}
	ts, sources, err := p.latestActivity()
	if err != nil || ts == nil || !ts.Equal(time.Date(2031, 3, 4, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("latest activity: %v %v", ts, err)
	}
	if strings.Join(sources, ",") != "orders,refunds" {
		t.Fatalf("sources %v (a source without last_ts is not one)", sources)
	}
}

func TestFetchDataProbeFallsBackPastTheSPA(t *testing.T) {
	// The first path answers index.html with 200 (SPA fall-through); the second serves the probe.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ops/data-probe" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(orderDeskProbe("2031-03-04T10:00:00Z", "2031-03-04T09:00:00Z", 3)))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(spaIndex))
	}))
	defer srv.Close()
	svc := NewService(gatewayEntry(t, srv.URL+"/", "", ""))
	p, err := svc.fetchDataProbe(context.Background(), "dev")
	if err != nil || p.Sample == nil || *p.Sample.Rows != 3 {
		t.Fatalf("probe %+v err %v", p, err)
	}
}

func TestFetchDataProbeReportsTheApplicationsReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"detail":"data_probe: database unavailable","reason":"read_failed"}`))
	}))
	defer srv.Close()
	svc := NewService(gatewayEntry(t, "", srv.URL, ""))
	_, err := svc.fetchDataProbe(context.Background(), "stg")
	if err == nil || !strings.Contains(err.Error(), "HTTP 503: data_probe: database unavailable") {
		t.Fatalf("want the 503 reason, got %v", err)
	}
	if _, err := svc.fetchDataProbe(context.Background(), "dev"); err == nil || !strings.Contains(err.Error(), "no dev gateway configured") {
		t.Fatalf("want no-gateway error, got %v", err)
	}
}

func TestDataFreshnessReadsActivityFromTheDataProbe(t *testing.T) {
	cloneStoreEnv(t)
	prod := probeApp(t, orderDeskProbe("2031-03-10T12:00:00Z", "2031-03-09T12:00:00Z", 40))
	dev := probeApp(t, orderDeskProbe("2031-03-05T12:00:00Z", "2031-03-01T12:00:00Z", 30)) // 5d behind
	stg := probeApp(t, orderDeskProbe("2031-03-02T00:00:00Z", "2031-03-09T18:00:00Z", 38)) // 0.75d behind
	svc := NewService(gatewayEntry(t, dev.URL, stg.URL, prod.URL))
	svc.SetPodExecForTest(noDatabaseReads(t))

	resp := svc.probeDataFreshness(context.Background())
	got := map[string]DataFreshnessDB{}
	for _, db := range resp.Databases {
		got[db.Name] = db
	}
	if p := got["bifrost_prod"]; p.Verdict != "reference" || p.LastActivityTS == nil || *p.LastActivityTS != "2031-03-10T12:00:00Z" {
		t.Fatalf("prod: %+v", p)
	}
	if d := got["bifrost_dev"]; d.Verdict != "aging" || d.LagVsProdDays == nil || *d.LagVsProdDays != 5 {
		t.Fatalf("dev: %+v", d)
	}
	if s := got["bifrost_stg"]; s.Verdict != "fresh" || s.LagVsProdDays == nil || *s.LagVsProdDays != 0.75 {
		t.Fatalf("stg: %+v", s)
	}
	if strings.Join(got["bifrost_dev"].Sources, ",") != "orders,refunds" {
		t.Fatalf("sources: %v", got["bifrost_dev"].Sources)
	}

	// Clone groups come from the clone source (prod); unusable groups are dropped.
	var names []string
	for _, g := range resp.CloneGroups {
		names = append(names, g.Name)
	}
	if strings.Join(names, ",") != "orders,products" || resp.CloneGroupsDetail != "" {
		t.Fatalf("clone groups %v detail %q", names, resp.CloneGroupsDetail)
	}
	if strings.Join(resp.CloneGroups[0].Tables, ",") != "orders,order_lines,line_notes,refunds" {
		t.Fatalf("orders group tables %v", resp.CloneGroups[0].Tables)
	}
}

func TestDataFreshnessUnknownWhenProdProbeIsDown(t *testing.T) {
	cloneStoreEnv(t)
	dev := probeApp(t, orderDeskProbe("2031-03-05T12:00:00Z", "2031-03-01T12:00:00Z", 30))
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close() // connection refused
	svc := NewService(gatewayEntry(t, dev.URL, dev.URL, downURL))
	svc.SetPodExecForTest(noDatabaseReads(t))

	resp := svc.probeDataFreshness(context.Background())
	for _, db := range resp.Databases {
		if db.Verdict != "unknown" {
			t.Fatalf("%s verdict %s, want unknown (no reference)", db.Name, db.Verdict)
		}
		if db.Name == "bifrost_dev" && (db.LastActivityTS == nil || !strings.Contains(db.Detail, "prod activity unavailable")) {
			t.Fatalf("dev keeps its own activity and says why: %+v", db)
		}
	}
	if len(resp.CloneGroups) != 0 || resp.CloneGroupsDetail != "bifrost_prod data-probe unavailable" {
		t.Fatalf("clone groups %v detail %q", resp.CloneGroups, resp.CloneGroupsDetail)
	}
}

// The double-flywheel test: the platform pointed at another application that publishes no
// data probe (every path answers the SPA, or 404). Everything reads "unknown", nothing errors,
// and no table is queried.
func TestDataFreshnessForAnAppWithoutADataProbe(t *testing.T) {
	cloneStoreEnv(t)
	spa := probeApp(t, "")
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	svc := NewService(gatewayEntry(t, spa.URL, notFound.URL, spa.URL))
	svc.SetPodExecForTest(noDatabaseReads(t))

	resp := svc.probeDataFreshness(context.Background())
	if len(resp.Databases) != 3 {
		t.Fatalf("databases %+v", resp.Databases)
	}
	for _, db := range resp.Databases {
		if db.Verdict != "unknown" || db.LastActivityTS != nil || !strings.Contains(db.Detail, "data-probe unavailable") {
			t.Fatalf("%s: %+v", db.Name, db)
		}
	}
	if resp.CloneGroups == nil || len(resp.CloneGroups) != 0 {
		t.Fatalf("clone groups must be an empty list, got %#v", resp.CloneGroups)
	}
}

func TestVerifyTargetReadsTheSampleFromTheDataProbe(t *testing.T) {
	dev := probeApp(t, orderDeskProbe("2031-03-05T12:00:00Z", "2031-03-01T12:00:00Z", 30))
	svc := NewService(gatewayEntry(t, dev.URL, "", ""))
	var seen []string
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		joined := strings.Join(command, " ")
		seen = append(seen, joined)
		switch {
		case strings.Contains(joined, dataCloneOwnerLeftoverSQL):
			return "0", nil
		case strings.Contains(joined, "information_schema.tables"):
			return "15", nil
		}
		return "", nil
	})
	vr := svc.verifyTarget(context.Background(), "pod", "bifrost_dev", "bifrost_prod")
	if !vr.OK || vr.SampleRows == nil || *vr.SampleRows != 30 || vr.SampleLabel != "orders" || vr.Detail != "15 tables · orders rows=30" {
		t.Fatalf("verify: %+v", vr)
	}
	for _, cmd := range seen {
		if strings.Contains(cmd, "FROM orders") || strings.Contains(cmd, "FROM public.orders") {
			t.Fatalf("verify must not count application tables itself: %q", cmd)
		}
	}
}

func TestVerifyTargetPassesWithAnUnknownSample(t *testing.T) {
	spa := probeApp(t, "")
	svc := NewService(gatewayEntry(t, "", spa.URL, ""))
	svc.SetPodExecForTest(func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
		joined := strings.Join(command, " ")
		switch {
		case strings.Contains(joined, dataCloneOwnerLeftoverSQL):
			return "0", nil
		case strings.Contains(joined, "information_schema.tables"):
			return "15", nil
		}
		return "", nil
	})
	vr := svc.verifyTarget(context.Background(), "pod", "bifrost_stg", "bifrost_prod")
	if !vr.OK || vr.SampleRows != nil || !strings.Contains(vr.Detail, "15 tables · sample unknown: data-probe unavailable") {
		t.Fatalf("verify: %+v", vr)
	}
}

func TestWatchlistSymbolsFromTheDataProbe(t *testing.T) {
	body := `{"generated_at": "2031-03-04T14:30:00Z", "activity": [],
  "watchlist": {"label": "optionable_stocks", "symbols": ["qzbb ", "QZAA", "QZBB", ""], "count": 4}}`
	svc := NewService(gatewayEntry(t, probeApp(t, body).URL, "", ""))
	syms, err := svc.WatchlistSymbols(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(syms, ",") != "QZAA,QZBB" {
		t.Fatalf("symbols %v (want trimmed, upper-cased, distinct, sorted)", syms)
	}
}

func TestWatchlistSymbolsEmptyIsAnAnswer(t *testing.T) {
	body := `{"activity": [], "watchlist": {"label": "optionable_stocks", "symbols": [], "count": 0}}`
	svc := NewService(gatewayEntry(t, "", probeApp(t, body).URL, ""))
	syms, err := svc.WatchlistSymbols(context.Background(), "stg")
	if err != nil || syms == nil || len(syms) != 0 {
		t.Fatalf("want an empty, non-nil list, got %v %v", syms, err)
	}
}

func TestWatchlistSymbolsNeverReadsSilenceAsEmpty(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"probe without the key": {orderDeskProbe("2031-03-04T10:00:00Z", "2031-03-04T09:00:00Z", 3), "no watchlist"},
		"symbols null":          {`{"activity": [], "watchlist": {"label": "x", "symbols": null, "count": null, "detail": "missing"}}`, "watchlist unavailable (missing)"},
		"no probe at all (SPA)": {"", "data-probe unavailable"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			svc := NewService(gatewayEntry(t, "", "", probeApp(t, c.body).URL))
			syms, err := svc.WatchlistSymbols(context.Background(), "prod")
			if err == nil || !strings.Contains(err.Error(), c.want) || syms != nil {
				t.Fatalf("want error containing %q, got %v %v", c.want, syms, err)
			}
		})
	}
}
