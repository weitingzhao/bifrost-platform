package rules

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// External hosts of a standing envelope (W-31 round 4, core block C-1;
// bifrost-trade-infra agent-config/work/multi-agent/
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.1, §2.2). An entry names a
// host outside the cluster's (the coordinator's Mac), the pin a task carries
// to be leased there and only there, the seat that proposes for it (recorded;
// since cleanup wave 1 any seat may set the pin, store/pin.go), the executor
// and model provider that run it, and the
// repositories and roles a pinned task may have. It holds only in a version
// of the envelope the Owner signed (store/pin.go); dropping it tightens, so
// it needs no signature and stops new pins at once.

// PinCoordinator is the one executor pin of this version: the database
// allows no other (0010 task_executor_pin_check).
const PinCoordinator = "external:coordinator"

// ExternalHost is one external_hosts entry.
type ExternalHost struct {
	Host, Pin, Proposer, Executor, Provider string
	Repos, Roles                            []string
	Executors                               []ExternalExecutor
}

// ExternalExecutor names an executor and its provider on an external host.
type ExternalExecutor struct{ Executor, Provider string }

// Tools returns the permitted executor/provider pairs, including legacy entries.
func (h ExternalHost) Tools() []ExternalExecutor {
	if len(h.Executors) > 0 {
		return h.Executors
	}
	return []ExternalExecutor{{h.Executor, h.Provider}}
}

// The shapes of an entry's names: a host id, a seat, an executor actor
// (exec:<tool>, as the store's executors are) and a model provider.
var (
	externalHostID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	seatName       = regexp.MustCompile(`^seat:[a-z][a-z0-9_-]{0,62}$`)
	executorName   = regexp.MustCompile(`^exec:[a-z]+$`)
	providerName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
)

// pinnedRoles are the roles a pinned task may have: an implementation only
// (0010).
var pinnedRoles = []string{"implement"}

// externalHosts reads the optional external_hosts list of an envelope: when
// given, at least one entry, each host and each pin once.
func (e *Envelope) externalHosts(o *object) error {
	if _, given := o.fields["external_hosts"]; !given {
		return nil
	}
	hosts, pins := map[string]bool{}, map[string]bool{}
	err := o.list("external_hosts", false, func(n *yaml.Node, where string) error {
		m, err := mapping(n, where)
		if err != nil {
			return err
		}
		var h ExternalHost
		for _, f := range []struct {
			key  string
			at   *string
			re   *regexp.Regexp
			what string
		}{
			{"host", &h.Host, externalHostID, "a host id"},
			{"pin", &h.Pin, nil, ""},
			{"proposer", &h.Proposer, seatName, "a seat (seat:<name>)"},
		} {
			if *f.at, err = m.str(f.key, false); err != nil {
				return err
			}
			if f.re != nil && !f.re.MatchString(*f.at) {
				return fmt.Errorf("%s.%s is %q, not %s", where, f.key, *f.at, f.what)
			}
		}
		if _, multi := m.fields["executors"]; multi {
			if _, ok := m.fields["executor"]; ok {
				return fmt.Errorf("%s mixes executor and executors", where)
			}
			if _, ok := m.fields["provider"]; ok {
				return fmt.Errorf("%s mixes provider and executors", where)
			}
			seen := map[string]bool{}
			err = m.list("executors", false, func(n *yaml.Node, at string) error {
				x, err := mapping(n, at)
				if err != nil {
					return err
				}
				var tool ExternalExecutor
				if tool.Executor, err = x.str("executor", false); err != nil {
					return err
				}
				if tool.Provider, err = x.str("provider", false); err != nil {
					return err
				}
				if !executorName.MatchString(tool.Executor) || !providerName.MatchString(tool.Provider) {
					return fmt.Errorf("%s: invalid executor or provider", at)
				}
				key := tool.Executor + "/" + tool.Provider
				if seen[key] {
					return fmt.Errorf("%s: executor/provider listed twice", at)
				}
				seen[key] = true
				if err = x.done(); err != nil {
					return err
				}
				h.Executors = append(h.Executors, tool)
				return nil
			})
			if err != nil {
				return err
			}
			if len(h.Executors) == 0 {
				return fmt.Errorf("%s.executors lists at least one executor", where)
			}
			slices.SortFunc(h.Executors, func(a, b ExternalExecutor) int {
				return strings.Compare(a.Executor+"/"+a.Provider, b.Executor+"/"+b.Provider)
			})
		} else {
			if h.Executor, err = m.str("executor", false); err != nil {
				return err
			}
			if !executorName.MatchString(h.Executor) {
				return fmt.Errorf("%s.executor is not an executor", where)
			}
			if h.Provider, err = m.str("provider", false); err != nil {
				return err
			}
			if !providerName.MatchString(h.Provider) {
				return fmt.Errorf("%s.provider is not a model provider", where)
			}
		}
		if h.Pin != PinCoordinator {
			return fmt.Errorf("%s.pin is %q; the one pin there is is %q", where, h.Pin, PinCoordinator)
		}
		if h.Repos, err = m.strings("repos", false); err != nil {
			return err
		}
		if h.Roles, err = m.strings("roles", false); err != nil {
			return err
		}
		if err = m.done(); err != nil {
			return err
		}
		switch {
		case len(h.Repos) == 0:
			return fmt.Errorf("%s.repos lists at least one repository", where)
		case len(h.Roles) == 0:
			return fmt.Errorf("%s.roles lists at least one of %v", where, pinnedRoles)
		case hosts[h.Host]:
			return fmt.Errorf("%s: host %s is listed twice", where, h.Host)
		case pins[h.Pin]:
			return fmt.Errorf("%s: pin %s is listed twice", where, h.Pin)
		}
		for _, r := range h.Roles {
			if !slices.Contains(pinnedRoles, r) {
				return fmt.Errorf("%s.roles: %q is none of %v", where, r, pinnedRoles)
			}
		}
		hosts[h.Host], pins[h.Pin] = true, true
		slices.Sort(h.Repos)
		slices.Sort(h.Roles)
		e.ExternalHosts = append(e.ExternalHosts, h)
		return nil
	})
	if err != nil {
		return err
	}
	if len(e.ExternalHosts) == 0 {
		return fmt.Errorf("%s.external_hosts lists at least one host; leave it out for none", o.where)
	}
	slices.SortFunc(e.ExternalHosts, func(a, b ExternalHost) int { return strings.Compare(a.Host, b.Host) })
	return nil
}

