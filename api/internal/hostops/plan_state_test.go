package hostops

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func TestFreezeUnfreezeAndReplay(t *testing.T) {
	tr, k, _, p := baseline(t, []Rule{testRule()})
	s := testState(p)
	frozen, e := s.Freeze(true, true)
	if e != nil {
		t.Fatal(e)
	}
	if frozen.Check("test", p.version, p.head) == nil {
		t.Fatal("freeze did not stop writes")
	}
	u := Unfreeze{1, "test", frozen.Epoch, frozen.Generation, p.version, p.head, "test-only thaw", "2026-10-25T00:00:00Z", "2026-11-24T00:00:00Z"}
	sig := signed(t, k, u, NamespaceUnfreeze)
	n, e := tr.Unfreeze(frozen, sig, p, testNow)
	if e != nil || n.Check("test", p.version, p.head) != nil {
		t.Fatal("valid unfreeze", e)
	}
	if _, e := tr.Unfreeze(n, sig, p, testNow); e == nil {
		t.Fatal("replayed unfreeze")
	}
	frozen2, _ := n.Freeze(true, false)
	if _, e := tr.Unfreeze(frozen2, sig, p, testNow); e == nil {
		t.Fatal("old epoch thaw")
	}
	if _, e := s.Freeze(false, false); e == nil {
		t.Fatal("unauthenticated freeze")
	}
	for _, bad := range []SafetyState{{}, {Present: true, Fresh: true, Environment: "test", Epoch: 1, MaxEpoch: 2, Generation: 1, MaxRuleVersion: 1, PolicyHead: p.head}} {
		if bad.Check("test", p.version, p.head) == nil {
			t.Fatal("missing/rolled-back state")
		}
		if _, e := tr.Unfreeze(bad, sig, p, testNow); e == nil {
			t.Fatal("unfreeze repaired missing state")
		}
	}
	frozen.MaxRuleVersion = p.version + 1
	if _, e := tr.Unfreeze(frozen, sig, p, testNow); e == nil {
		t.Fatal("version rollback thaw")
	}
}

func TestPermitStopsNextStep(t *testing.T) {
	_, _, _, p := baseline(t, []Rule{testRule()})
	s := testState(p)
	permit := Permit{testDigest, s.Epoch, s.Generation, testNow, testNow.Add(5 * time.Second)}
	if permit.Check(s, testDigest, testNow) != nil {
		t.Fatal("fresh permit")
	}
	for _, edit := range []func(*SafetyState){func(s *SafetyState) { s.Frozen = true }, func(s *SafetyState) { s.LocalStop = true }, func(s *SafetyState) { s.Epoch++; s.MaxEpoch++ }, func(s *SafetyState) { s.Generation++ }, func(s *SafetyState) { s.Fresh = false }} {
		n := s
		edit(&n)
		if permit.Check(n, testDigest, testNow) == nil {
			t.Fatal("old permission survives revocation")
		}
	}
	if permit.Check(s, testDigest, testNow.Add(5*time.Second)) == nil {
		t.Fatal("permit expiry")
	}
	permit.ExpiresAt = testNow.Add(6 * time.Second)
	if permit.Check(s, testDigest, testNow) == nil {
		t.Fatal("long offline permit")
	}
}

func TestNonceRetentionAndReplay(t *testing.T) {
	c := Consumption{"nonce", "execution", testDigest, 1, "2026-10-26T11:00:00Z", "2026-10-26T11:01:00Z", true, true, false, true, true}
	if !c.CanCollect(testNow) {
		t.Fatal("settled archived tombstone not collectible")
	}
	for _, edit := range []func(*Consumption){func(c *Consumption) { c.Terminal = false }, func(c *Consumption) { c.RollbackComplete = false }, func(c *Consumption) { c.Unknown = true }, func(c *Consumption) { c.ResultArchived = false }, func(c *Consumption) { c.AuditArchived = false }, func(c *Consumption) { c.ApprovalExpiresAt = "2026-10-26T12:00:00Z" }, func(c *Consumption) { c.TicketsExpireAt = "2026-10-26T12:01:00Z" }} {
		n := c
		edit(&n)
		if n.CanCollect(testNow) {
			t.Fatal("premature tombstone collection")
		}
	}
	if start, e := BindNonce(nil, c); e != nil || !start {
		t.Fatal("initial binding", e)
	}
	if start, e := BindNonce([]Consumption{c}, c); e != nil || start {
		t.Fatal("replay must be lookup-only", e)
	}
	n := c
	n.PlanHash = Digest([]byte("changed"))
	if _, e := BindNonce([]Consumption{c}, n); e == nil {
		t.Fatal("nonce rebound")
	}
	n = c
	n.Nonce = "new"
	if _, e := BindNonce([]Consumption{c}, n); e == nil {
		t.Fatal("execution rebound")
	}
	// More than the UI's closed-record cap cannot evict authorization state.
	records := []Consumption{c}
	for i := 0; i < 501; i++ {
		n := c
		n.ExecutionID = string(rune(i + 1000))
		n.Nonce = n.ExecutionID
		records = append(records, n)
	}
	if start, e := BindNonce(records, c); e != nil || start {
		t.Fatal("closed record cap allowed replay")
	}
}

