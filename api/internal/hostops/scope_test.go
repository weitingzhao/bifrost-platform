package hostops

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func scopeObject(id, kind string) Object {
	return Object{ID: id, Host: "host-a", Kind: kind, Canonical: id, Aliases: []string{"alias." + id}, ConfigDigest: Digest([]byte("config:" + id)), Dependencies: []string{}, Actions: []string{"host_status"}, ActionScopes: []ActionScope{}, Files: []string{}, Ports: []int{}, Methods: []string{}, Repositories: map[string]string{}}
}
func observed(e ScopedEffect) Effect {
	params := map[string]string{}
	for k, v := range e.Parameters {
		params[k] = v
	}
	return Effect{Identity: e.ObjectID, Operation: e.Operation, SourceDigest: e.SourceDigest, Parameters: params, Host: "host-a"}
}
func cloneAdmission(a Admission) Admission {
	// JSON clone the mutable observations; Signed.Text and the policy are immutable.
	a.ForwardEffects = append([]Effect{}, a.ForwardEffects...)
	a.RollbackEffects = append([]Effect{}, a.RollbackEffects...)
	for _, effects := range [][]Effect{a.ForwardEffects, a.RollbackEffects} {
		for n := range effects {
			p := map[string]string{}
			for k, v := range effects[n].Parameters {
				p[k] = v
			}
			effects[n].Parameters = p
		}
	}
	return a
}

