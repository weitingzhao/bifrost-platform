package store

import (
	"encoding/json"
	"fmt"
)

// AgentdBuild is the build of agentd a host runs, as its claims report it: the
// commit it was built from, whether the work tree had changes then, and the
// sha256 of its binary. The platform knows by it which build a host runs,
// before and after agentd is deployed.
type AgentdBuild struct {
	Commit   string `json:"commit"`
	SHA256   string `json:"sha256"`
	Modified bool   `json:"modified"`
}

// Check says why b is not a build a claim may report, nil when it is: a
// commit of 40 hex and a sha256 of 64, both lower case.
func (b AgentdBuild) Check() error {
	switch {
	case !commitRE.MatchString(b.Commit):
		return fmt.Errorf("store: an agentd build's commit is 40 lower-case hex, not %q", b.Commit)
	case !sha256RE.MatchString(b.SHA256):
		return fmt.Errorf("store: an agentd build's sha256 is 64 lower-case hex, not %q", b.SHA256)
	}
	return nil
}

// badAgentdBuild is why v is not a note's agentd_build, or "".
func badAgentdBuild(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	var m map[string]json.RawMessage
	if err = json.Unmarshal(raw, &m); err != nil || len(m) != 3 || m["commit"] == nil || m["sha256"] == nil || m["modified"] == nil {
		return "an agentd build is exactly commit, sha256 and modified"
	}
	var b AgentdBuild
	if err = json.Unmarshal(raw, &b); err != nil {
		return err.Error()
	}
	if err = b.Check(); err != nil {
		return err.Error()
	}
	return ""
}
