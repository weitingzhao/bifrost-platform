package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// Historical host and executor shapes remain for event validation and
// compatibility. The dispatch presence view and controller are retired.

// Offer is a tool a host offers, as its claim named it and its stream records
// it.
type Offer struct {
	Executor string `json:"executor"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

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

// HostPresence is a host as the hosts document has it.
type HostPresence struct {
	HostID          string       `json:"host_id"`
	HostKind        string       `json:"host_kind"`
	State           string       `json:"state"`
	LastPollAt      *time.Time   `json:"last_poll_at"`
	Online          bool         `json:"online"`
	ReportedOffline bool         `json:"reported_offline"`
	Pruning         bool         `json:"pruning"`
	Claiming        *bool        `json:"claiming"`
	ContactAt       *time.Time   `json:"contact_at"`
	DiskLow         bool         `json:"disk_low"`
	LiveAttempts    int          `json:"live_attempts"`
	Offered         *[]Offer     `json:"offered"`
	Build           *AgentdBuild `json:"build"`
	BuildAt         *time.Time   `json:"build_at"`
	Controls        []struct {
		Kind string `json:"kind"`
	} `json:"controls"`
}

// ExecutorPresence is an executor some job's candidate names: whether it is
// available (nil: not known, while a host that could take work has not said
// what it offers), why, the hosts that offer it now, and the jobs it is a
// candidate of.
type ExecutorPresence struct {
	Executor  string   `json:"executor"`
	Available *bool    `json:"available"`
	Why       string   `json:"why"`
	Hosts     []string `json:"hosts"`
	Jobs      []string `json:"jobs"`
}

// presence is the view at one moment: the hosts' document as read, its hosts,
// the executors, the jobs no executor can take now (each has candidates and
// each of them is known not to be available) and the jobs it is not known of
// (none of their candidates is known available, some is not known).
type presence struct {
	now       time.Time
	doc       []byte
	hosts     []HostPresence
	executors []ExecutorPresence
	unserved  []string
	unknown   []string
	unjudged  map[string]any // presencejudge.go
	deferred  map[string]any
}

// judged is a candidate judged: available or not (nil: not known), why, and
// the usable hosts that offer a tool it fits.
type judged struct {
	ok    *bool
	why   []string
	hosts []string
}
