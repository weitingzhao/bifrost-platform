package store

import (
	"strings"
	"testing"
)

// B-02: the trust chain's answers have a shape: the question text alone, an
// unrelated numbered list, a missing, empty, duplicated, unexplained or
// copied answer are all refused; the four structured answers pass.
func TestC5B02TrustAnswersAreStructured(t *testing.T) {
	t.Parallel()
	answers := func(lines ...string) string {
		return "The diff is correct.\n\n## Trust chain answers\n" + strings.Join(lines, "\n") + "\n"
	}
	full := []string{
		"- T1: No, approvers and tiers are untouched in this diff.",
		"- T2: No, the material page and its digests are unchanged.",
		"- T3: Yes — the token is now read from a file at start, not per call.",
		"- T4: No, the gate's checks and their order are as before.",
	}
	if !trustAnswered([]byte(answers(full...))) {
		t.Fatal("the control is not answered")
	}
	if !trustAnswered([]byte(answers(full...) + "\n## Other notes\nnothing\n")) {
		t.Fatal("a section after the answers hides them")
	}
	for name, text := range map[string]string{
		"questions only":          TrustReviewText,
		"questions then heading":  TrustReviewText + "\n## Trust chain answers\n",
		"unrelated numbered list": "# General review\n1. Formatting looks good.\n2. Naming looks good.\n3. Tests pass.\n4. Build passes.\n",
		"no heading":              strings.Join(full, "\n"),
		"one missing":             answers(full[:3]...),
		"one empty":               answers(full[0], full[1], full[2], "- T4:"),
		"no yes or no":            answers(full[0], full[1], full[2], "- T4: the gate is fine as far as I can see"),
		"yes alone":               answers(full[0], full[1], full[2], "- T4: Yes"),
		"copied question":         answers(full[0], full[1], full[2], "- T4: No, does it change how the landing gate decides, or open a way around it"),
		"question back":           answers(full[0], full[1], full[2], "- T4: No, who knows whether the gate changed?"),
		"placeholder":             answers(full[0], full[1], full[2], "- T4: No, <what in the diff shows it>"),
		"duplicated":              answers(full[0], full[1], full[2], full[3], full[3]),
		"answers before heading":  strings.Join(full, "\n") + "\n## Trust chain answers\n",
	} {
		if trustAnswered([]byte(text)) {
			t.Errorf("%s accepted", name)
		}
	}
}
