package store_test

// TD-386: a blocked task's card says why the check failed (moved out of
// blockedcard_test.go, which crossed code-health's 800-line limit).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// verifyJSON is agentd's verify.json for commands, each an argv, its exit
// code, whether it timed out and its output's tail, and reason.
func verifyJSON(t *testing.T, reason string, commands ...struct {
	argv     []string
	exit     int
	timedOut bool
	tail     string
}) string {
	t.Helper()
	type trace struct {
		Argv     []string `json:"argv"`
		ExitCode int      `json:"exit_code"`
		Seconds  float64  `json:"seconds"`
		TimedOut bool     `json:"timed_out,omitempty"`
		Tail     string   `json:"tail"`
	}
	v := struct {
		Pass     bool    `json:"pass"`
		Commands []trace `json:"commands"`
		Reason   string  `json:"reason,omitempty"`
	}{Commands: []trace{}, Reason: reason}
	for _, c := range commands {
		v.Commands = append(v.Commands, trace{Argv: c.argv, ExitCode: c.exit, Seconds: 1.5, TimedOut: c.timedOut, Tail: c.tail})
	}
	b, err := json.MarshalIndent(v, "", "  ")
	ok(t, err)
	return string(b)
}

type traced = struct {
	argv     []string
	exit     int
	timedOut bool
	tail     string
}

// passing is three commands that passed, each with a tail of about 4000
// bytes, whose lines say which it is.
func passing() []traced {
	var cs []traced
	for i, argv := range [][]string{{"go", "-C", "api", "build", "./..."}, {"go", "-C", "api", "vet", "./..."}, {"npm", "--prefix", "console", "test"}} {
		cs = append(cs, traced{argv: argv, tail: strings.Repeat(fmt.Sprintf("passing output %d\n", i), 4000/17)})
	}
	return cs
}

// TD-386: a deterministic check's card quotes its first failed command and
// the end of that command's output, wherever it is in verify.json; not the
// report's start, nor what passing commands wrote.
func TestBlockedCardQuotesTheFailedCommand(t *testing.T) {
	t.Parallel()
	failed := traced{argv: []string{"go", "-C", "api", "test", "./internal/x"}, exit: 1,
		tail: strings.Repeat("--- FAIL: TestX (0.00s)\n", 400) + "    x_test.go:12: got 2, want 3\nFAIL\tgithub.com/x 0.1s\n"}
	report := verifyJSON(t, "", append(passing(), failed)...)
	if strings.Index(report, `FAIL\tgithub.com/x`) < 4096 {
		t.Fatal("the failed command should lie past the report's first 4096 bytes")
	}
	body := store.BlockedCardBodyWithCheckForTest("deterministic", report, "t", "deterministic failed in round 2 of 2", "")
	for _, want := range []string{"First failed command: go -C api test ./internal/x exited 1.\nIts output ends:\n\n",
		"x_test.go:12: got 2, want 3\nFAIL\tgithub.com/x 0.1s\n", "(cut; the whole report is on the task's page)\n--- FAIL: TestX (0.00s)\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("the card lacks %q:\n%s", want, body)
		}
	}
	for _, not := range []string{"passing output", "Its report begins", `"pass"`, "timed out"} {
		if strings.Contains(body, not) {
			t.Errorf("the card has %q:\n%s", not, body)
		}
	}
	if n := questions.BodyBytes(body); n > questions.BodyLimit {
		t.Errorf("%d bytes, more than the question store's %d", n, questions.BodyLimit)
	}

	// A timed-out command says so; a long argv is cut; an output with no
	// line in its end is cut mid-line, from its front.
	long := append([]string{"sh", "-c"}, strings.Repeat("a", 500))
	report = verifyJSON(t, "", traced{argv: long, exit: -1, timedOut: true, tail: strings.Repeat("z", 3000) + "killed"})
	body = store.BlockedCardBodyWithCheckForTest("deterministic", report, "t", "deterministic failed in round 2 of 2", "")
	for _, want := range []string{"First failed command: sh -c aaa", "… exited -1, timed out.\nIts output ends:\n\n(cut;", "zzkilled\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("a timed-out command: the card lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, strings.Repeat("a", 300)) {
		t.Errorf("a long command is not cut:\n%s", body)
	}

	// A command that failed writing nothing.
	report = verifyJSON(t, "", traced{argv: []string{"false"}, exit: 1})
	if body = store.BlockedCardBodyWithCheckForTest("deterministic", report, "t", "r", ""); !strings.Contains(body,
		"First failed command: false exited 1; it wrote nothing.\n") {
		t.Errorf("a silent command:\n%s", body)
	}
}

