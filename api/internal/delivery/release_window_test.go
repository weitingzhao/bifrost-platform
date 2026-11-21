package delivery

import (
	"testing"
	"time"
)

func TestResearchRefusedWithoutAWindow(t *testing.T) {
	msg := decideReleaseWindow(false, nil, "bifrost-deliver-research", "")
	if !stringsContains(msg, "REFUSED") || !stringsContains(msg, "bifrost-research") {
		t.Fatalf("got %q", msg)
	}
}

func TestResearchAllowedForTheHolder(t *testing.T) {
	window := map[string]any{"who": "ada@host", "what": "bifrost-research"}
	if msg := decideReleaseWindow(true, window, "bifrost-deliver-research", "ada@host"); msg != "" {
		t.Fatalf("holder refused: %s", msg)
	}
	if msg := decideReleaseWindow(true, window, "bifrost-build-research-dagster", "ada@host"); msg != "" {
		t.Fatalf("dagster holder refused: %s", msg)
	}
}

func TestMissingWhoIsRefusedWhenWindowMatches(t *testing.T) {
	window := map[string]any{"who": "ada@host", "what": "bifrost-platform,bifrost-ui"}
	msg := decideReleaseWindow(true, window, "bifrost-deliver-platform", "")
	if !stringsContains(msg, "missing who") || !stringsContains(msg, "ada@host") {
		t.Fatalf("got %q", msg)
	}
}

func TestSomeoneElseIsRefused(t *testing.T) {
	window := map[string]any{"who": "ada@host", "what": "bifrost-research"}
	msg := decideReleaseWindow(true, window, "bifrost-deliver-research", "bob@host")
	if !stringsContains(msg, "someone else") {
		t.Fatalf("got %q", msg)
	}
	if msg := decideReleaseWindow(true, window, "bifrost-deliver-stg", "ada@host"); !stringsContains(msg, "someone else") {
		t.Fatalf("trade during a research window: %q", msg)
	}
}

func TestTradeAllowedWhenNoWindowIsOpen(t *testing.T) {
	if msg := decideReleaseWindow(false, nil, "bifrost-deliver-stg", ""); msg != "" {
		t.Fatalf("got %q", msg)
	}
}

func TestPluginBuildsAreGuarded(t *testing.T) {
	for _, name := range []string{"bifrost-build-market-data", "bifrost-build-flex-query", "bifrost-build-ib-gateway"} {
		if msg := decideReleaseWindow(false, nil, name, ""); !stringsContains(msg, "REFUSED") {
			t.Fatalf("%s started with no window: %q", name, msg)
		}
	}
	held := map[string]any{"who": "ada@host", "what": "bifrost-platform-plugin"}
	if msg := decideReleaseWindow(true, held, "bifrost-build-ib-gateway", "ada@host"); msg != "" {
		t.Fatalf("ib gateway holder refused: %s", msg)
	}
}

func TestExpiredWindowIsEmpty(t *testing.T) {
	window := map[string]any{
		"who":        "ada@host",
		"what":       "bifrost-research",
		"expires_at": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}
	if msg := decideReleaseWindow(true, window, "bifrost-deliver-research", ""); !stringsContains(msg, "no release window") {
		t.Fatalf("expired window was still open: %q", msg)
	}
	future := map[string]any{
		"who":        "ada@host",
		"what":       "bifrost-research",
		"expires_at": time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}
	if msg := decideReleaseWindow(true, future, "bifrost-deliver-research", "ada@host"); msg != "" {
		t.Fatalf("live window refused the holder: %q", msg)
	}
}

func TestFullSHA(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if !isFullGitSHA(sha) {
		t.Fatal("expected a sha")
	}
	if isFullGitSHA("main") || isFullGitSHA(sha[:39]) || isFullGitSHA("A"+sha[1:]) {
		t.Fatal("non-sha accepted")
	}
	if !requiresFullSHA("bifrost-deliver-research") || requiresFullSHA("bifrost-deliver-stg") {
		t.Fatal("sha pipeline set drifted")
	}
}

func stringsContains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub)))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
