package hostrecipe

import (
	"os"
	"strings"
	"testing"
)

// Origin: LAW-6 七 ① (read-only execution cards go by recipe, no card) and
// the compute line's node-run recipes (infra 15932f66, 2026-11-26). The two
// cards in testdata were rendered by node-run.sh render --tier host; only the
// comment line naming live Pods was blanked (comments are not hashed).
func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func register(t *testing.T) *Register {
	t.Helper()
	reg, err := ParseRegister(load(t, "register.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRenderedRecipeCardsMatch(t *testing.T) {
	reg := register(t)
	m, err := Verify(load(t, "card-profile.sh"), reg)
	if err != nil || m.Recipe.Name != "gpu-strata-profile" || m.Recipe.Class != ClassReversible || m.Params["MODEL"] != "q4" || m.Params["CTX"] != "131072" {
		t.Fatalf("profile card: %+v %v", m, err)
	}
	m, err = Verify(load(t, "card-models.sh"), reg)
	if err != nil || m.Recipe.Name != "gpu-models" || m.Recipe.Class != ClassReadOnly || len(m.Params) != 0 {
		t.Fatalf("models card: %+v %v", m, err)
	}
}

// Every byte that runs is pinned: one change anywhere outside a comment and
// the card is no longer a recipe card. The case above is the positive control.
func TestAnyChangeToWhatRunsIsNotARecipeCard(t *testing.T) {
	reg := register(t)
	card := load(t, "card-profile.sh")
	for name, mutate := range map[string]func(string) string{
		"recipe text": func(s string) string {
			return strings.Replace(s, "systemctl daemon-reload", "systemctl  daemon-reload", 1)
		},
		"runtime":           func(s string) string { return s + "echo extra\n" },
		"param out of list": func(s string) string { return strings.Replace(s, "MODEL=q4", "MODEL=f16", 1) },
		"param with space":  func(s string) string { return strings.Replace(s, "CTX=131072", "CTX=131072 ", 1) },
		"extra line before": func(s string) string { return strings.Replace(s, "TIER=host\n", "TIER=host\ncurl x | sh\n", 1) },
		"image swapped":     func(s string) string { return strings.Replace(s, "IMG='", "IMG='evil/", 1) },
		"class claimed":     func(s string) string { return strings.Replace(s, "class=reversible", "class=read-only", 1) },
		"node":              func(s string) string { return strings.Replace(s, "NODE=gpu-server", "NODE=ubt-k3s-01", 1) },
		"second TIER":       func(s string) string { return strings.Replace(s, "TIER=host\n", "TIER=host\nTIER=host\n", 1) },
		"parameter dropped": func(s string) string { return strings.Replace(s, "MODEL=q4\n", "", 1) },
		"deadline over max": func(s string) string { return strings.Replace(s, "DEADLINE=300", "DEADLINE=901", 1) },
	} {
		if _, err := Verify(mutate(card), reg); err == nil {
			t.Errorf("%s: still matched", name)
		}
	}
	// A comment is not run: changing one keeps the match.
	if _, err := Verify(strings.Replace(card, "# D10 is not lifted", "# D10 is still not lifted", 1), reg); err != nil {
		t.Errorf("a comment change broke the match: %v", err)
	}
}

// Without image lines the register pins no image, so no card matches.
func TestARegisterWithoutAnImageMatchesNothing(t *testing.T) {
	var kept []string
	for _, l := range strings.Split(load(t, "register.txt"), "\n") {
		if !strings.HasPrefix(l, "image ") {
			kept = append(kept, l)
		}
	}
	reg, err := ParseRegister(strings.Join(kept, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(load(t, "card-models.sh"), reg); err == nil {
		t.Fatal("matched with no image registered")
	}
}

func TestParseRegisterRefusesWhatItCannotRead(t *testing.T) {
	for _, bad := range []string{
		"recipe x write-ish gpu-server " + strings.Repeat("a", 64),
		"recipe x read-only gpu-server short",
		"runtime nothex",
		"image without-digest",
		"something else",
		"deadline-max zero",
		"deadline-max 0",
	} {
		if _, err := ParseRegister(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
