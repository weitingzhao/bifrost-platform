package guardblock

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/registry" // declares the body
)

// Origin: 2026-11-21, the wall list for the law-reform seat needed how often
// each guard rule refuses whom, and that was read only from one Mac's session
// transcripts (no system archive, law 47). A block of a known rule passes the
// register with or without a host; the controls: an unknown rule, a session
// with a space and a host with a slash are refused, and nothing in the body
// can carry the command.
func TestABlockIsABodyTheRegisterAccepts(t *testing.T) {
	for _, b := range []Block{
		{Rule: "owner", Session: "4558a214-eeec-4214-8403-73520d03de28"},
		{Rule: "gitea-primary", Session: "local_c00f7b95", Host: "vision-mac-trader"},
		{Rule: "D10"},
	} {
		body, err := Body(b)
		if err != nil {
			t.Fatalf("%+v: %v", b, err)
		}
		if err := eventlog.CheckBody(Type, body); err != nil {
			t.Fatalf("%+v refused by the register: %v", b, err)
		}
	}
	for name, b := range map[string]Block{
		"unknown rule": {Rule: "cat-secret", Session: "s"},
		"space":        {Rule: "owner", Session: "a b"},
		"host with /":  {Rule: "owner", Session: "s", Host: "a/b"},
	} {
		if _, err := Body(b); err == nil {
			t.Errorf("%s: accepted, want refused", name)
		}
	}
}
