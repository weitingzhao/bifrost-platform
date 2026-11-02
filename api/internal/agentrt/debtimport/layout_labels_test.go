package debtimport

import (
	"slices"
	"testing"
)

// The layout label set decides which words between bindings are dropped as
// template rather than stored as import_raw. A word added to it silently
// moves prose out of the text (adding "PASS" or "Bifrost" broke no other
// test, review of 946b6b49), so the set is pinned exactly: changing it is a
// decision this test makes visible.
func TestLayoutLabelsAreExactlyTheTemplateLabels(t *testing.T) {
	want := []string{"状态", "审批", "代价", "风险", "repos", "验收结果", "验收", "防线"}
	if !slices.Equal(layoutLabels, want) {
		t.Fatalf("layoutLabels = %q; want exactly %q (a new label is a decision: update this test with the reason)", layoutLabels, want)
	}
}
