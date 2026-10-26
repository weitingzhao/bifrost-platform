package hostops

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func TestClosedDocument(t *testing.T) {
	raw := canonical(testRule())
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return append(b, []byte("\n---\n{}")...) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"Schema_Version":1`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"schema_version":1`), []byte(`"schema_version":"1"`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"max_per_day":4`), []byte(`"max_per_day":null`), 1)
		},
		func(b []byte) []byte { return bytes.Replace(b, []byte(`"max_per_day":4,`), nil, 1) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"max_per_day":4`), []byte(`"max_per_day":4.0`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"disabled":false`), []byte(`"disabled":"false"`), 1)
		},
		func(b []byte) []byte { return bytes.Replace(b, []byte(`"hosts":[`), []byte(`"hosts": &scope [`), 1) },
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"rule_version":1`), []byte(`"rule_version": !!int 1`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"rule_id":"test-rule"`), []byte(`"rule_id":*missing`), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"rule_id":"test-rule"`), []byte(`"<<":{},"rule_id":"test-rule"`), 1)
		},
	} {
		var r Rule
		if err := Decode(mutate(raw), &r); err == nil {
			t.Fatalf("invalid document accepted: %s", mutate(raw))
		}
	}
	var r Rule
	if err := Decode(raw, &r); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"2026-10-26T00:00:00", "2027-10-25T00:00:00Z"} {
		r = testRule()
		r.ExpiresAt = bad
		if r.validate() == nil {
			t.Fatal("invalid time accepted")
		}
	}
}

func TestSignatureNamespacesAndRawBytes(t *testing.T) {
	k := rptest.NewKey(t)
	other := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	s := signed(t, k, inventory(), NamespaceInventory)
	if _, e := tr.Inventory(s); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []Signed{
		{Text: s.Text + "\n", Signature: s.Signature},
		{Text: s.Text, Signature: k.Sign(t, []byte(s.Text), NamespacePolicy)},
		{Text: s.Text, Signature: other.Sign(t, []byte(s.Text), NamespaceInventory)},
		{Text: s.Text, Signature: s.Signature + "garbage"},
		{Text: s.Text, Signature: s.Signature + s.Signature},
	} {
		if _, e := tr.Inventory(bad); e == nil {
			t.Fatal("untrusted/cross-domain/tampered signature accepted")
		}
	}
	if _, e := (Trust{}).Inventory(s); e == nil {
		t.Fatal("empty trust anchor accepted")
	}
	for _, ns := range []string{NamespacePolicy, NamespaceInventory, NamespaceBundle, NamespaceApproval, NamespaceUnfreeze, NamespaceCircuitRelease} {
		s := signed(t, k, inventory(), ns)
		for _, otherNS := range []string{NamespacePolicy, NamespaceInventory, NamespaceBundle, NamespaceApproval, NamespaceUnfreeze, NamespaceCircuitRelease} {
			if err := tr.verify(s, otherNS); (err == nil) != (ns == otherNS) {
				t.Fatal("namespace isolation")
			}
		}
	}
}

func TestArtifactVersionAndExpiry(t *testing.T) {
	m := metadata("asset")
	raw := []byte("signed bytes")
	mark, e := CheckArtifact(m, raw, ArtifactMark{}, "test", testNow)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := CheckArtifact(m, raw, mark, "test", testNow); e != nil {
		t.Fatal("idempotent reload", e)
	}
	if _, e := CheckArtifact(m, []byte("changed"), mark, "test", testNow); e == nil {
		t.Fatal("same version changed bytes")
	}
	m.Version = 2
	newMark, e := CheckArtifact(m, raw, mark, "test", testNow)
	if e != nil {
		t.Fatal(e)
	}
	m.Version = 1
	if _, e := CheckArtifact(m, raw, newMark, "test", testNow); e == nil {
		t.Fatal("rollback")
	}
	m.Version = 3
	if _, e := CheckArtifact(m, raw, newMark, "prod", testNow); e == nil {
		t.Fatal("cross environment")
	}
	if _, e := CheckArtifact(m, raw, newMark, "test", testNow.Add(90*24*time.Hour)); e == nil {
		t.Fatal("expired")
	}
}