// Each fixture has a freshly signed inventory, bundle and M baseline. Malicious
// observations are introduced only AFTER this positive fixture is admitted.
func typedAdmission(t *testing.T, action string) (Trust, Admission) {
	t.Helper()
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	bundle, files := bundleFixture()
	bundle.Actions = []string{action}
	manifest := signed(t, k, bundle, NamespaceBundle)
	md := Digest([]byte(manifest.Text))
	inv := inventory()
	inv.Objects = append(inv.Objects, scopeObject("project-a", "project"), scopeObject("installation-a", "install"), scopeObject("dependency", "root"))
	inv.Objects[0].Dependencies = []string{"dependency"}
	inv.Objects[1].Dependencies = []string{"exporter", "dependency"}
	x := testRequest()
	x.Action = action
	primary := 0
	s := ActionScope{ID: "typed", Action: action, Params: map[string]ScopeParameter{}, Forward: []ScopedEffect{}, Rollback: []ScopedEffect{}}
	usesBundle := false
	switch action {
	case "host_service_restart":
		x.Params = map[string]string{"service": "exporter"}
		s.Params["service"] = objectScope("exporter")
		s.Forward = []ScopedEffect{scopedEffect("exporter", "restart", testDigest)}
		s.Rollback = []ScopedEffect{scopedEffect("exporter", "state", testDigest)}
		s.Rollback[0].Parameters["state"] = "stop"
	case "host_service_state":
		x.Params = map[string]string{"service": "exporter", "state": "start"}
		s.Params["service"] = objectScope("exporter")
		s.Params["state"] = enumScope("start")
		s.Forward = []ScopedEffect{scopedEffect("exporter", "state", testDigest)}
		s.Forward[0].Parameters["state"] = "start"
		s.Rollback = []ScopedEffect{scopedEffect("exporter", "state", testDigest)}
		s.Rollback[0].Parameters["state"] = "stop"
	case "host_container_restart":
		x.Params = map[string]string{"service": "exporter", "project": "project-a"}
		s.Params["service"] = objectScope("exporter")
		s.Params["project"] = objectScope("project-a")
		s.Forward = []ScopedEffect{scopedEffect("exporter", "restart", testDigest)}
		s.Forward[0].Parameters["project"] = "project-a"
		s.Rollback = []ScopedEffect{scopedEffect("exporter", "restart", testDigest)}
		s.Rollback[0].Parameters["project"] = "project-a"
	case "host_compose_reconcile":
		primary = 1
		usesBundle = true
		x.Params = map[string]string{"project": "project-a", "bundle": md}
		s.Params["project"] = objectScope("project-a")
		s.Params["bundle"] = enumScope(md)
		s.Forward = []ScopedEffect{scopedEffect("project-a", "recreate", bundle.Entries[0].SHA256), scopedEffect("exporter", "restart", testDigest)}
		s.Rollback = []ScopedEffect{scopedEffect("project-a", "recreate", inv.Objects[1].ConfigDigest), scopedEffect("exporter", "state", testDigest)}
		s.Rollback[1].Parameters["state"] = "stop"
		for _, es := range [][]ScopedEffect{s.Forward, s.Rollback} {
			for n := range es {
				es[n].Parameters["project"] = "project-a"
			}
		}
	case "host_monitoring_install", "host_agentd_update":
		usesBundle = true
		x.Params = map[string]string{"bundle": md}
		s.Params["bundle"] = enumScope(md)
		if action == "host_agentd_update" {
			x.Params["environment"] = "test"
			s.Params["environment"] = enumScope("test")
		}
		s.Forward = []ScopedEffect{scopedEffect("exporter", "install", bundle.Entries[0].SHA256)}
		s.Rollback = []ScopedEffect{scopedEffect("exporter", "restore", testDigest)}
	case "host_monitoring_remove":
		primary = 2
		x.Params = map[string]string{"install_id": "installation-a"}
		s.Params["install_id"] = objectScope("installation-a")
		s.Forward = []ScopedEffect{scopedEffect("installation-a", "remove", inv.Objects[2].ConfigDigest)}
		s.Rollback = []ScopedEffect{scopedEffect("installation-a", "restore", inv.Objects[2].ConfigDigest)}
	default:
		t.Fatal("unsupported test action", action)
	}
	inv.Objects[primary].Actions = []string{action}
	inv.Objects[primary].ActionScopes = []ActionScope{s}
	invSigned := signed(t, k, inv, NamespaceInventory)
	r := testRule()
	r.InventoryDigest = Digest([]byte(invSigned.Text))
	c := &r.Clauses[0]
	c.Action = action
	c.Params = map[string][]string{}
	for name, v := range x.Params {
		c.Params[name] = []string{v}
		if name == "service" {
			c.Params[name] = append(c.Params[name], "com.example.exporter")
		}
		if name == "project" {
			c.Params[name] = append(c.Params[name], "alias.project-a")
		}
	}
	bundles := []ReceivedBundle{}
	if usesBundle {
		x.BundleDigests = []string{md}
		c.BundleDigests = []string{md}
		bundles = []ReceivedBundle{{manifest, ArtifactMark{1, md}, files}}
	}
	set := PolicySet{signed(t, k, PolicyBundle{metadata("test-policy"), "", map[string]string{}, []Rule{r}}, NamespacePolicy), []Tightening{}}
	p, err := tr.AdmitBaseline(set.Baseline, nil, "test", testNow)
	if err != nil {
		t.Fatal(err)
	}
	cat := catalog()
	for _, o := range inv.Objects {
		cat.Aliases[o.Canonical] = o.Canonical
		for _, alias := range o.Aliases {
			cat.Aliases[alias] = o.Canonical
		}
	}
	a := Admission{RequestJSON: canonical(x), AuthenticatedExecutor: x.ExecutorID, Environment: x.Environment, PreinstalledActions: []string{action}, ObjectID: inv.Objects[primary].ID, Inventory: invSigned, InventoryMark: ArtifactMark{1, r.InventoryDigest}, PolicySet: set, State: testState(p), Facts: testFacts(), Catalog: cat, Bundles: bundles}
	for _, e := range s.Forward {
		a.ForwardEffects = append(a.ForwardEffects, observed(e))
	}
	for _, e := range s.Rollback {
		a.RollbackEffects = append(a.RollbackEffects, observed(e))
	}
	if d := tr.EvaluateM(a, testNow); !d.Allowed {
		t.Fatal("positive fixture", action, d)
	}
	return tr, a
}

