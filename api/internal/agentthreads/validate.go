package agentthreads

// What a beat and a host heartbeat must look like before they are stored.

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	idRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	vendRe  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	workRe  = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9A-Za-z-]{1,24}$`)
	toolRe  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,96}$`)
	turnRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	keyRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	maxText = 160
)

// Validate checks a beat before it is stored.
func (b *Beat) Validate() error {
	b.Thread = strings.TrimSpace(b.Thread)
	b.Vendor = strings.TrimSpace(b.Vendor)
	b.Host = strings.TrimSpace(b.Host)
	b.Work = strings.TrimSpace(b.Work)
	b.Tool = strings.TrimSpace(b.Tool)
	b.Title = strings.Join(strings.Fields(b.Title), " ")
	b.TurnID = strings.TrimSpace(b.TurnID)
	b.Reason = strings.TrimSpace(b.Reason)
	b.ThreadKey = strings.TrimSpace(b.ThreadKey)
	b.RegisterNonce = strings.TrimSpace(b.RegisterNonce)
	b.Sidebar = strings.TrimSpace(b.Sidebar)
	b.Session = strings.TrimSpace(b.Session)
	b.HostIDSource = strings.TrimSpace(b.HostIDSource)
	if err := b.validateIdentity(); err != nil {
		return err
	}
	switch {
	case !idRe.MatchString(b.Thread):
		return fmt.Errorf("thread: want 1-128 of [A-Za-z0-9._:-]")
	case (b.sidebarPresent || b.Sidebar != "") && !idRe.MatchString(b.Sidebar):
		return fmt.Errorf("sidebar: want 1-128 of [A-Za-z0-9._:-]")
	case !vendRe.MatchString(b.Vendor):
		return fmt.Errorf("vendor: want a lowercase name")
	case !hostRe.MatchString(b.Host):
		return fmt.Errorf("host: want a host name")
	case b.Work != "" && !workRe.MatchString(b.Work):
		return fmt.Errorf("work: want an id like W-54")
	case b.Tool != "" && !toolRe.MatchString(b.Tool):
		return fmt.Errorf("tool: want a tool name")
	case b.ToolTimeoutS < 0 || b.ToolTimeoutS > maxToolTimeout:
		return fmt.Errorf("tool_timeout_s: want 0..%d", maxToolTimeout)
	case !turnRe.MatchString(b.TurnID):
		return fmt.Errorf("turn_id: want 1-128 of [A-Za-z0-9._:-]")
	case b.Seq == 0:
		return fmt.Errorf("seq: want a sequence that starts at 1")
	case b.ThreadKey != "" && !keyRe.MatchString(b.ThreadKey):
		return fmt.Errorf("thread_key: want 64 hex characters")
	case b.RegisterNonce != "" && !keyRe.MatchString(b.RegisterNonce):
		return fmt.Errorf("register_nonce: want 64 hex characters")
	}
	switch b.Event {
	case TurnStart, BeforeTool, AfterTool, TurnEnd, WaitingOwner, SessionStart, PreCompact:
	default:
		return fmt.Errorf("event: want turn_start, before_tool, after_tool, turn_end, waiting_owner, session_start or precompact")
	}
	switch b.Event {
	case WaitingOwner:
		if _, ok := waitingReasons[b.Reason]; !ok {
			return fmt.Errorf("reason: want permission_prompt, idle_prompt or elicitation")
		}
	case SessionStart:
		if _, ok := sideReasons[SessionStart][b.Reason]; !ok {
			return fmt.Errorf("reason: want startup, resume, clear, compact or fork")
		}
	case PreCompact:
		if _, ok := sideReasons[PreCompact][b.Reason]; !ok {
			return fmt.Errorf("reason: want manual or auto")
		}
	default:
		b.Reason = ""
	}
	if b.Event != BeforeTool {
		b.ToolTimeoutS = 0
	}
	if r := []rune(b.Title); len(r) > maxText {
		b.Title = string(r[:maxText])
	}
	return nil
}

// validateIdentity checks the session key and the diagnoses a beat names
// (§3.1): each optional, each of a closed shape.
func (b *Beat) validateIdentity() error {
	switch {
	case b.Session != "" && !idRe.MatchString(b.Session):
		return fmt.Errorf("session: want 1-128 of [A-Za-z0-9._:-]")
	case b.Epoch < 0 || b.Epoch > maxEpoch:
		return fmt.Errorf("epoch: want 0..%d", maxEpoch)
	case b.Epoch > 0 && b.Session == "":
		return fmt.Errorf("epoch: names the epoch of a session; send the session")
	case b.HostIDSource != "":
		if _, ok := hostIDSources[b.HostIDSource]; !ok {
			return fmt.Errorf("host_id_source: want file, env or hostname")
		}
	}
	if c := b.ClientBind; c != nil {
		c.State, c.Reason, c.At = strings.TrimSpace(c.State), strings.TrimSpace(c.Reason), strings.TrimSpace(c.At)
		if _, ok := clientBindStates[c.State]; !ok {
			return fmt.Errorf("client_bind.state: want unlinked, refused or linked")
		}
		if _, ok := clientBindReasons[c.Reason]; c.Reason != "" && !ok {
			return fmt.Errorf("client_bind.reason: want one of the closed reasons")
		}
		if _, err := time.Parse(time.RFC3339Nano, c.At); c.At != "" && err != nil {
			return fmt.Errorf("client_bind.at: want an RFC 3339 time")
		}
	}
	return nil
}

// Validate checks a host heartbeat.
func (b *HostBeat) Validate() error {
	b.Host = strings.TrimSpace(b.Host)
	if !hostRe.MatchString(b.Host) {
		return fmt.Errorf("host: want a host name")
	}
	if b.Vendors == nil {
		b.Vendors = map[string]VendorReport{}
	}
	for name := range b.Vendors {
		if !knownVendor(name) {
			return fmt.Errorf("vendor: want claude, cursor or codex")
		}
	}
	b.Vendors = fillVendors(b.Vendors)
	return nil
}

func knownVendor(name string) bool {
	for _, v := range Vendors {
		if v == name {
			return true
		}
	}
	return false
}
