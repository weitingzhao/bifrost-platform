package hostops

import (
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// Same signed inventory, policy, request parameter and effects throughout.
// A second manifest has identical file bytes but a distinct signed identity.
// Membership in the policy's allowed artifact pool cannot replace selection A.
func TestReview604SelectedManifestMustBeReceived(t *testing.T) {
	for _, action := range []string{"host_compose_reconcile", "host_monitoring_install", "host_agentd_update"} {
		t.Run(action, func(t *testing.T) {
			_, a := typedAdmission(t, action)
			k := rptest.NewKey(t)
			tr := Trust{k.Fingerprint}
			var x Request
			if err := Decode(a.RequestJSON, &x); err != nil {
				t.Fatal(err)
			}
			var inv Inventory
			if err := Decode([]byte(a.Inventory.Text), &inv); err != nil {
				t.Fatal(err)
			}
			var alternate Bundle
			if err := Decode([]byte(a.Bundles[0].Manifest.Text), &alternate); err != nil {
				t.Fatal(err)
			}
			selectedBundle := alternate
			selectedBundle.Meta.ExpiresAt = testNow.Add(30 * time.Minute).Format(time.RFC3339)
			selectedSigned := signed(t, k, selectedBundle, NamespaceBundle)
			selected := Digest([]byte(selectedSigned.Text))
			a.Bundles[0].Manifest = selectedSigned
			a.Bundles[0].Mark.Digest = selected
			x.Params["bundle"] = selected
			x.BundleDigests = []string{selected}
			a.RequestJSON = canonical(x)
			for n := range inv.Objects {
				if inv.Objects[n].ID == a.ObjectID {
					inv.Objects[n].ActionScopes[0].Params["bundle"] = enumScope(selected)
				}
			}
			a.Inventory = signed(t, k, inv, NamespaceInventory)
			a.InventoryMark.Digest = Digest([]byte(a.Inventory.Text))
			alternate.Meta.ID = "different-manifest"
			alternateSigned := signed(t, k, alternate, NamespaceBundle)
			alternateDigest := Digest([]byte(alternateSigned.Text))
			var pb PolicyBundle
			if err := Decode([]byte(a.PolicySet.Baseline.Text), &pb); err != nil {
				t.Fatal(err)
			}
			pb.Rules[0].InventoryDigest = a.InventoryMark.Digest
			pb.Rules[0].Clauses[0].Params["bundle"] = []string{selected}
			pb.Rules[0].Clauses[0].BundleDigests = []string{selected, alternateDigest}
			a.PolicySet = PolicySet{signed(t, k, pb, NamespacePolicy), []Tightening{}}
			p, err := tr.AdmitBaseline(a.PolicySet.Baseline, nil, "test", testNow)
			if err != nil {
				t.Fatal(err)
			}
			a.State = testState(p)
			if d := tr.EvaluateM(a, testNow); !d.Allowed {
				t.Fatalf("positive control: %+v", d)
			}
			later := testNow.Add(time.Hour)
			expired := cloneAdmission(a)
			expired.Facts.ClockSyncedAt = later
			if d := tr.EvaluateM(expired, later); d.Allowed || !strings.Contains(d.Reason, "bundle outside signed scope") {
				t.Fatalf("expired A control must refuse for manifest validity: %+v", d)
			}
			x.BundleDigests = []string{alternateDigest}
			a.RequestJSON = canonical(x)
			a.Bundles = []ReceivedBundle{{alternateSigned, ArtifactMark{alternate.Meta.Version, alternateDigest}, a.Bundles[0].Files}}
			for _, now := range []time.Time{testNow, later} {
				a.Facts.ClockSyncedAt = now
				if d := tr.EvaluateM(a, now); d.Allowed {
					t.Errorf("at %s: selected manifest %s (expires %s) not received; only %s verified; decision=%+v", now.Format(time.RFC3339), selected, selectedBundle.Meta.ExpiresAt, alternateDigest, d)
				}
			}
		})
	}
}

// A real rollback source requires its own manifest. Selecting the forward
// manifest must not collapse this set, nor exempt either manifest from checks.
func TestReview604SelectedAndRollbackManifestSet(t *testing.T) {
	for _, action := range []string{"host_compose_reconcile", "host_monitoring_install", "host_agentd_update"} {
		t.Run(action, func(t *testing.T) {
			_, a := typedAdmission(t, action)
			k := rptest.NewKey(t)
			tr := Trust{k.Fingerprint}
			var x Request
			var inv Inventory
			var forward Bundle
			var pb PolicyBundle
			for _, input := range []struct {
				raw []byte
				out any
			}{
				{a.RequestJSON, &x}, {[]byte(a.Inventory.Text), &inv},
				{[]byte(a.Bundles[0].Manifest.Text), &forward}, {[]byte(a.PolicySet.Baseline.Text), &pb},
			} {
				if err := Decode(input.raw, input.out); err != nil {
					t.Fatal(err)
				}
			}
			forward.Meta.ExpiresAt = testNow.Add(30 * time.Minute).Format(time.RFC3339)
			forwardSigned := signed(t, k, forward, NamespaceBundle)
			selected := Digest([]byte(forwardSigned.Text))
			a.Bundles[0].Manifest = forwardSigned
			a.Bundles[0].Mark = ArtifactMark{forward.Meta.Version, selected}

			rollback, files := bundleFixture()
			rollback.Meta.ID = "rollback-manifest"
			rollback.Actions = []string{action}
			files[0].Path = "rollback/exporter"
			files[0].Bytes = []byte("distinct rollback test bytes, never run")
			rollback.Entries[0].Path = files[0].Path
			rollback.Entries[0].Size = uint64(len(files[0].Bytes))
			rollback.Entries[0].SHA256 = Digest(files[0].Bytes)
			rollback.Entries[0].Purpose = "rollback"
			rollbackSigned := signed(t, k, rollback, NamespaceBundle)
			rollbackDigest := Digest([]byte(rollbackSigned.Text))
			a.Bundles = append(a.Bundles, ReceivedBundle{rollbackSigned, ArtifactMark{rollback.Meta.Version, rollbackDigest}, files})
			x.Params["bundle"] = selected
			x.BundleDigests = []string{selected, rollbackDigest}
			a.RequestJSON = canonical(x)
			for n := range inv.Objects {
				if inv.Objects[n].ID == a.ObjectID {
					s := &inv.Objects[n].ActionScopes[0]
					s.Params["bundle"] = enumScope(selected)
					s.Rollback[0].SourceDigest = rollback.Entries[0].SHA256
				}
			}
			a.RollbackEffects[0].SourceDigest = rollback.Entries[0].SHA256
			a.Inventory = signed(t, k, inv, NamespaceInventory)
			a.InventoryMark.Digest = Digest([]byte(a.Inventory.Text))
			pb.Rules[0].InventoryDigest = a.InventoryMark.Digest
			pb.Rules[0].Clauses[0].Params["bundle"] = []string{selected}
			pb.Rules[0].Clauses[0].BundleDigests = []string{selected, rollbackDigest}
			a.PolicySet = PolicySet{signed(t, k, pb, NamespacePolicy), []Tightening{}}
			p, err := tr.AdmitBaseline(a.PolicySet.Baseline, nil, "test", testNow)
			if err != nil {
				t.Fatal(err)
			}
			a.State = testState(p)
			if d := tr.EvaluateM(a, testNow); !d.Allowed {
				t.Fatalf("forward and distinct rollback bundles: %+v", d)
			}
			t.Run("reordered", func(t *testing.T) {
				b := cloneAdmission(a)
				b.Bundles = []ReceivedBundle{a.Bundles[1], a.Bundles[0]}
				if d := tr.EvaluateM(b, testNow); !d.Allowed {
					t.Fatalf("manifest set order changed admission: %+v", d)
				}
			})
			for _, mutation := range []string{
				"selected-not-committed", "selected-not-received", "selected-omitted",
				"selected-not-installed", "selected-wrong-version", "selected-invalid-signature", "selected-invalid-bytes",
				"selected-expired", "rollback-omitted", "rollback-not-installed", "rollback-invalid-signature",
				"rollback-invalid-bytes", "duplicate-manifest", "extra-uncommitted-manifest",
			} {
				t.Run(mutation, func(t *testing.T) {
					b := cloneAdmission(a)
					b.Bundles = append([]ReceivedBundle{}, a.Bundles...)
					for n := range b.Bundles {
						b.Bundles[n].Files = append([]ReceivedFile{}, a.Bundles[n].Files...)
					}
					var req Request
					if err := Decode(b.RequestJSON, &req); err != nil {
						t.Fatal(err)
					}
					now := testNow
					switch mutation {
					case "selected-not-committed":
						req.BundleDigests = []string{rollbackDigest}
					case "selected-not-received":
						b.Bundles = b.Bundles[1:]
					case "selected-omitted":
						req.BundleDigests = []string{rollbackDigest}
						b.Bundles = b.Bundles[1:]
					case "selected-not-installed":
						b.Bundles[0].Mark = ArtifactMark{}
					case "selected-wrong-version":
						b.Bundles[0].Mark.Version++
					case "selected-invalid-signature":
						b.Bundles[0].Manifest.Signature = b.Bundles[1].Manifest.Signature
					case "selected-invalid-bytes":
						b.Bundles[0].Files[0].Bytes = []byte("tampered")
					case "selected-expired":
						now = testNow.Add(time.Hour)
						b.Facts.ClockSyncedAt = now
					case "rollback-omitted":
						req.BundleDigests = []string{selected}
						b.Bundles = b.Bundles[:1]
					case "rollback-not-installed":
						b.Bundles[1].Mark = ArtifactMark{}
					case "rollback-invalid-signature":
						b.Bundles[1].Manifest.Signature = b.Bundles[0].Manifest.Signature
					case "rollback-invalid-bytes":
						b.Bundles[1].Files[0].Bytes = []byte("tampered")
					case "duplicate-manifest":
						req.BundleDigests = []string{selected, selected}
						b.Bundles[1] = b.Bundles[0]
					case "extra-uncommitted-manifest":
						b.Bundles = append(b.Bundles, b.Bundles[1])
					}
					b.RequestJSON = canonical(req)
					if d := tr.EvaluateM(b, now); d.Allowed {
						t.Fatalf("manifest binding bypass: %+v", d)
					}
				})
			}
		})
	}
}
