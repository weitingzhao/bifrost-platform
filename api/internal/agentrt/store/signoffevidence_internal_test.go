package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

type signoffReadTx struct {
	pgx.Tx
	body    []byte
	missing []string
	ref     string
}

func (s signoffReadTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	return collectorTestRow(func(dest ...any) error {
		switch {
		case strings.Contains(q, "FROM agentrt.event"):
			if !strings.Contains(q, "actor_id='owner'") || !strings.Contains(q, "ORDER BY seq DESC LIMIT 1") || args[1] != s.ref {
				return fmt.Errorf("reader did not select latest exact Owner signoff")
			}
			if s.body == nil {
				return pgx.ErrNoRows
			}
			*dest[0].(*int64) = 42
			if len(dest) > 1 {
				*dest[1].(*[]byte) = s.body
			}
		case strings.Contains(q, "FROM unnest"):
			*dest[0].(*[]string) = s.missing
		default:
			return fmt.Errorf("unexpected read: %s", q)
		}
		return nil
	})
}
func TestPV2ReadsEvidenceFromLatestOwnerEvent(t *testing.T) {
	t.Parallel()
	_, b := collectorTestFixture(t)
	b.Source = "owner_signoff"
	b.Atom = "external_verified"
	b.Repo = ""
	b.Result = ""
	id := "sha256:" + strings.Repeat("c", 64)
	valid := acceptanceSignoff{Binding: b.Digest(), Acceptance: b.AcceptanceSHA256, Version: b.Version, Evidence: []string{id}}
	for _, name := range []string{"valid", "unsigned", "no evidence", "missing artifact", "binding changed", "acceptance changed", "plan changed"} {
		t.Run(name, func(t *testing.T) {
			signed := valid
			switch name {
			case "no evidence":
				signed.Evidence = nil
			case "binding changed":
				signed.Binding = strings.Repeat("d", 64)
			case "acceptance changed":
				signed.Acceptance = strings.Repeat("e", 64)
			case "plan changed":
				signed.Version++
			}
			raw, _ := json.Marshal(signed)
			tx := signoffReadTx{body: raw, ref: b.SignoffRef()}
			if name == "unsigned" {
				tx.body = nil
			}
			if name == "missing artifact" {
				tx.missing = []string{id}
			}
			n := planObservation{Value: "unknown", Evidence: []string{}}
			if err := readPlanSource(context.Background(), tx, remoteRead{}, b, &n); err != nil {
				t.Fatal(err)
			}
			if name == "valid" {
				if n.Value != "true" || !slices.Equal(n.Evidence, []string{"event:42", id}) {
					t.Fatalf("lost signing evidence: %+v", n)
				}
			} else if n.Value == "true" {
				t.Fatalf("invalid signing became true: %+v", n)
			}
		})
	}
}
func TestPV2EvidenceLimits(t *testing.T) {
	t.Parallel()
	ids := []string{}
	for i := 0; i < 21; i++ {
		ids = append(ids, fmt.Sprintf("sha256:%064x", i))
	}
	for _, n := range []int{0, 1, 20, 21} {
		_, err := signoffEvidence(ids[:n])
		if (err == nil) != (n >= 1 && n <= 20) {
			t.Fatalf("count %d: %v", n, err)
		}
	}
	for _, bad := range [][]string{{ids[0], ids[0]}, {"event:42"}, {"sha256:" + strings.Repeat("G", 64)}} {
		if _, err := signoffEvidence(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestPV2BindingRejectsEveryEvidenceField(t *testing.T) {
	t.Parallel()
	db, _ := collectorTestFixture(t)
	d := db.doc
	a := d.Items[0].Acceptance[0]
	b := planview.Binding{V: 1, Kind: planview.BindingKind, ID: "sign", Version: 1, Tenant: d.Tenant, Root: d.Root, Node: d.Items[0].Node, Step: "R4:4.1-1", Acceptance: a.ID, AcceptanceSHA256: planview.AcceptanceDigest(a), Atom: "external_verified", Source: "owner_signoff"}
	raw, _ := json.Marshal(b)
	if _, err := planview.ParseBinding(raw); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`[]`, `null`, `["sha256:` + strings.Repeat("a", 64) + `"]`} {
		with := append(slices.Clone(raw[:len(raw)-1]), []byte(`,"evidence":`+value+`}`)...)
		if _, err := planview.ParseBinding(with); err == nil {
			t.Fatal("accepted binding evidence", value)
		}
	}
}