func bundleFixture() (Bundle, []ReceivedFile) {
	content := []byte("fixed test executable, never run")
	b := Bundle{metadata("bundle-test"), []string{"host_monitoring_install"}, []string{"host-a"}, []string{testDigest}, []BundleEntry{{"bin/exporter", uint64(len(content)), Digest(content), "root", 0755, "executable", ""}}}
	return b, []ReceivedFile{{"bin/exporter", content, "root", 0755, true, 1}}
}
func TestBundleSignedManifestAndExactBytes(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	b, files := bundleFixture()
	verified, e := tr.Bundle(signed(t, k, b, NamespaceBundle))
	if e != nil {
		t.Fatal(e)
	}
	check := func(f []ReceivedFile) error {
		return verified.VerifyFiles(f, "test", "host-a", "host_monitoring_install", testDigest, testNow)
	}
	if e := check(files); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*ReceivedFile){func(f *ReceivedFile) { f.Bytes = []byte("tampered") }, func(f *ReceivedFile) { f.Path = "../escape" }, func(f *ReceivedFile) { f.Regular = false }, func(f *ReceivedFile) { f.Links = 2 }, func(f *ReceivedFile) { f.Mode = 0777 }, func(f *ReceivedFile) { f.Owner = "agent" }} {
		f := append([]ReceivedFile{}, files...)
		edit(&f[0])
		if check(f) == nil {
			t.Fatal("unsafe received file accepted")
		}
	}
	if check(append(files, files[0])) == nil || check([]ReceivedFile{}) == nil {
		t.Fatal("wrong file set")
	}
	for _, p := range []string{"/absolute", "../escape", "a/../b", "a//b", ".", "a\\b", "a\x00b"} {
		bad := b
		bad.Entries = append([]BundleEntry{}, b.Entries...)
		bad.Entries[0].Path = p
		if _, e := tr.Bundle(signed(t, k, bad, NamespaceBundle)); e == nil {
			t.Fatal("unsafe signed path", p)
		}
	}
	b.Entries[0].Purpose = "image"
	b.Entries[0].OCIDigest = "image:latest"
	if _, e := tr.Bundle(signed(t, k, b, NamespaceBundle)); e == nil {
		t.Fatal("mutable image")
	}
	b.Entries[0].Purpose = "secret"
	if _, e := tr.Bundle(signed(t, k, b, NamespaceBundle)); e == nil {
		t.Fatal("secret purpose")
	}
}