func TestTypedScopeFirstSevenActions(t *testing.T) {
	for _, action := range firstMaintenanceActions {
		t.Run(action, func(t *testing.T) {
			tr, a := typedAdmission(t, action)
			for _, name := range []string{"wrong-operation", "cross-object", "cross-host", "extra-effect", "missing-effect", "unknown-parameters", "unknown-source", "rollback-operation", "rollback-host", "rollback-parameters"} {
				t.Run(name, func(t *testing.T) {
					b := cloneAdmission(a)
					switch name {
					case "wrong-operation":
						b.ForwardEffects[0].Operation = "access"
					case "rollback-operation":
						b.RollbackEffects[0].Operation = "access"
					case "rollback-host":
						b.RollbackEffects[0].Host = "host-b"
					case "rollback-parameters":
						b.RollbackEffects[0].Parameters["command"] = "unrecognized"
					case "cross-object":
						b.ForwardEffects[0].Identity = "dependency"
					case "cross-host":
						b.ForwardEffects[0].Host = "host-b"
					case "extra-effect":
						b.ForwardEffects = append(b.ForwardEffects, b.ForwardEffects[0])
					case "missing-effect":
						b.ForwardEffects = b.ForwardEffects[1:]
					case "unknown-parameters":
						b.ForwardEffects[0].Parameters["command"] = "unrecognized"
					case "unknown-source":
						b.ForwardEffects[0].SourceDigest = Digest([]byte("unverified"))
					}
					if d := tr.EvaluateM(b, testNow); d.Allowed {
						t.Fatal("unsafe scope accepted", d)
					}
				})
			}
		})
	}
}

func TestTypedScopeAliasesAndMultiObjectClosure(t *testing.T) {
	for _, action := range []string{"host_service_restart", "host_container_restart", "host_compose_reconcile"} {
		t.Run(action, func(t *testing.T) {
			tr, a := typedAdmission(t, action)
			var x Request
			if err := Decode(a.RequestJSON, &x); err != nil {
				t.Fatal(err)
			}
			if _, ok := x.Params["service"]; ok {
				x.Params["service"] = "com.example.exporter"
			}
			if _, ok := x.Params["project"]; ok {
				x.Params["project"] = "alias.project-a"
			}
			a.RequestJSON = canonical(x)
			for _, es := range [][]Effect{a.ForwardEffects, a.RollbackEffects} {
				for n := range es {
					if es[n].Identity == "exporter" {
						es[n].Identity = "com.example.exporter"
					} else if es[n].Identity == "project-a" {
						es[n].Identity = "alias.project-a"
					}
				}
			}
			// Effect sets are order independent, but complete and phase-specific.
			if len(a.ForwardEffects) > 1 {
				a.ForwardEffects[0], a.ForwardEffects[1] = a.ForwardEffects[1], a.ForwardEffects[0]
			}
			if d := tr.EvaluateM(a, testNow); !d.Allowed {
				t.Fatal("registered aliases/legal closure", d)
			}
		})
	}
}

func TestTypedScopeDependenciesAndPhaseBindings(t *testing.T) {
	tr, a := typedAdmission(t, "host_compose_reconcile")
	for _, name := range []string{"forward-dependency", "rollback-dependency", "extra-dependency", "phase-swap", "wrong-project", "missing-project", "request-project", "unsigned-alias", "catalog-disagrees"} {
		t.Run(name, func(t *testing.T) {
			b := cloneAdmission(a)
			switch name {
			case "forward-dependency":
				b.ForwardEffects[1].Identity = "dependency"
			case "rollback-dependency":
				b.RollbackEffects[1].Identity = "dependency"
			case "extra-dependency":
				b.ForwardEffects = append(b.ForwardEffects, Effect{Identity: "dependency", Operation: "restart", SourceDigest: testDigest, Host: "host-a"})
			case "phase-swap":
				b.ForwardEffects, b.RollbackEffects = b.RollbackEffects, b.ForwardEffects
			case "wrong-project":
				b.ForwardEffects[1].Parameters["project"] = "other-project"
			case "missing-project":
				delete(b.ForwardEffects[1].Parameters, "project")
			case "request-project":
				var x Request
				_ = Decode(b.RequestJSON, &x)
				x.Params["project"] = "other-project"
				b.RequestJSON = canonical(x)
			case "unsigned-alias":
				b.ForwardEffects[1].Identity = "exporter.service"
			case "catalog-disagrees":
				b.Catalog = catalog()
				b.Catalog.Aliases["project-a"] = "project-a"
				b.Catalog.Aliases["exporter"] = "other"
			}
			if d := tr.EvaluateM(b, testNow); d.Allowed {
				t.Fatal("scope bypass", d)
			}
		})
	}
	tr, a = typedAdmission(t, "host_service_state")
	for _, value := range []string{"stop", "disable", "", "restart"} {
		b := cloneAdmission(a)
		b.ForwardEffects[0].Parameters["state"] = value
		if d := tr.EvaluateM(b, testNow); d.Allowed {
			t.Fatal("state ambiguity", value)
		}
	}
	b := cloneAdmission(a)
	b.ForwardEffects, b.RollbackEffects = b.RollbackEffects, b.ForwardEffects
	if d := tr.EvaluateM(b, testNow); d.Allowed {
		t.Fatal("forward/rollback state swapped")
	}
}

