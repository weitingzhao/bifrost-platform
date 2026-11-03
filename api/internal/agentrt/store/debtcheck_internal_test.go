package store

import (
	"errors"
	"testing"
)

// M-5: no free text a filing keeps names a local path (T13): title, area,
// a text's heading and body, evidence, and every string of the acceptance
// spec; the same filing without the path is taken.
func TestDebtFilingRefusesLocalPathsEverywhere(t *testing.T) {
	t.Parallel()
	const path = "~/scratch/private-notes.txt"
	base := func() DebtFiling {
		return DebtFiling{Project: "crew", Title: "t", Area: "area", Priority: "P2",
			Texts:      []DebtText{{Kind: "claim", Heading: "Measured", Body: "b"}},
			Evidence:   []DebtEvidence{{Kind: "code_ref", Repo: "bifrost-platform", Path: "api/x.go", Quote: "x"}},
			Acceptance: []any{map[string]any{"kind": "manual", "say": "look", "expect": "fine"}}}
	}
	if err := base().check(); err != nil {
		t.Fatalf("the filing without a path: %v", err)
	}
	for name, spoil := range map[string]func(*DebtFiling){
		"title":      func(f *DebtFiling) { f.Title = "see " + path },
		"area":       func(f *DebtFiling) { f.Area = path },
		"heading":    func(f *DebtFiling) { f.Texts[0].Heading = path },
		"body":       func(f *DebtFiling) { f.Texts[0].Body = path },
		"quote":      func(f *DebtFiling) { f.Evidence[0].Quote = path },
		"acceptance": func(f *DebtFiling) { f.Acceptance[0].(map[string]any)["say"] = "open " + path },
		"ci_test path": func(f *DebtFiling) {
			f.Acceptance = []any{map[string]any{"kind": "ci_test", "repo": "bifrost-platform", "path": path, "name": "TestX"}}
		},
	} {
		f := base()
		spoil(&f)
		if err := f.check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("a local path in the %s: %v, want ErrInvalid", name, err)
		}
	}
}
