package main

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// TD-460 ratchet: every platform action CANARY.md tells an operator to
// request must exist in the action catalog, with the parameters the catalog
// takes. Action references are the first column of a table headed
// "action", and the "action" of a JSON request example.
var (
	canaryActionCell = regexp.MustCompile("^`([a-z][a-z0-9_]*)`$")
	canaryJSONAction = regexp.MustCompile(`"action"\s*:\s*"([^"]+)"`)
)

type canaryRequest struct {
	Action string
	Params map[string]any
}

func canaryActions(doc string) (names []string, requests []canaryRequest) {
	inTable, inCode := false, false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			for _, m := range canaryJSONAction.FindAllStringSubmatch(trimmed, -1) {
				names = append(names, m[1])
			}
			var req struct {
				Action string         `json:"action"`
				Params map[string]any `json:"params"`
			}
			if strings.HasPrefix(trimmed, "{") && json.Unmarshal([]byte(trimmed), &req) == nil && req.Action != "" {
				requests = append(requests, canaryRequest{req.Action, req.Params})
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			inTable = false
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		first := strings.TrimSpace(cells[0])
		switch {
		case strings.EqualFold(first, "action"):
			inTable = true
		case inTable && strings.Trim(first, "-: ") == "":
		case inTable:
			if m := canaryActionCell.FindStringSubmatch(first); m != nil {
				names = append(names, m[1])
			} else {
				names = append(names, first) // an unrecognised cell must still be a catalog id
			}
		}
	}
	return names, requests
}

func checkCanaryActions(doc string) []string {
	var problems []string
	names, requests := canaryActions(doc)
	for _, name := range names {
		if _, ok := actions.ByID(name); !ok {
			problems = append(problems, "unknown action "+name)
		}
	}
	for _, req := range requests {
		act, ok := actions.ByID(req.Action)
		if !ok {
			continue
		}
		known := map[string]bool{}
		for _, p := range act.Params {
			known[p.Name] = true
			if _, has := req.Params[p.Name]; p.Required && !has {
				problems = append(problems, req.Action+" example lacks required "+p.Name)
			}
		}
		for key := range req.Params {
			if !known[key] {
				problems = append(problems, req.Action+" example has unknown param "+key)
			}
		}
	}
	return problems
}

func TestCanaryActionsExistInCatalog(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("CANARY.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if problems := checkCanaryActions(doc); len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
	// Positive control: the scan must actually see the sample steps and the
	// stop path, or an empty scan would pass.
	names, requests := canaryActions(doc)
	for _, want := range []string{"stg_release_sample_create", "stg_release_sample_finalize", "gitops_sync_app", "rollout_restart_deployment"} {
		if !slices.Contains(names, want) {
			t.Errorf("scan did not find %s in CANARY.md", want)
		}
	}
	var examples []string
	for _, r := range requests {
		examples = append(examples, r.Action)
	}
	if !slices.Contains(examples, "stg_release_sample_create") || !slices.Contains(examples, "stg_release_sample_finalize") {
		t.Errorf("CANARY.md must carry a request example for each sample action, found %v", examples)
	}
}

// Negative control: the checker reports a missing action, an action written
// in a table's free text, and an example with a wrong or missing param.
func TestCanaryActionCheckerCatchesDrift(t *testing.T) {
	t.Parallel()
	doc := strings.Join([]string{
		"| action | params |", "|---|---|",
		"| `stg_release_sample_creat` | x |",
		"| create the sample | x |",
		"",
		"```json",
		`{"action":"stg_release_sample_finalize","params":{"env":"stg","run":"r","uuid":"u"}}`,
		"```",
	}, "\n")
	got := checkCanaryActions(doc)
	for _, want := range []string{"unknown action stg_release_sample_creat", "unknown action create the sample",
		"stg_release_sample_finalize example lacks required uid", "stg_release_sample_finalize example has unknown param uuid"} {
		if !slices.Contains(got, want) {
			t.Errorf("checker missed %q in %v", want, got)
		}
	}
}