func TestTypedScopeAllReadActions(t *testing.T) {
	for _, action := range readActions {
		t.Run(action, func(t *testing.T) {
			k := rptest.NewKey(t)
			tr := Trust{k.Fingerprint}
			inv := inventory()
			o := &inv.Objects[0]
			x := testRequest()
			x.Action = action
			s := ActionScope{ID: "read", Action: action, Params: map[string]ScopeParameter{}, Forward: []ScopedEffect{scopedEffect("exporter", "read", testDigest)}, Rollback: []ScopedEffect{}}
			switch action {
			case "host_status":
				x.Params = map[string]string{"check": "service"}
				s.Params["check"] = enumScope("service")
			case "host_logs":
				s = fixtureScopes()[2]
				x.Params = map[string]string{"service": "com.example.exporter", "since": "2026-10-26T11:00:00Z", "lines": "200"}
			case "host_storage_status":
				o.Kind = "root"
				x.Params = map[string]string{"root_id": "exporter"}
				s.Params["root_id"] = objectScope("exporter")
			case "host_network_probe":
				o.Kind = "endpoint"
				x.Params = map[string]string{"endpoint_id": "exporter", "kind": "tcp"}
				s.Params["endpoint_id"] = objectScope("exporter")
				s.Params["kind"] = enumScope("tcp")
			case "host_integrity_check":
				x.Params = map[string]string{"profile": "integrity-basic"}
				s.Params["profile"] = enumScope("integrity-basic")
				s.Forward[0].Parameters["profile"] = "integrity-basic"
			case "host_inventory_plan":
				x.Params = map[string]string{"operation": "installations"}
				s.Params["operation"] = enumScope("installations")
			case "platform_state_check":
				x.Params = map[string]string{"scope": "test", "check": "window"}
				s.Params["scope"] = enumScope("test")
				s.Params["check"] = enumScope("window")
			}
			o.Actions = []string{action}
			o.ActionScopes = []ActionScope{s}
			signedInv := signed(t, k, inv, NamespaceInventory)
			mark := ArtifactMark{1, Digest([]byte(signedInv.Text))}
			effects := []Effect{observed(s.Forward[0])}
			check := func(x Request, es []Effect) error {
				return tr.ValidateReadScope(signedInv, mark, x, "exporter", catalog(), es, testNow)
			}
			if err := check(x, effects); err != nil {
				t.Fatal("read positive", err)
			}
			for _, name := range []string{"cross-object", "cross-host", "wrong-operation", "unknown-source", "extra-effect", "empty-effects", "parameter-substitution"} {
				t.Run(name, func(t *testing.T) {
					es := []Effect{observed(s.Forward[0])}
					req := x
					req.Params = map[string]string{}
					for k, v := range x.Params {
						req.Params[k] = v
					}
					switch name {
					case "cross-object":
						es[0].Identity = "sshd"
					case "cross-host":
						es[0].Host = "host-b"
					case "wrong-operation":
						es[0].Operation = "restart"
					case "unknown-source":
						es[0].SourceDigest = Digest([]byte("other"))
					case "extra-effect":
						es = append(es, es[0])
					case "empty-effects":
						es = nil
					case "parameter-substitution":
						for k := range req.Params {
							req.Params[k] = "unregistered"
							break
						}
					}
					if err := check(req, es); err == nil {
						t.Fatal("read scope accepted substitution")
					}
				})
			}
			if action == "host_integrity_check" {
				effects[0].Parameters["profile"] = "other-profile"
				if check(x, effects) == nil {
					t.Fatal("actual profile substitution")
				}
			}
		})
	}
}