func TestCanonicalHPlanAndAllBindings(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	p := testPlan()
	raw := canonical(p)
	c := planContext(p)
	b, hash, e := CanonicalPlan(raw)
	if e != nil || !bytes.Equal(b, raw) {
		t.Fatal(e)
	}
	sig := k.Sign(t, []byte(hash), NamespaceApproval)
	if _, e := tr.VerifyApproval(raw, sig, c, testNow); e != nil {
		t.Fatal(e)
	}
	var formatted bytes.Buffer
	_ = json.Indent(&formatted, raw, "", "  ")
	_, sameHash, e := CanonicalPlan(formatted.Bytes())
	if e != nil || sameHash != hash {
		t.Fatal("canonical hash differs")
	}
	if _, e := tr.VerifyApproval(formatted.Bytes(), sig, c, testNow); e == nil {
		t.Fatal("noncanonical transport accepted")
	}
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	// Alter every top-level signed field; even if fresh observations match the
	// modified plan, the old signature cannot authorize it.
	for field, value := range obj {
		t.Run(field, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			switch v := value.(type) {
			case string:
				m[field] = v + "x"
			case float64:
				m[field] = v + 1
			case []any:
				m[field] = append(v, "extra")
			case map[string]any:
				v["extra"] = "changed"
				m[field] = v
			case bool:
				m[field] = !v
			}
			changed, _ := json.Marshal(m)
			if _, e := tr.VerifyApproval(changed, sig, c, testNow); e == nil {
				t.Fatal("changed field accepted")
			}
		})
	}
	for _, edit := range []func(*PlanContext){func(c *PlanContext) { c.BeforeDigest = Digest([]byte("drift")) }, func(c *PlanContext) { c.Target.Generation++ }, func(c *PlanContext) { c.Target.HostKey = "changed" }, func(c *PlanContext) { c.ExecutorID = "other" }, func(c *PlanContext) { c.State.Frozen = true }, func(c *PlanContext) { c.Locks = []string{"host:host-a"} }, func(c *PlanContext) { c.FencingGeneration++ }, func(c *PlanContext) { c.PreinstalledActions = []string{} }, func(c *PlanContext) { c.Consumptions = []Consumption{{Nonce: p.Nonce}} }} {
		ctx := c
		edit(&ctx)
		if _, e := tr.VerifyApproval(raw, sig, ctx, testNow); e == nil {
			t.Fatal("stale observation accepted")
		}
	}
	if _, e := tr.VerifyApproval(raw, "approved", c, testNow); e == nil {
		t.Fatal("admin flag accepted")
	}
	if _, e := tr.VerifyApproval(raw, sig, c, testNow.Add(2*time.Hour)); e == nil {
		t.Fatal("expired H after tombstone collection accepted")
	}
	bad := bytes.Replace(raw, []byte(`"action":"host_service_restart"`), []byte(`"action":"host_service_restart","action":"host_service_state"`), 1)
	if _, _, e := CanonicalPlan(bad); e == nil {
		t.Fatal("duplicate JSON key")
	}
}

func TestSignatureCoversEveryCanonicalPlanField(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	original := testPlan()
	raw := canonical(original)
	sig := k.Sign(t, []byte(Digest(raw)), NamespaceApproval)
	for index := 0; index < reflect.TypeOf(original).NumField(); index++ {
		var p Plan
		if e := Decode(raw, &p); e != nil {
			t.Fatal(e)
		}
		v := reflect.ValueOf(&p).Elem().Field(index)
		switch v.Kind() {
		case reflect.String:
			v.SetString(v.String() + "changed")
		case reflect.Int:
			v.SetInt(v.Int() + 1)
		case reflect.Uint64:
			v.SetUint(v.Uint() + 1)
		case reflect.Map:
			v.SetMapIndex(reflect.ValueOf("changed"), reflect.ValueOf("value"))
		case reflect.Struct:
			v.Field(0).SetString(v.Field(0).String() + "changed")
		case reflect.Slice:
			if v.Type().Elem().Kind() == reflect.String {
				v.Set(reflect.Append(v, reflect.ValueOf("changed")))
			} else {
				v.Index(0).Field(0).SetString("changed")
			}
		default:
			t.Fatal("unhandled signed field")
		}
		changed := canonical(p)
		if bytes.Equal(raw, changed) {
			t.Fatal("ineffective mutation")
		}
		if e := tr.verify(Signed{Text: Digest(changed), Signature: sig}, NamespaceApproval); e == nil {
			t.Fatal("signature failed to bind", reflect.TypeOf(p).Field(index).Name)
		}
	}
}

