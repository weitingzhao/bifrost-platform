package hostops

import (
	"errors"
	"time"
)

type ReceivedBundle struct {
	Manifest Signed
	Mark     ArtifactMark
	Files    []ReceivedFile
}

// Admission is a reference input assembled independently at each of the three
// verifiers. Only RequestJSON originates with the requester. Observations,
// identities, marks, effects, history and clocks are trusted adapter inputs.
type Admission struct {
	RequestJSON           []byte
	AuthenticatedExecutor string
	Environment           string
	PreinstalledActions   []string
	ObjectID              string
	Inventory             Signed
	InventoryMark         ArtifactMark
	PolicySet             PolicySet
	State                 SafetyState
	Facts                 Facts
	Catalog               SafetyCatalog
	ForwardEffects        []Effect
	RollbackEffects       []Effect
	Bundles               []ReceivedBundle
}

// EvaluateM composes the S1 checks; a true result still needs S2's atomic
// reservation and S3's local recheck, durable intent and online write permit.
func (t Trust) EvaluateM(a Admission, now time.Time) Decision {
	var x Request
	if err := Decode(a.RequestJSON, &x); err != nil {
		return denied(err.Error())
	}
	if x.ExecutorID != a.AuthenticatedExecutor || x.Environment != a.Environment {
		return denied("authenticated identity mismatch")
	}
	if !contains(a.PreinstalledActions, x.Action) {
		return denied("unsupported: action capability not installed/admitted")
	}
	if len(a.ForwardEffects) == 0 || len(a.RollbackEffects) == 0 {
		return denied("missing forward or rollback effect proof")
	}
	if err := a.Catalog.Boundary(x.Action, "M", append(append([]Effect{}, a.ForwardEffects...), a.RollbackEffects...)); err != nil {
		return denied(err.Error())
	}
	i, err := t.Inventory(a.Inventory)
	if err != nil {
		return denied(err.Error())
	}
	if a.InventoryMark.Version != i.Meta.Version || a.InventoryMark.Digest != Digest([]byte(a.Inventory.Text)) {
		return denied("inventory not locally installed")
	}
	o, err := i.InventoryScope(x, a.ObjectID, now)
	if err != nil {
		return denied(err.Error())
	}
	if NeverM(o.Canonical) {
		return denied("never-M object")
	}
	if x.Action == "host_compose_reconcile" && o.Stateful {
		return denied("stateful recreate requires H")
	}
	if contains([]string{"host_service_restart", "host_service_state", "host_container_restart"}, x.Action) {
		name, ok := x.Params["service"]
		if !ok || name != o.Canonical && !contains(o.Aliases, name) {
			return denied("service does not resolve to registered object")
		}
	}
	p, err := t.Load(a.PolicySet, a.State.PolicyHead, a.State.MaxRuleVersion)
	if err != nil {
		return denied(err.Error())
	}
	f := a.Facts
	f.InventoryDigest = Digest([]byte(a.Inventory.Text))
	f.CatalogDigest = a.Catalog.Digest
	if len(x.BundleDigests) != len(a.Bundles) {
		return denied("manifest set mismatch")
	}
	if contains([]string{"host_compose_reconcile", "host_image_stage", "host_monitoring_install", "host_agentd_update", "host_toolchain_install", "host_validation_run"}, x.Action) && len(a.Bundles) == 0 {
		return denied("signed bundle required")
	}
	seen := map[string]bool{}
	sources := []string{x.HandlerDigest, o.ConfigDigest}
	for _, received := range a.Bundles {
		d := Digest([]byte(received.Manifest.Text))
		if seen[d] || !contains(x.BundleDigests, d) {
			return denied("duplicate or unauthorized manifest")
		}
		seen[d] = true
		b, err := t.Bundle(received.Manifest)
		if err != nil {
			return denied(err.Error())
		}
		if received.Mark.Version != b.Meta.Version || received.Mark.Digest != d {
			return denied("bundle not locally installed")
		}
		if err := b.VerifyFiles(received.Files, x.Environment, x.Host, x.Action, x.HandlerDigest, now); err != nil {
			return denied(err.Error())
		}
		for _, e := range b.Entries {
			sources = append(sources, e.SHA256)
		}
	}
	for _, e := range append(append([]Effect{}, a.ForwardEffects...), a.RollbackEffects...) {
		if !contains(sources, e.SourceDigest) {
			return denied("effect proof does not bind verified bytes")
		}
	}
	return p.Allow(x, now, a.State, f)
}

// ValidateReadScope does not depend on M policy validity. The read allowlist
// and restrictions come from a locally installed, currently signed inventory.
func (t Trust) ValidateReadScope(s Signed, mark ArtifactMark, x Request, objectID string, catalog SafetyCatalog, effects []Effect, now time.Time) error {
	if !validParams(x.Action, x.Params) || !nonemptyParams(x.Params) {
		return errors.New("invalid read parameters")
	}
	if err := catalog.Boundary(x.Action, "R", effects); err != nil {
		return err
	}
	i, err := t.Inventory(s)
	if err != nil {
		return err
	}
	if mark.Version != i.Meta.Version || mark.Digest != Digest([]byte(s.Text)) {
		return errors.New("inventory not installed")
	}
	_, err = i.InventoryScope(x, objectID, now)
	return err
}
