package coord

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A claim alert from a letter names the letter's artifact; one from a plan
// note names none (its text is in the note); the rules other domains once
// wrote are not this type any more and are refused.
//
// Origin: 2026-11-17, TD-583 (step 6 of the note and policy_alert split):
// policy_alert became coordination's, narrowed to the approval claim. It
// replaces the store's TestPolicyAlertNamesItsArtifact (TD-381), whose other
// rules each have their own type now or are retired.
func TestPolicyAlertIsTheApprovalClaim(t *testing.T) {
	t.Parallel()
	sha := "sha256:" + strings.Repeat("ab", 32)
	phrase := ApprovalClaims[0]
	for _, c := range []struct {
		body map[string]any
		ok   bool
	}{
		{map[string]any{"rule": "approval_claim", "source": "message", "phrase": phrase, "artifact_id": sha}, true},
		{map[string]any{"rule": "approval_claim", "source": "message", "phrase": phrase}, false},
		{map[string]any{"rule": "approval_claim", "source": "plan_note", "phrase": phrase}, true},
		{map[string]any{"rule": "approval_claim", "source": "message", "phrase": "not a claim", "artifact_id": sha}, false},
		{map[string]any{"rule": "local_path", "source": "review", "phrase": "a local path (H8)", "artifact_id": sha}, false},
		{map[string]any{"rule": "repeated_failure", "source": "attempt", "phrase": "failed three times in a row; not offered the task again"}, false},
	} {
		if err := eventlog.CheckBody("policy_alert", c.body); (err == nil) != c.ok {
			t.Errorf("%v: %v; want ok %v", c.body, err, c.ok)
		}
	}
}

// policy_alert is registered in bus/registry/ledger/types.yaml under
// coordination.
//
// Origin: 2026-11-17, TD-583.
func TestPolicyAlertIsRegisteredUnderCoordination(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "bus", "registry", "ledger", "types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Types []struct{ Type, Domain string } `yaml:"types"`
	}
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, lt := range doc.Types {
		if lt.Type == "policy_alert" {
			if lt.Domain != "coordination" {
				t.Fatalf("policy_alert is registered under %q", lt.Domain)
			}
			return
		}
	}
	t.Fatalf("policy_alert is not registered (%d types read)", len(doc.Types))
}