func TestSignedHUnknownParameterStillRejected(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	p := testPlan()
	p.Params["command"] = "unexpected"
	raw := canonical(p)
	sig := k.Sign(t, []byte(Digest(raw)), NamespaceApproval)
	if _, err := tr.VerifyApproval(raw, sig, planContext(p), testNow); err == nil {
		t.Fatal("signed unknown parameter accepted")
	}
}

func TestD10FullContractMatrix(t *testing.T) {
	var vectors []struct {
		Carrier   string `json:"carrier"`
		Action    string `json:"action"`
		Tier      string `json:"tier"`
		Operation string `json:"operation"`
	}
	raw, e := os.ReadFile("testdata/d10.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = Decode(raw, &vectors); e != nil {
		t.Fatal(e)
	}
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	for _, site := range []string{"platform", "executor", "helper"} {
		for _, v := range vectors {
			for _, alias := range []string{"daemon", "bifrost-engine", "bifrost-engine.service", "trading_engine"} {
				t.Run(site+"/"+v.Carrier+"/"+alias, func(t *testing.T) {
					effects := []Effect{{alias, v.Operation, testDigest}}
					if err := catalog().Boundary(v.Action, v.Tier, effects); err == nil || !strings.Contains(err.Error(), "D10") {
						t.Fatalf("D10 not enforced: %v", err)
					}
					// A valid Owner signature over forbidden forward or rollback effects
					// cannot override the independent H boundary.
					p := testPlan()
					p.Action = "host_access_change"
					p.Params = map[string]string{"account": "test-account", "plan": "test-plan"}
					if v.Carrier == "rollback-old-bundle" {
						p.Rollback.Effects = effects
					} else {
						p.Effects = effects
					}
					ctx := planContext(p)
					raw := canonical(p)
					sig := k.Sign(t, []byte(Digest(raw)), NamespaceApproval)
					if _, err := tr.VerifyApproval(raw, sig, ctx, testNow); err == nil || !strings.Contains(err.Error(), "D10") {
						t.Fatalf("H signature bypass: %v", err)
					}
				})
			}
		}
	}
	for _, action := range []string{"host_status", "host_logs"} {
		if e := catalog().Boundary(action, "R", []Effect{{"daemon", "read", testDigest}}); e != nil {
			t.Fatal("registered read positive", e)
		}
	}
	if e := catalog().Boundary("host_monitoring_install", "M", []Effect{{"daemon", "observe", testDigest}}); e != nil {
		t.Fatal("pure observation positive", e)
	}
	if e := catalog().Boundary("host_monitoring_install", "M", []Effect{{"unknown", "observe", testDigest}}); e == nil {
		t.Fatal("unproven bundle")
	}
}

func TestNeverMCanonicalAliasesAndSelfMaintenance(t *testing.T) {
	for _, name := range neverM {
		for _, alias := range []string{name, name + ".service", name + "@instance.service"} {
			if !NeverM(alias) {
				t.Fatal(alias)
			}
			for _, action := range []string{"host_service_restart", "host_service_state", "host_monitoring_install", "host_agentd_update"} {
				if e := catalog().Boundary(action, "M", []Effect{{alias, "restart", testDigest}}); e == nil {
					t.Fatal("never-M indirect", alias)
				}
			}
		}
	}
	if e := catalog().Boundary("host_service_restart", "M", []Effect{{"org.example.sshd", "restart", testDigest}}); e == nil {
		t.Fatal("launchd alias")
	}
	if e := catalog().Boundary("host_service_restart", "H", []Effect{{"sshd", "restart", testDigest}}); e != nil {
		t.Fatal("H maintenance positive", e)
	}
	if e := catalog().Boundary("host_access_change", "H", []Effect{{"com.example.host-exec", "access", testDigest}}); e == nil {
		t.Fatal("self-maintenance H")
	}
	if e := catalog().Boundary("host_new_node_join", "M", []Effect{{"k3s-agent", "join_first_start", testDigest}}); e != nil {
		t.Fatal("first worker join contract", e)
	}
	if e := catalog().Boundary("host_new_node_join", "M", []Effect{{"k3s-agent", "restart", testDigest}}); e == nil {
		t.Fatal("join repaired existing service")
	}
}