// TD-386: a deterministic check where no command failed quotes verify.json's
// reason; a report that is not verify.json reads from its start, as before.
func TestBlockedCardDeterministicFallbacks(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ report, want, not string }{
		"injected": {verifyJSON(t, "failure injected for this attempt (STG)", passing()...),
			"No command failed; its reason:\n\nfailure injected for this attempt (STG)\n", "passing output"},
		"withheld":  {verifyJSON(t, "no commands: the spec gives none"), "No command failed; its reason:\n\nno commands: the spec gives none\n", "Its report"},
		"no reason": {verifyJSON(t, "", passing()...), "Its report begins:\n\n{\n  \"pass\": false", "Its output ends"},
		"not JSON": {"h4failed\n" + strings.Repeat("報", 2000) + "\nthe end", "Its report begins:\n\nh4failed\n報報",
			"the end"},
	} {
		body := store.BlockedCardBodyWithCheckForTest("deterministic", c.report, "t", "deterministic failed in round 2 of 2", "")
		if !strings.Contains(body, c.want) || strings.Contains(body, c.not) {
			t.Errorf("%s: want %q and not %q:\n%s", name, c.want, c.not, body)
		}
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			t.Errorf("%s: %d bytes, more than the question store's %d", name, n, questions.BodyLimit)
		}
	}
	if body := store.BlockedCardBodyWithCheckForTest("deterministic", "h4failed\n"+strings.Repeat("報", 2000), "t", "r", ""); !strings.Contains(body,
		"報\n(cut; the whole report is on the task's page)\n") {
		t.Errorf("a report's start is not cut from its end:\n%s", body)
	}
}

// TD-386: with a long output's end, a long reason, words and title, the card
// still fits the question store, its excerpt cut from its front: the
// output's last line and a review's VERDICT stay.
func TestBlockedCardCutsAnEndFromItsFront(t *testing.T) {
	t.Parallel()
	title, reason, words := strings.Repeat("修", 200), strings.Repeat("🚧", 500), strings.Repeat(`"`, 300)
	failed := traced{argv: []string{"go", "-C", "api", "test", "./internal/x"}, exit: 1,
		tail: strings.Repeat("報告の行\n", 900) + "FAIL\tgithub.com/x 0.1s"}
	for name, c := range map[string]struct{ method, report, last string }{
		"deterministic": {"deterministic", verifyJSON(t, "", append(passing(), failed)...), "FAIL\tgithub.com/x 0.1s\n"},
		"review":        {"review", strings.Repeat(strings.Repeat("報", 300)+"\n", 40) + "VERDICT: FAIL", "VERDICT: FAIL\n"},
	} {
		body := store.BlockedCardBodyWithCheckForTest(c.method, c.report, title, reason, words)
		if n := questions.BodyBytes(body); n > questions.BodyLimit {
			t.Errorf("%s: %d bytes, more than the question store's %d", name, n, questions.BodyLimit)
		}
		for _, want := range []string{c.last, "(cut; the whole report is on the task's page)\n",
			store.BlockedRetry + ": it goes back to ready for round 3 of 3, and a host takes it as usual."} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the card lacks %q:\n%s", name, want, body)
			}
		}
		if name == "deterministic" && !strings.Contains(body, "First failed command: go -C api test ./internal/x exited 1.") {
			t.Errorf("%s: the card lacks its command:\n%s", name, body)
		}
	}
}
