package store

import (
	"context"
	"testing"
)

// Exercise the response builder, including the execution facts read from its
// read-only snapshot. Real SQL and historical attempts are covered separately.
func TestPlanSignoffReadiness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		state     any
		candidate bool
		attempt   bool
		blocked   bool
		readiness string
	}{
		{name: "not started", state: "planned", readiness: "Not started"},
		{name: "candidate", state: "planned", candidate: true, readiness: "Ready"},
		{name: "active", state: "active", readiness: "Ready"},
		{name: "running", state: "running", readiness: "Ready"},
		{name: "done", state: "done", readiness: "Ready"},
		{name: "historical attempt", state: "planned", attempt: true, readiness: "Ready"},
		{name: "unknown", state: "Unknown", readiness: "Not started"},
		{name: "missing state", readiness: "Not started"},
		{name: "unknown with candidate", state: "Unknown", candidate: true, readiness: "Ready"},
		{name: "stopped without attempt", state: "stopped", readiness: "Not started"},
		{name: "blocked before start", state: "planned", blocked: true, readiness: "Blocked"},
		{name: "blocked with candidate", state: "planned", candidate: true, blocked: true, readiness: "Blocked"},
		{name: "blocked while active", state: "active", blocked: true, readiness: "Blocked"},
		{name: "blocked with attempt", state: "planned", attempt: true, blocked: true, readiness: "Blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSignoffViewFixture(t)
			step := "R4:4.1-1"
			if tc.blocked {
				step = "R4:4.2-1"
			}
			for _, n := range f.nodes {
				if n["plan_node_id"] == step {
					n["state"] = tc.state
				}
			}
			if !tc.candidate {
				delete(f.candidates, step)
			}
			f.attempted[step] = tc.attempt
			// Work and candidates for another step cannot make this one Ready.
			f.attempted["R4:4.3-1"] = true
			out, err := PlanView(context.Background(), f, f.doc.Root)
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, bucket := range []string{"owner_waiting", "owner_later"} {
				for _, raw := range out[bucket].([]any) {
					entry := raw.(map[string]any)
					if entry["plan_node_id"] != step {
						continue
					}
					found++
					if entry["readiness"] != tc.readiness || (bucket == "owner_waiting") != (tc.readiness == "Ready") {
						t.Fatalf("%s: %v; want %s", bucket, entry, tc.readiness)
					}
					if tc.readiness == "Not started" && entry["reason"] != "Work for this step has not started; no candidate evidence yet" {
						t.Fatalf("misleading reason: %v", entry)
					}
				}
			}
			if found != 1 {
				t.Fatalf("signoff occurs %d times", found)
			}
		})
	}
}
