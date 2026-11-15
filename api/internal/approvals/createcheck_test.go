package approvals

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Origin: the three-questions review of the gates (2026-11-15). The Owner
// decides an execution card in a median 61 seconds across 24 cards and cannot
// read a script, so a card carries two plain lines; the traps below each cost
// an approval before (#550 --raw trial that really deleted, #536/#537/#541
// connection and policy refusals, #558/#559 DROP not named in the reason).

const twoLines = "x\n改了什么：删一个无状态对象\n批了以后谁因此能做什么新事：没有新能力"

func buildOwnerRun(svc *Service, command, reason string) createResult {
	return svc.create(context.Background(), "s", "owner_run_command", reason, "", map[string]any{"command": command, "reason": "r"})
}

// Positive control: a card with both lines and none of the traps is built.
func TestOwnerRunWithTheTwoLinesIsBuilt(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	if res := buildOwnerRun(svc, "kubectl -n cicd delete configmap x --dry-run=server", twoLines); res.Status != http.StatusCreated {
		t.Fatalf("create = %d %v", res.Status, res.Body)
	}
}

func TestOwnerRunMissingALineIsRefusedAndSaysWhich(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	for name, reason := range map[string]string{
		"none":               "plain reason",
		"first only":         "x\n改了什么：删一个对象",
		"second only":        "x\n批了以后谁因此能做什么新事：没有",
		"label without text": "x\n改了什么：\n批了以后谁因此能做什么新事：没有",
	} {
		res := buildOwnerRun(svc, "true", reason)
		if res.Status != http.StatusBadRequest || !strings.Contains(res.Body["error"].(string), "改了什么") && !strings.Contains(res.Body["error"].(string), "批了以后") {
			t.Fatalf("%s: %d %v", name, res.Status, res.Body)
		}
	}
	// ASCII colon and a bullet are accepted.
	if res := buildOwnerRun(svc, "true", "- 改了什么: a\n- 批了以后谁因此能做什么新事: b"); res.Status != http.StatusCreated {
		t.Fatalf("ascii colon = %d %v", res.Status, res.Body)
	}
}

func TestOwnerRunKnownTrapsAreRefused(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	for name, command := range map[string]string{
		"raw":                "kubectl get --raw /apis",
		"client dry run":     "kubectl apply -f x --dry-run=client",
		"bare dry run":       "kubectl apply -f x --dry-run",
		"own kubeconfig":     "kubectl --kubeconfig /tmp/k get pods",
		"own server":         "kubectl --server=https://x get pods",
		"drop not in reason": "psql -c 'DROP TABLE t'",
	} {
		res := buildOwnerRun(svc, command, twoLines)
		if res.Status != http.StatusBadRequest {
			t.Fatalf("%s: %d %v", name, res.Status, res.Body)
		}
	}
	// A DROP the reason names is allowed.
	if res := buildOwnerRun(svc, "psql -c 'DROP TABLE t'", twoLines+"\nDROP TABLE t: 16 张空表，迁移里有不空就中止"); res.Status != http.StatusCreated {
		t.Fatalf("named DROP = %d %v", res.Status, res.Body)
	}
}

// A drill is shown, never approved: no lines needed.
func TestDrillNeedsNoLines(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	res := svc.createWith(context.Background(), createInput{Requester: "s", Action: "owner_run_command", Reason: "drill",
		Params: map[string]any{"command": "true", "reason": "r"}, Drill: true})
	if res.Status != http.StatusCreated {
		t.Fatalf("drill = %d %v", res.Status, res.Body)
	}
}
