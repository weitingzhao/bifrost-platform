package store

import (
	"encoding/json"
	"fmt"
	"time"
)

// Presence (W-31 round 3, 3.5; CUT-online-status-2026-10-23 option A: the
// Owner's 「主机掉线或执行者不可用，Console 和参谋长简报 1 分钟内说出来」). A
// host is online while its last poll is at most HostLostAfter old; what it
// offers, and whether its free disk is below the floor, is what its latest
// claim said (RecordHostOffered: a note when that changed, no table or
// column). From those, the controls, the accounts' pools and the jobs'
// candidates, an executor is available when some candidate of it may take
// work now: a host that is online, active, under no stop, pause or drain and
// with room on disk offers a tool the candidate fits, and its account neither
// cools down nor reads at its tenant's quota stop. GET /hosts and GET
// /progress read the hosts' document (HostsSQL), and Progress and the
// controller's presence pass (presencepass.go) read the same view
// (readPresence), so the Console and the Owner's conversation say the same
// thing.
//
// Each kind of presence record of one host or executor has a stream of its
// own (review 526 B2), so its latest state is the stream's last event, read
// through the stream's head (headSQL): two index probes, whatever else the
// runtime has recorded and however long ago the state was set. Nothing else
// is written to these streams.

// headSQL is the SQL of the last event of the stream the SQL expression
// stream names, as one jsonb value {"type", "at", "body"}, NULL while the
// stream has none. It is a scalar subquery, so it is read by the stream's
// head and the event's key for each row it is asked for, never joined
// against the events at large (review 526 B2: a LATERAL join of the same
// was planned as a hash join over every event).
func headSQL(stream string) string {
	return `(SELECT jsonb_build_object('type', e.event_type, 'at', e.at, 'body', e.body_json) FROM agentrt.event e
	    WHERE e.event_stream_id = ` + stream + ` AND e.seq = (SELECT s.last_seq FROM agentrt.event_stream s WHERE s.event_stream_id = ` + stream + `))`
}

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
