package landing

import (
	"testing"
	"time"
)

func TestS65PreparedIdentitySurvivesFold(t *testing.T) {
	t.Parallel()
	rs := records()[:4]
	rs[1]["v"], rs[1]["platform_revision"], rs[1]["generation"] = Version, A, 1
	s := mustFold(t, rs)
	o := s.Operations["land:1"]
	if o.PlatformRevision != A || o.Generation != 1 || o.R != R || o.M0 != M0 {
		t.Fatal(o)
	}
	for _, change := range []string{"missing sibling", "missing generation", "floating sibling", "wrong generation", "extra", "old version"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			r := rs[1]
			bad := Record{}
			for k, v := range r {
				bad[k] = v
			}
			switch change {
			case "missing sibling":
				delete(bad, "platform_revision")
			case "missing generation":
				delete(bad, "generation")
			case "floating sibling":
				bad["platform_revision"] = "main"
			case "wrong generation":
				bad["generation"] = 2
			case "extra":
				bad["extra"] = true
			case "old version":
				bad["v"] = 1
			}
			if _, err := Fold(entries([]Record{rs[0], bad})); err == nil {
				t.Fatal("invalid preparation accepted", bad)
			}
		})
	}
	// Older infra preparations remain readable but carry no invented key.
	old := mustFold(t, records()[:4]).Operations["land:1"]
	if old.PlatformRevision != "" || old.Generation != 0 {
		t.Fatal("invented historical identity", old)
	}
}
func TestS65CISeenCarriesSelectedEvidence(t *testing.T) {
	t.Parallel()
	ci := CIRun{Run: "ci-run", UID: "uid", R: R, Status: "Succeeded", CreatedAt: epoch.Add(2 * time.Minute),
		Results: map[string]string{"infra-commit": R, "base-commit": M0, "platform-commit": A, "tools-from": M0, "doc-policy": "pass", "purpose": "landing"}}
	record, err := Parse(CIRecord("land:1", ci))
	if err != nil {
		t.Fatal(err)
	}
	if CIChanged(record, ci) {
		t.Fatal("same evidence changed")
	}
	changed := ci
	changed.UID = "replacement"
	if !CIChanged(record, changed) {
		t.Fatal("UID replacement ignored")
	}
	changed = ci
	changed.Results = map[string]string{"doc-policy": "skipped"}
	if !CIChanged(record, changed) {
		t.Fatal("results changed silently")
	}
	for _, which := range []string{"uid", "results", "extra result", "old version"} {
		r := CIRecord("land:1", ci)
		switch which {
		case "uid":
			delete(r, "uid")
		case "results":
			delete(r, "results")
		case "extra result":
			r["results"] = map[string]string{"unknown": "pass"}
		case "old version":
			r["v"] = 1
		}
		if _, err := Parse(r); err == nil {
			t.Fatal("open evidence shape", which)
		}
	}
	ci.Results = nil
	old, err := Parse(CIRecord("land:1", ci))
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 7 || CIChanged(old, ci) {
		t.Fatal("platform record changed", old)
	}
}