// Pinned is the external host entry of a pin, if the envelope has one.
func (e *Envelope) Pinned(pin string) (ExternalHost, bool) {
	if e == nil {
		return ExternalHost{}, false
	}
	for _, h := range e.ExternalHosts {
		if h.Pin == pin {
			return h, true
		}
	}
	return ExternalHost{}, false
}

// externalHostsDoc is the list as the rule's document has it, nil when the
// envelope has none (so an envelope without it reads as it always did).
func (e *Envelope) externalHostsDoc() []any {
	if len(e.ExternalHosts) == 0 {
		return nil
	}
	out := make([]any, 0, len(e.ExternalHosts))
	for _, h := range e.ExternalHosts {
		doc := map[string]any{"host": h.Host, "pin": h.Pin, "proposer": h.Proposer, "executor": h.Executor,
			"provider": h.Provider, "repos": anyList(h.Repos), "roles": anyList(h.Roles)}
		if len(h.Executors) > 0 {
			delete(doc, "executor")
			delete(doc, "provider")
			tools := []any{}
			for _, x := range h.Executors {
				tools = append(tools, map[string]any{"executor": x.Executor, "provider": x.Provider})
			}
			doc["executors"] = tools
		}
		out = append(out, doc)
	}
	return out
}

// externalHostsTighten: every entry of next was in prev with the same host,
// pin, proposer, executor and provider, its repositories and roles no more.
// An entry dropped, or the whole list, tightens.
func externalHostsTighten(p, n []ExternalHost) error {
	for _, h := range n {
		i := slices.IndexFunc(p, func(q ExternalHost) bool { return q.Host == h.Host })
		if i < 0 {
			return fmt.Errorf("external host %s is new", h.Host)
		}
		q := p[i]
		if h.Pin != q.Pin || h.Proposer != q.Proposer {
			return errors.New("external host " + h.Host + " changes its pin, proposer, executor or provider")
		}
		for _, tool := range h.Tools() {
			if !slices.Contains(q.Tools(), tool) {
				return fmt.Errorf("external host %s adds executor/provider %s/%s", h.Host, tool.Executor, tool.Provider)
			}
		}
		if err := subset(h.Repos, q.Repos, "external host "+h.Host+" repos"); err != nil {
			return err
		}
		if err := subset(h.Roles, q.Roles, "external host "+h.Host+" roles"); err != nil {
			return err
		}
	}
	return nil
}