func TestTypedScopeMalformedSignedContracts(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	for _, name := range []string{"unknown-target", "wrong-operation", "unknown-constraint", "unknown-action", "cross-host-target", "request-target", "duplicate-effect", "missing-rollback", "unbound-profile", "future-plan-template"} {
		t.Run(name, func(t *testing.T) {
			inv := inventory()
			s := &inv.Objects[0].ActionScopes[0]
			switch name {
			case "unknown-target":
				s.Forward[0].ObjectID = "missing"
			case "wrong-operation":
				s.Forward[0].Operation = "access"
			case "unknown-constraint":
				s.Params["service"] = ScopeParameter{Kind: "wildcard", Values: []string{"*"}}
			case "unknown-action":
				s.Action = "unimplemented"
			case "cross-host-target":
				o := scopeObject("other", "service")
				o.Host = "host-b"
				inv.Hosts = append(inv.Hosts, inv.Hosts[0])
				inv.Hosts[1].ID = "host-b"
				inv.Objects = append(inv.Objects, o)
				s.Forward[0].ObjectID = "other"
			case "request-target":
				o := scopeObject("other", "service")
				inv.Objects = append(inv.Objects, o)
				s.Params["service"] = objectScope("other")
			case "duplicate-effect":
				s.Forward = append(s.Forward, s.Forward[0])
			case "missing-rollback":
				s.Rollback = []ScopedEffect{}
			case "unbound-profile":
				s.Action = "host_integrity_check"
				inv.Objects[0].Actions = append(inv.Objects[0].Actions, s.Action)
				s.Params = map[string]ScopeParameter{"profile": enumScope("p")}
				s.Forward[0].Operation = "read"
				s.Rollback = []ScopedEffect{}
			case "future-plan-template":
				s.Action = "host_temp_cleanup"
				inv.Objects[0].Actions = append(inv.Objects[0].Actions, s.Action)
				s.Params = map[string]ScopeParameter{"plan": enumScope("cleanup-a")}
			}
			if _, err := tr.Inventory(signed(t, k, inv, NamespaceInventory)); err == nil {
				t.Fatal("invalid signed contract accepted")
			}
		})
	}
	// An action name/capability with no contract remains unsupported.
	tr, a := admission(t)
	var inv Inventory
	_ = Decode([]byte(a.Inventory.Text), &inv)
	inv.Objects[0].ActionScopes = []ActionScope{}
	// Re-sign with a test key and pin the new inventory in a matching policy.
	invSigned := signed(t, k, inv, NamespaceInventory)
	r := testRule()
	r.InventoryDigest = Digest([]byte(invSigned.Text))
	set := PolicySet{signed(t, k, PolicyBundle{metadata("p"), "", map[string]string{}, []Rule{r}}, NamespacePolicy), []Tightening{}}
	tr = Trust{k.Fingerprint}
	p, err := tr.AdmitBaseline(set.Baseline, nil, "test", testNow)
	if err != nil {
		t.Fatal(err)
	}
	a.Inventory = invSigned
	a.InventoryMark = ArtifactMark{1, r.InventoryDigest}
	a.PolicySet = set
	a.State = testState(p)
	if d := tr.EvaluateM(a, testNow); d.Allowed || !strings.Contains(d.Reason, "unsupported") {
		t.Fatal("missing scope fallback", d)
	}
}

