package planimport_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// The Console's plan panes read the plan with a TypeScript port of
// FromPlanWithRequest (console/src/api/agentRuntimePlanView.ts), held to this
// function's outputs by its test on the cases of the fixture below (W-31 2.5
// slice 2, review 416 C3 and 418). This test holds this function to the same
// outputs, so a change to the view's rule or refusals goes red here until the
// fixture is regenerated, and the port with it:
//
//	CONSOLE_PLAN_VIEW_WRITE=1 go test ./internal/agentrt/planimport/ -run TestConsolePlanViewFixture
//
// rewrites each case's output from its stored input; then run the Console's
// tests (npx vitest run src/api/__tests__/agentRuntimePlanView.test.ts) and
// bring the port along. The inputs themselves are the handlers' answers on a
// test database; how they are captured is in that test's header.

const consoleFixtures = "../../../../console/src/api/__fixtures__"

type consoleCase struct {
	Name    string           `json:"name"`
	Plan    string           `json:"plan"`
	Root    string           `json:"root"`
	Request bool             `json:"request"`
	View    *planimport.View `json:"view,omitempty"`
	Error   string           `json:"error,omitempty"`
}

type consoleViewFixture struct {
	Cases  []consoleCase              `json:"cases"`
	Inputs map[string]json.RawMessage `json:"inputs"`
}

func readConsoleFixture(t *testing.T, name string, v any) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(consoleFixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return body
}

func TestConsolePlanViewFixture(t *testing.T) {
	const viewFile = "agentrt-plan-view.go.json"
	var fx consoleViewFixture
	stored := readConsoleFixture(t, viewFile, &fx)
	var imported, moved struct {
		Plan    json.RawMessage `json:"plan"`
		Request json.RawMessage `json:"request"`
	}
	readConsoleFixture(t, "agentrt-plan-imported.testdb.json", &imported)
	readConsoleFixture(t, "agentrt-plan-moved.testdb.json", &moved)
	var req planimport.Request
	if err := json.Unmarshal(imported.Request, &req); err != nil {
		t.Fatal(err)
	}
	docs := map[string][]byte{"imported": imported.Plan, "moved": moved.Plan}
	for name, doc := range fx.Inputs {
		docs[name] = doc
	}
	if len(fx.Cases) == 0 {
		t.Fatalf("%s holds no cases", viewFile)
	}

	got := make([]consoleCase, len(fx.Cases))
	for i, c := range fx.Cases {
		doc, ok := docs[c.Plan]
		if !ok {
			t.Fatalf("case %q reads input %q, which no fixture holds", c.Name, c.Plan)
		}
		var r *planimport.Request
		if c.Request {
			r = &req
		}
		g := consoleCase{Name: c.Name, Plan: c.Plan, Root: c.Root, Request: c.Request}
		if v, err := planimport.FromPlanWithRequest(doc, c.Root, r); err != nil {
			g.Error = err.Error()
		} else {
			g.View = &v
		}
		got[i] = g
	}

	if os.Getenv("CONSOLE_PLAN_VIEW_WRITE") != "" {
		raw, err := json.Marshal(consoleViewFixture{Cases: got, Inputs: fx.Inputs})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := json.Indent(&out, raw, "", " "); err != nil {
			t.Fatal(err)
		}
		out.WriteByte('\n')
		if !bytes.Equal(out.Bytes(), stored) {
			if err := os.WriteFile(filepath.Join(consoleFixtures, viewFile), out.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("rewrote %s", viewFile)
		}
		return
	}
	for i := range got {
		want, err := json.Marshal(fx.Cases[i])
		if err != nil {
			t.Fatal(err)
		}
		have, err := json.Marshal(got[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("case %q: FromPlanWithRequest gives\n%s\nthe Console's fixture holds\n%s\n(regenerate with CONSOLE_PLAN_VIEW_WRITE=1, then bring the Console's port along)",
				fx.Cases[i].Name, have, want)
		}
	}
}
