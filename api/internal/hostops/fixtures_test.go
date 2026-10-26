package hostops

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

var testNow = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
var testDigest = Digest([]byte("test-only handler"))

func metadata(id string) Metadata {
	return Metadata{1, id, 1, "test", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
}
func testRule() Rule {
	return Rule{ID: "test-rule", Version: 1, Direction: "initial", SchemaVersion: 1, Environment: "test", EffectiveAt: "2026-10-25T00:00:00Z", ExpiresAt: "2026-11-24T00:00:00Z", InventoryDigest: testDigest, CatalogDigest: testDigest, ExecutorIDs: []string{"exec-a", "exec-b"},
		Clauses: []Clause{{ID: "A", QuotaID: "restart-quota", Action: "host_service_restart", Hosts: []string{"host-a", "host-b"}, ExecutorIDs: []string{"exec-a", "exec-b"}, Params: map[string][]string{"service": {"exporter", "other"}}, HandlerDigest: testDigest, BundleDigests: []string{}, Conditions: []string{"identity_matches"}, TimeoutS: 120, WatchdogS: 120, Rollback: "restore_previous_service_state", MaxPerDay: 4, MaxPerWeek: 12, RepeatedSuccess: RepeatedSuccess{3600, 3}}},
		Limits:  Limits{1, 1, 0, 1}, RemindersDays: []int{14, 3, 1}, Digest: Calendar{"America/Chicago", "monday", "09:00"}}
}
func testRequest() Request {
	return Request{"host_service_restart", map[string]string{"service": "exporter"}, "host-a", "test", "exec-a", testDigest, []string{}, "work-584", "test-thread"}
}
func testFacts() Facts {
	return Facts{InventoryDigest: testDigest, CatalogDigest: testDigest, Conditions: map[string]bool{"identity_matches": true, "config_unchanged": true}, Fresh: true, AuditDurable: true, NotificationHealthy: true, RollbackAvailable: true, LockAvailable: true, ClockSyncedAt: testNow, History: []Event{}}
}
func testState(p *Policy) SafetyState {
	return SafetyState{Present: true, Fresh: true, Environment: "test", Epoch: 1, MaxEpoch: 1, MaxRuleVersion: p.version, PolicyHead: p.head, Generation: 1}
}
func signed(t testing.TB, k rptest.Key, v any, ns string) Signed {
	t.Helper()
	raw := canonical(v)
	return Signed{string(raw), k.Sign(t, raw, ns)}
}
func baseline(t testing.TB, rules []Rule) (Trust, rptest.Key, PolicySet, *Policy) {
	t.Helper()
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	b := PolicyBundle{metadata("test-policy"), "", map[string]string{}, rules}
	s := PolicySet{signed(t, k, b, NamespacePolicy), []Tightening{}}
	p, e := tr.AdmitBaseline(s.Baseline, nil, "test", testNow)
	if e != nil {
		t.Fatal(e)
	}
	return tr, k, s, p
}
func cloneRule(r Rule) Rule {
	var out Rule
	if err := Decode(canonical(r), &out); err != nil {
		panic(err)
	}
	return out
}
func tighten(t testing.TB, tr Trust, s PolicySet, p *Policy, n Rule) (PolicySet, *Policy) {
	t.Helper()
	d := Tightening{p.head, p.RuleDigests()[n.ID], p.version + 1, n}
	s.Chain = append(append([]Tightening{}, s.Chain...), d)
	next, e := tr.Load(s, Digest(canonical(d)), d.Version)
	if e != nil {
		t.Fatal(e)
	}
	return s, next
}
func event(outcome string) Event {
	return Event{"execution-old", []string{"restart-quota"}, "host-a", "host_service_restart", testNow.Add(-time.Minute).Format(time.RFC3339), outcome, true}
}

func inventory() Inventory {
	return Inventory{Meta: metadata("test-inventory"), Hosts: []Host{{"host-a", "test", "linux", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "02:00:00:00:00:01", "192.0.2.1", "out-of-cluster", 1, []string{"exec-a"}}}, Objects: []Object{{ID: "exporter", Host: "host-a", Kind: "service", Canonical: "exporter", Aliases: []string{"com.example.exporter"}, ConfigDigest: testDigest, Dependencies: []string{}, Actions: []string{"host_service_restart", "host_status", "host_logs", "host_compose_reconcile", "host_monitoring_install"}, MaintenanceScope: []string{}, Files: []string{}, Ports: []int{}, Methods: []string{}, Repositories: map[string]string{}}}, ProtectedIDs: []string{}}
}
func catalog() SafetyCatalog {
	return SafetyCatalog{testDigest, map[string]string{"exporter": "exporter", "other": "other", "com.example.exporter": "exporter", "daemon": "daemon", "bifrost-engine": "daemon", "bifrost-engine.service": "daemon", "trading_engine": "daemon", "bifrost-host-exec": "bifrost-host-exec", "com.example.host-exec": "bifrost-host-exec", "k3s": "k3s", "k3s-agent": "k3s-agent", "containerd": "containerd", "docker": "docker", "postgresql": "postgresql", "sshd": "sshd", "systemd-networkd": "systemd-networkd", "org.example.sshd": "sshd"}, []string{"daemon"}, []string{"bifrost-host-exec"}}
}
func testPlan() Plan {
	return Plan{SchemaVersion: 1, Action: "host_service_restart", Params: map[string]string{"service": "exporter"}, Target: Target{"host-a", "test", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "linux", 1}, BeforeDigest: testDigest, ArtifactDigests: []string{}, InventoryDigest: testDigest, CatalogDigest: testDigest, HandlerDigest: testDigest, ExecutionID: "execution-h", ApprovalID: "approval-h", Nonce: "00000000000000000000000000000001", EffectiveAt: "2026-10-26T11:00:00Z", ExpiresAt: "2026-10-26T13:00:00Z", ExecutorID: "exec-a", FreezeEpoch: 1, RecoveryGeneration: 1, FencingGeneration: 1, Rollback: Rollback{"restore", map[string]string{"state": "running"}, []string{}, "2026-10-26T14:00:00Z", 120, []Effect{{"exporter", "restart", testDigest}}}, Locks: []string{"host:host-a", "dependency:metrics"}, Effects: []Effect{{"exporter", "restart", testDigest}}}
}
func planContext(p Plan) PlanContext {
	return PlanContext{Target: p.Target, ExecutorID: p.ExecutorID, ExecutionID: p.ExecutionID, ApprovalID: p.ApprovalID, BeforeDigest: p.BeforeDigest, InventoryDigest: p.InventoryDigest, HandlerDigest: p.HandlerDigest, ArtifactDigests: p.ArtifactDigests, Rollback: p.Rollback, Effects: p.Effects, Locks: p.Locks, FencingGeneration: 1, State: SafetyState{Present: true, Fresh: true, Environment: "test", Epoch: 1, MaxEpoch: 1, MaxRuleVersion: 1, PolicyHead: testDigest, Generation: 1}, Catalog: catalog(), PreinstalledActions: []string{"host_service_restart", "host_access_change"}}
}
