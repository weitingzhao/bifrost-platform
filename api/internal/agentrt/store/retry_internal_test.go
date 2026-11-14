package store

import (
	"strings"
	"testing"
)

// Every policy_alert names its artifact but repeated_failure, whose event's
// attempt is what it is about (TD-381).
func TestPolicyAlertNamesItsArtifact(t *testing.T) {
	t.Parallel()
	sha := "sha256:" + strings.Repeat("ab", 32)
	for _, c := range []struct {
		body map[string]any
		ok   bool
	}{
		{map[string]any{"rule": "repeated_failure", "source": "attempt", "phrase": repeatedFailurePhrase}, true},
		{map[string]any{"rule": "local_path", "source": "review", "phrase": localPathPhrase, "artifact_id": sha}, true},
		{map[string]any{"rule": "local_path", "source": "review", "phrase": localPathPhrase}, false},
		{map[string]any{"rule": "approval_claim", "source": "message", "phrase": "Owner 已批准"}, false},
		{map[string]any{"rule": "workspace_ref", "source": "report", "phrase": workspaceRefPhrase}, false},
	} {
		if err := checkBody("policy_alert", c.body); (err == nil) != c.ok {
			t.Errorf("%v: %v; want ok %v", c.body, err, c.ok)
		}
	}
}