func admission(t *testing.T) (Trust, Admission) {
	t.Helper()
	r := testRule()
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	inv := signed(t, k, inventory(), NamespaceInventory)
	r.InventoryDigest = Digest([]byte(inv.Text))
	b := PolicyBundle{metadata("test-policy"), "", map[string]string{}, []Rule{r}}
	set := PolicySet{signed(t, k, b, NamespacePolicy), []Tightening{}}
	p, e := tr.AdmitBaseline(set.Baseline, nil, "test", testNow)
	if e != nil {
		t.Fatal(e)
	}
	return tr, Admission{RequestJSON: canonical(testRequest()), AuthenticatedExecutor: "exec-a", Environment: "test", PreinstalledActions: []string{"host_service_restart"}, ObjectID: "exporter", Inventory: inv, InventoryMark: ArtifactMark{1, Digest([]byte(inv.Text))}, PolicySet: set, State: testState(p), Facts: testFacts(), Catalog: catalog(), ForwardEffects: []Effect{{"exporter", "restart", testDigest, nil, "host-a"}}, RollbackEffects: []Effect{{"exporter", "restart", testDigest, nil, "host-a"}}, Bundles: []ReceivedBundle{}}
}
func TestAdmissionReference(t *testing.T) {
	tr, a := admission(t)
	if d := tr.EvaluateM(a, testNow); !d.Allowed {
		t.Fatal(d)
	}
	for _, mutate := range []func(*Admission){
		func(a *Admission) { a.AuthenticatedExecutor = "exec-b" }, func(a *Admission) { a.Environment = "prod" }, func(a *Admission) { a.PreinstalledActions = nil }, func(a *Admission) { a.InventoryMark = ArtifactMark{} }, func(a *Admission) { a.PolicySet.Baseline.Text = "" }, func(a *Admission) { a.State.Frozen = true }, func(a *Admission) { a.RollbackEffects = nil }, func(a *Admission) {
			a.ForwardEffects = []Effect{{"daemon", "enable_execution", testDigest, nil, "host-a"}}
		}, func(a *Admission) { a.Facts.LockAvailable = false }, func(a *Admission) { a.ObjectID = "unknown" }, func(a *Admission) {
			a.ForwardEffects = []Effect{{"exporter", "restart", Digest([]byte("unverified")), nil, "host-a"}}
		},
	} {
		b := a
		mutate(&b)
		if d := tr.EvaluateM(b, testNow); d.Allowed {
			t.Fatal("bad admission accepted")
		}
	}
	x := testRequest()
	x.Action = "host_status"
	x.Params = map[string]string{"check": "service"}
	if err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"exporter", "read", testDigest, nil, "host-a"}}, testNow); err != nil {
		t.Fatal(err)
	}
	// Expired M baseline does not remove registered read diagnostics.
	a.PolicySet.Baseline.Text = "expired"
	if err := tr.ValidateReadScope(a.Inventory, a.InventoryMark, x, "exporter", catalog(), []Effect{{"exporter", "read", testDigest, nil, "host-a"}}, testNow); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionVerifiesBundleAndInventoryBindings(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	bundle, files := bundleFixture()
	manifest := signed(t, k, bundle, NamespaceBundle)
	md := Digest([]byte(manifest.Text))
	i := inventory()
	i.Objects[0].ActionScopes = append(i.Objects[0].ActionScopes, ActionScope{ID: "install", Action: "host_monitoring_install", Params: map[string]ScopeParameter{"bundle": enumScope(md)}, Forward: []ScopedEffect{scopedEffect("exporter", "install", bundle.Entries[0].SHA256)}, Rollback: []ScopedEffect{scopedEffect("exporter", "restart", testDigest)}})
	inv := signed(t, k, i, NamespaceInventory)
	r := testRule()
	r.InventoryDigest = Digest([]byte(inv.Text))
	r.Clauses[0].Action = "host_monitoring_install"
	r.Clauses[0].Params = map[string][]string{"bundle": {md}}
	r.Clauses[0].BundleDigests = []string{md}
	set := PolicySet{signed(t, k, PolicyBundle{metadata("test-policy"), "", map[string]string{}, []Rule{r}}, NamespacePolicy), []Tightening{}}
	p, e := tr.AdmitBaseline(set.Baseline, nil, "test", testNow)
	if e != nil {
		t.Fatal(e)
	}
	x := testRequest()
	x.Action = "host_monitoring_install"
	x.Params = map[string]string{"bundle": md}
	x.BundleDigests = []string{md}
	a := Admission{RequestJSON: canonical(x), AuthenticatedExecutor: x.ExecutorID, Environment: x.Environment, PreinstalledActions: []string{"host_monitoring_install"}, ObjectID: "exporter", Inventory: inv, InventoryMark: ArtifactMark{1, Digest([]byte(inv.Text))}, PolicySet: set, State: testState(p), Facts: testFacts(), Catalog: catalog(), ForwardEffects: []Effect{{"exporter", "install", bundle.Entries[0].SHA256, nil, "host-a"}}, RollbackEffects: []Effect{{"exporter", "restart", testDigest, nil, "host-a"}}, Bundles: []ReceivedBundle{{manifest, ArtifactMark{1, md}, files}}}
	if d := tr.EvaluateM(a, testNow); !d.Allowed {
		t.Fatal(d)
	}
	for _, mutate := range []func(*Admission){
		func(a *Admission) { a.Bundles = []ReceivedBundle{} },
		func(a *Admission) { a.Bundles = []ReceivedBundle{{manifest, ArtifactMark{}, files}} },
		func(a *Admission) {
			bad := append([]ReceivedFile{}, files...)
			bad[0].Bytes = []byte("bad")
			a.Bundles = []ReceivedBundle{{manifest, ArtifactMark{1, md}, bad}}
		},
		func(a *Admission) {
			s := manifest
			s.Signature = "invalid"
			a.Bundles = []ReceivedBundle{{s, ArtifactMark{1, md}, files}}
		},
		func(a *Admission) {
			i := inventory()
			i.Meta.Version++
			i.Objects[0].Actions = append(i.Objects[0].Actions, "host_toolchain_install")
			a.Inventory = signed(t, k, i, NamespaceInventory)
			a.InventoryMark = ArtifactMark{2, Digest([]byte(a.Inventory.Text))}
		},
	} {
		b := a
		mutate(&b)
		if d := tr.EvaluateM(b, testNow); d.Allowed {
			t.Fatal("bundle/inventory binding bypass")
		}
	}
}

func FuzzClosedRule(f *testing.F) {
	f.Add(string(canonical(testRule())))
	f.Add("{}")
	f.Add("x: &x [*x]")
	f.Fuzz(func(t *testing.T, s string) {
		var r Rule
		if Decode([]byte(s), &r) == nil {
			if _, err := json.Marshal(r); err != nil {
				t.Fatal(err)
			}
		}
	})
}
func TestInventoryAmbiguityAndUnknownStateful(t *testing.T) {
	i := inventory()
	i.Objects = append(i.Objects, i.Objects[0])
	i.Objects[1].ID = "other"
	if i.validate() == nil {
		t.Fatal("ambiguous alias accepted")
	}
	raw := canonical(inventory())
	raw = bytes.Replace(raw, []byte(`"stateful":false,`), nil, 1)
	var out Inventory
	if Decode(raw, &out) == nil {
		t.Fatal("unknown stateful accepted")
	}
	i = inventory()
	i.Objects[0].Canonical = "minio"
	if i.validate() == nil {
		t.Fatal("known stateful as false")
	}
	if strings.Contains(string(canonical(inventory())), "192.168.") {
		t.Fatal("fixture contains real host")
	}
}