func TestTypedScopeHPlanSelectorBinding(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	p := testPlan()
	p.Action = "host_node_power"
	p.Params = map[string]string{"operation": "power-off", "plan": "power-plan-a"}
	p.Effects[0].Parameters = map[string]string{"plan": "power-plan-a"}
	p.Rollback.Params = map[string]string{"plan": "restore-plan-a"}
	p.Rollback.Effects[0].Parameters = map[string]string{"plan": "restore-plan-a"}
	check := func(p Plan) error {
		c := planContext(p)
		c.PreinstalledActions = []string{p.Action}
		raw := canonical(p)
		signature := k.Sign(t, []byte(Digest(raw)), NamespaceApproval)
		_, err := tr.VerifyApproval(raw, signature, c, testNow)
		return err
	}
	if err := check(p); err != nil {
		t.Fatal("signed H plan context positive", err)
	}
	for _, name := range []string{"forward", "rollback", "phase-swap", "host"} {
		t.Run(name, func(t *testing.T) {
			var q Plan
			if err := Decode(canonical(p), &q); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "forward":
				q.Effects[0].Parameters["plan"] = "other-plan"
			case "rollback":
				delete(q.Rollback.Effects[0].Parameters, "plan")
			case "phase-swap":
				q.Effects, q.Rollback.Effects = q.Rollback.Effects, q.Effects
			case "host":
				q.Effects[0].Host = "host-b"
			}
			// Even a valid test signature cannot accept contradictory phase selectors.
			if check(q) == nil {
				t.Fatal("H selector mismatch accepted")
			}
		})
	}
}

func TestTypedScopeAmbiguousVariantsAndSignature(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	inv := inventory()
	duplicate := inv.Objects[0].ActionScopes[1]
	duplicate.ID = "second-status"
	inv.Objects[0].ActionScopes = append(inv.Objects[0].ActionScopes, duplicate)
	s := signed(t, k, inv, NamespaceInventory)
	mark := ArtifactMark{1, Digest([]byte(s.Text))}
	x := testRequest()
	x.Action = "host_status"
	x.Params = map[string]string{"check": "service"}
	effects := []Effect{observed(duplicate.Forward[0])}
	if err := tr.ValidateReadScope(s, mark, x, "exporter", catalog(), effects, testNow); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatal("ambiguous variant", err)
	}
	s = signed(t, k, inventory(), NamespaceInventory)
	s.Text = strings.Replace(s.Text, `"operation":"restart"`, `"operation":"access"`, 1)
	mark.Digest = Digest([]byte(s.Text))
	if _, err := tr.Inventory(s); err == nil {
		t.Fatal("scope change without new signature")
	}
}

func TestTypedScopeSignedSourceStillNeedsVerifiedBytes(t *testing.T) {
	_, a := admission(t)
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	inv := inventory()
	unverified := Digest([]byte("not a handler, registered config or received bundle"))
	inv.Objects[0].ActionScopes[0].Forward[0].SourceDigest = unverified
	invSigned := signed(t, k, inv, NamespaceInventory)
	r := testRule()
	r.InventoryDigest = Digest([]byte(invSigned.Text))
	set := PolicySet{signed(t, k, PolicyBundle{metadata("p"), "", map[string]string{}, []Rule{r}}, NamespacePolicy), []Tightening{}}
	p, err := tr.AdmitBaseline(set.Baseline, nil, "test", testNow)
	if err != nil {
		t.Fatal(err)
	}
	a.Inventory = invSigned
	a.InventoryMark = ArtifactMark{1, r.InventoryDigest}
	a.PolicySet = set
	a.State = testState(p)
	a.ForwardEffects[0].SourceDigest = unverified
	if d := tr.EvaluateM(a, testNow); d.Allowed || !strings.Contains(d.Reason, "verified bytes") {
		t.Fatal("signed scope replaced byte proof", d)
	}
}

func TestTypedScopeRejectsLegacyInventory(t *testing.T) {
	k := rptest.NewKey(t)
	tr := Trust{k.Fingerprint}
	inv := inventory()
	raw := strings.Replace(string(canonical(inv)), `"action_scopes":`, `"maintenance_scope":`, 1)
	s := Signed{Text: raw, Signature: k.Sign(t, []byte(raw), NamespaceInventory)}
	if _, err := tr.Inventory(s); err == nil {
		t.Fatal("legacy field granted implicit scope")
	}
}
