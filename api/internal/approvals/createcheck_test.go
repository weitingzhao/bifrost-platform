package approvals

import (
	"context"
	"net/http"
	"os"
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
		"raw after delete":    "kubectl delete --raw /apis",
		"raw after apply":     "kubectl apply --raw /apis -f x",
		"raw outside kubectl": "curl --raw /apis",
		"raw without get":     "kubectl --raw /apis",
		"client dry run":      "kubectl apply -f x --dry-run=client",
		"bare dry run":        "kubectl apply -f x --dry-run",
		"own kubeconfig":      "kubectl --kubeconfig /tmp/k get pods",
		"own server":          "kubectl --server=https://x get pods",
		"drop not in reason":  "psql -c 'DROP TABLE t'",
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

// Origin: card #576 draft, 2026-11-17 (CI line): a CronJob script that pages a
// list with "kubectl get --raw" was refused with every --raw; and its
// DRY="${DRY_RUN:+--dry-run=server}" was read as a dry run that is not
// server. Main coordinator, 2026-11-17: --raw is a read only when the kubectl
// subcommand is get, read by position; a dry run that is not the server's
// stays refused whatever it is wrapped in. #550 (a trial run with --raw really
// deleted) is the reason the rest stays refused.
func TestRawIsAReadOnlyWithGet(t *testing.T) {
	svc := New(filepath.Join(t.TempDir(), "approvals"), nil)
	allowed := map[string]string{
		"get raw":                   "kubectl get --raw /apis",
		"get raw with flags":        `kubectl -n cicd get --raw "/apis/x?limit=500" >page.json`,
		"get raw, long flag":        "kubectl --namespace cicd get --raw /apis",
		"get raw, flag with equals": "kubectl --namespace=cicd get --raw /apis",
		"get raw in a pipeline":     "kubectl get nodes | kubectl get --raw /apis",
		"dry run in an expansion":   `DRY="${DRY_RUN:+--dry-run=server}"; kubectl delete pvc x ${DRY}`,
		"dry run in a comment":      "# deletes are sent with --dry-run=server.\ntrue",
	}
	for name, command := range allowed {
		if res := buildOwnerRun(svc, command, twoLines); res.Status != http.StatusCreated {
			t.Errorf("allowed %s: %d %v", name, res.Status, res.Body)
		}
	}
	refused := map[string]string{
		"delete raw":                    "kubectl delete --raw /apis",
		"create raw":                    "kubectl create --raw /apis -f x",
		"replace raw":                   "kubectl replace --raw /apis -f x",
		"apply raw":                     "kubectl apply --raw /apis -f x",
		"a made-up subcommand":          "kubectl -n get apply2 --raw /x",
		"no subcommand":                 "kubectl --raw /apis",
		"raw outside kubectl":           "curl --raw /apis",
		"get, then a delete with raw":   "kubectl get x; kubectl delete --raw /y",
		"delete with raw, then a get":   "kubectl delete --raw /y; kubectl get z",
		"raw and a server dry run":      "kubectl delete --raw /y --dry-run=server",
		"delete raw inside a heredoc":   "kubectl apply -f - <<'EOF'\nkind: ConfigMap\nEOF\nkubectl -n x delete --raw /y\n",
		"a client dry run":              "kubectl apply -f x --dry-run=client",
		"a bare dry run":                "kubectl apply -f x --dry-run",
		"a client dry run in a braces":  `D="${V:+--dry-run=client}"; kubectl apply -f x ${D}`,
		"dry run with a variable value": "kubectl apply -f x --dry-run=${MODE}",
		"dry run server with a suffix":  "kubectl apply -f x --dry-run=server-x",
		"dry run with a longer word":    "kubectl apply -f x --dry-run=servers",
	}
	for name, command := range refused {
		if res := buildOwnerRun(svc, command, twoLines); res.Status != http.StatusBadRequest {
			t.Errorf("refused %s: %d %v", name, res.Status, res.Body)
		}
	}
	// The command that was refused at 09:15: it must now pass the check.
	raw, err := os.ReadFile("testdata/ttl_cronjob_card_command.sh")
	if err != nil {
		t.Fatal(err)
	}
	if problem := ownerRunProblem(string(raw), twoLines); problem != "" {
		t.Fatalf("the CronJob card command is refused: %s", problem)
	}
}
