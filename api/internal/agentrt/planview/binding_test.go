package planview

import (
	"encoding/json"
	"strings"
	"testing"
)

func ownerBinding(d *Document) Binding {
	n := d.Items[0]
	a := n.Acceptance[0]
	return Binding{V: 1, Kind: BindingKind, ID: "signoff", Version: d.Version, Tenant: d.Tenant, Root: d.Root, Node: n.Node, Step: "real-step", Acceptance: a.ID, AcceptanceSHA256: AcceptanceDigest(a), Atom: "external_verified", Source: "owner_signoff", Evidence: []string{"sha256:" + strings.Repeat("a", 64)}}
}
func TestBindingIdentityAndClosedProtocol(t *testing.T) {
	d := document(t)
	b := ownerBinding(d)
	raw, _ := json.Marshal(b)
	d.Items[0].Bindings = []json.RawMessage{raw}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Binding){
		"tenant": func(b *Binding) { b.Tenant = "business" }, "root": func(b *Binding) { b.Root = "R2" }, "node": func(b *Binding) { b.Node = "4.2" }, "version": func(b *Binding) { b.Version++ }, "acceptance": func(b *Binding) { b.AcceptanceSHA256 = strings.Repeat("b", 64) }, "untrusted": func(b *Binding) { b.Source = "registrant" }, "empty evidence": func(b *Binding) { b.Evidence = nil }, "unknown atom": func(b *Binding) { b.Atom = "submitted" }, "extra identity": func(b *Binding) { b.Verification = "v_unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			x := b
			change(&x)
			r, _ := json.Marshal(x)
			d.Items[0].Bindings = []json.RawMessage{r}
			if d.Validate() == nil {
				t.Fatal("accepted mismatched binding")
			}
		})
	}
	raw = append(raw[:len(raw)-1], []byte(`,"verified":true}`)...)
	if _, err := ParseBinding(raw); err == nil {
		t.Fatal("accepted unknown field")
	}
	old := b.SignoffRef()
	b.Evidence = []string{"sha256:" + strings.Repeat("b", 64)}
	if old == b.SignoffRef() {
		t.Fatal("signoff ignored evidence version")
	}
}
func TestR410HasFiveDistinctVerificationSources(t *testing.T) {
	d := document(t)
	seen := map[string]bool{}
	for _, n := range d.Items {
		if n.Node != "4.10" {
			continue
		}
		for _, a := range n.Acceptance {
			if seen[a.Rule] || !strings.Contains(a.Rule, a.ID+":owner_signoff") {
				t.Fatal("shared external acceptance", a.Rule)
			}
			seen[a.Rule] = true
		}
	}
	if len(seen) != 5 {
		t.Fatal("missing independent acceptances")
	}
}
