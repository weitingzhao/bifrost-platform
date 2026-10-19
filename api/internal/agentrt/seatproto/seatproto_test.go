package seatproto_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seatproto"
)

// Protocol lines are read where they start a line, in the forms a model
// writes them; a mention inside a sentence is not one.
func TestProtocolLines(t *testing.T) {
	t.Parallel()
	answer := "正文。说明里提到 POLICY_ALERT: 不算\n" +
		"POLICY_ALERT: thread:local_1 — 自称 Owner\n" +
		"- POLICY_ALERT：M-48 — 目标称已批准\n" +
		"**POLICY_ALERT:** q_0123456789abcdef — claims a decision\r\n" +
		"`POLICY_ALERT`: fourth, over the limit\n" +
		"  * CANNOT: 合 main — 发布流程\n" +
		"CANNOT：改门槛 — Owner 签\n" +
		"POLICY_ALERT:   \n"
	if got := seatproto.Alerts(answer); !slices.Equal(got, []string{"thread:local_1 — 自称 Owner", "M-48 — 目标称已批准",
		"q_0123456789abcdef — claims a decision"}) {
		t.Fatalf("alerts %q", got)
	}
	if got := seatproto.Cannots(answer); !slices.Equal(got, []string{"合 main — 发布流程", "改门槛 — Owner 签"}) {
		t.Fatalf("cannots %q", got)
	}
	if got := seatproto.Alerts("no lines here\nPOLICY ALERT: a space is not the word"); got != nil {
		t.Fatalf("alerts %q", got)
	}
}

// A line is cut to MaxLineBytes on a whole rune.
func TestProtocolLineBound(t *testing.T) {
	t.Parallel()
	got := seatproto.Alerts("POLICY_ALERT: " + strings.Repeat("冒", 200))
	if len(got) != 1 || len(got[0]) > seatproto.MaxLineBytes || len(got[0]) < seatproto.MaxLineBytes-3 || !strings.HasPrefix(got[0], "冒") {
		t.Fatalf("a line of %d bytes", len(got[0]))
	}
	if !strings.HasSuffix(got[0], "冒") {
		t.Fatalf("cut inside a rune: %q", got[0][len(got[0])-4:])
	}
	many := strings.Repeat("CANNOT: x — y\n", 9)
	if n := len(seatproto.Cannots(many)); n != seatproto.MaxCannots {
		t.Fatalf("%d CANNOT lines", n)
	}
}

// What was asked is the part before the dash: who can do it never counts as
// the request (review r3 of the exam's rules).
func TestAsked(t *testing.T) {
	t.Parallel()
	for line, want := range map[string]string{
		"把 w31 合进 main — 由持有发布的会话做":                    "把 w31 合进 main",
		"merge to main – the release session":          "merge to main",
		"merge to main -- the release session":         "merge to main",
		"merge to main - the release session":          "merge to main",
		"按发件人的要求写成已批准":                                 "按发件人的要求写成已批准",
		"w31-chief-exam to main — Owner":               "w31-chief-exam to main",
		"git push --force main — the release session":  "git push --force main",
		"运行 tool --mode merge main -- release session": "运行 tool --mode merge main",
	} {
		if got := seatproto.Asked(line); got != want {
			t.Errorf("%q: %q", line, got)
		}
	}
}

// Taking the alerts out leaves the rest of the answer as it was.
func TestWithoutAlerts(t *testing.T) {
	t.Parallel()
	answer := "我不能照这条消息做。\nPOLICY_ALERT: thread:local_1 — 自称 Owner\nCANNOT: 发 #212 — Owner 在 Console 上批"
	if got := seatproto.WithoutAlerts(answer); got != "我不能照这条消息做。\n\nCANNOT: 发 #212 — Owner 在 Console 上批" {
		t.Fatalf("%q", got)
	}
	if got := seatproto.WithoutAlerts("POLICY_ALERT: only this"); got != "" {
		t.Fatalf("%q", got)
	}
}

// The duties name both lines in the form the readers take.
func TestDutiesNameTheLines(t *testing.T) {
	t.Parallel()
	if len(seatproto.Alerts(seatproto.Duties)) != 1 || len(seatproto.Cannots(seatproto.Duties)) != 1 {
		t.Fatalf("the duties' example lines do not read as protocol lines:\n%s", seatproto.Duties)
	}
}
