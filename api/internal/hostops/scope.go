package hostops

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// ActionScope is signed inside its primary inventory object. Dependencies are
// deliberately not consulted here: affected/locked objects confer no authority.
// A variant describes one complete effect set, separately for each phase.
type ActionScope struct {
	ID       string                    `json:"id"`
	Action   string                    `json:"action"`
	Params   map[string]ScopeParameter `json:"params"`
	Forward  []ScopedEffect            `json:"forward"`
	Rollback []ScopedEffect            `json:"rollback"`
}

// Kind is enum, object, log_since or log_lines. Object values resolve ONLY via
// the signed canonical name/aliases (or the exact object ID for *_id params).
// No globs, regexes, free paths or implicit dependency expansion are supported.
type ScopeParameter struct {
	Kind     string   `json:"kind"`
	ObjectID string   `json:"object_id"`
	Values   []string `json:"values"`
}

type ScopedEffect struct {
	ObjectID     string            `json:"object_id"`
	Operation    string            `json:"operation"`
	SourceDigest string            `json:"source_digest"`
	Parameters   map[string]string `json:"parameters"`
}

var firstMaintenanceActions = []string{"host_service_restart", "host_service_state", "host_container_restart", "host_compose_reconcile", "host_monitoring_install", "host_monitoring_remove", "host_agentd_update"}

func (i Inventory) object(id string) (Object, bool) {
	for _, o := range i.Objects {
		if o.ID == id {
			return o, true
		}
	}
	return Object{}, false
}

// Operation families are closed per template, not a universal write whitelist.
func scopeOperations(action string, rollback bool) []string {
	if contains(readActions, action) {
		return []string{"read"}
	}
	switch action {
	case "host_service_restart", "host_container_restart":
		if rollback {
			return []string{"restart", "state"}
		}
		return []string{"restart"}
	case "host_service_state":
		return []string{"state"}
	case "host_compose_reconcile":
		return []string{"recreate", "restart", "state"}
	case "host_monitoring_install":
		if rollback {
			return []string{"remove", "restore", "state", "restart", "observe"}
		}
		return []string{"install", "state", "restart", "observe"}
	case "host_monitoring_remove":
		if rollback {
			return []string{"install", "restore", "state", "restart"}
		}
		return []string{"remove", "state"}
	case "host_agentd_update":
		return []string{"install", "restore", "state", "restart"}
	}
	return nil
}

func objectParameterKind(name string) string {
	switch name {
	case "service":
		return "service"
	case "project":
		return "project"
	case "root_id":
		return "root"
	case "endpoint_id":
		return "endpoint"
	case "install_id":
		return "install"
	}
	return ""
}

func (i Inventory) validateActionScopes(primary Object) error {
	ids := map[string]bool{}
	for _, s := range primary.ActionScopes {
		if s.ID == "" || ids[s.ID] || !contains(primary.Actions, s.Action) ||
			(!contains(firstMaintenanceActions, s.Action) && !contains(readActions, s.Action)) || !validParams(s.Action, s.Params) ||
			len(s.Forward) == 0 || contains(readActions, s.Action) && len(s.Rollback) != 0 || contains(firstMaintenanceActions, s.Action) && len(s.Rollback) == 0 {
			return errors.New("unsupported or incomplete signed action scope")
		}
		ids[s.ID] = true
		for name, p := range s.Params {
			kind := objectParameterKind(name)
			if kind != "" {
				o, ok := i.object(p.ObjectID)
				if p.Kind != "object" || len(p.Values) != 0 || !ok || o.Host != primary.Host || o.Kind != kind {
					return errors.New("invalid object parameter binding")
				}
				if name == "service" && o.ID != primary.ID || name == "project" && s.Action == "host_compose_reconcile" && o.ID != primary.ID ||
					contains([]string{"root_id", "endpoint_id", "install_id"}, name) && o.ID != primary.ID {
					return errors.New("request target differs from primary object")
				}
				continue
			}
			if p.ObjectID != "" {
				return errors.New("unexpected object binding")
			}
			if s.Action == "host_logs" && (name == "since" || name == "lines") {
				if p.Kind != "log_"+name || len(p.Values) != 0 {
					return errors.New("invalid log parameter constraint")
				}
				continue
			}
			if p.Kind != "enum" || !setOK(p.Values, false) {
				return errors.New("finite parameter constraint required")
			}
			for _, v := range p.Values {
				if !nonemptyParams(map[string]string{name: v}) {
					return errors.New("invalid parameter value")
				}
			}
			// These select an effect graph; one variant must bind one concrete graph.
			if contains([]string{"profile", "plan", "state", "bundle", "environment"}, name) && len(p.Values) != 1 {
				return errors.New("effect selector must be singular")
			}
			if name == "state" && !contains([]string{"start", "stop", "disable"}, p.Values[0]) {
				return errors.New("unknown service state")
			}
			if name == "bundle" && !validDigest(p.Values[0]) {
				return errors.New("bundle selector must be a manifest digest")
			}
			if name == "environment" && p.Values[0] != i.Meta.Environment {
				return errors.New("cross environment scope")
			}
		}
		for phase, effects := range [][]ScopedEffect{s.Forward, s.Rollback} {
			seen := map[string]bool{}
			for _, e := range effects {
				o, ok := i.object(e.ObjectID)
				if !ok || o.Host != primary.Host || !contains(scopeOperations(s.Action, phase == 1), e.Operation) || !validDigest(e.SourceDigest) || !nonemptyParams(e.Parameters) {
					return errors.New("unknown target, operation or source in signed scope")
				}
				if contains([]string{"host_service_restart", "host_service_state", "host_container_restart", "host_logs"}, s.Action) && o.ID != primary.ID {
					return errors.New("single service action has another effect target")
				}
				if s.Action == "host_compose_reconcile" && (o.Stateful || primary.Stateful) {
					return errors.New("stateful compose effect requires H")
				}
				if contains(readActions, s.Action) && e.SourceDigest != o.ConfigDigest {
					return errors.New("read source must bind registered config")
				}
				for name := range e.Parameters {
					if !contains([]string{"state", "project", "profile", "plan"}, name) {
						return errors.New("unknown effect parameter")
					}
				}
				if e.Operation == "state" {
					if !contains([]string{"start", "stop", "disable"}, e.Parameters["state"]) {
						return errors.New("state effect needs exact state")
					}
					if s.Action == "host_service_state" && phase == 0 && e.Parameters["state"] != s.Params["state"].Values[0] {
						return errors.New("forward state differs from requested state")
					}
				} else if _, ok := e.Parameters["state"]; ok {
					return errors.New("state parameter on non-state effect")
				}
				// Every effect carries independently observed project/profile/plan context.
				// Object context uses IDs; profile/plan are exact signed selectors.
				for _, name := range []string{"project", "profile", "plan"} {
					p, present := s.Params[name]
					value, supplied := e.Parameters[name]
					if present {
						expected := p.ObjectID
						if p.Kind == "enum" {
							expected = p.Values[0]
						}
						if !supplied || value != expected {
							return errors.New("unbound effect context")
						}
					} else if supplied {
						return errors.New("unexpected effect context")
					}
				}
				key := string(canonical(e))
				if seen[key] {
					return errors.New("duplicate signed effect")
				}
				seen[key] = true
			}
		}
	}
	return nil
}

func (i Inventory) scopeParamsMatch(s ActionScope, x Request, now time.Time) bool {
	if !validParams(x.Action, x.Params) || !nonemptyParams(x.Params) {
		return false
	}
	if x.Action == "host_logs" && !validLogParams(x.Params, now) {
		return false
	}
	for name, p := range s.Params {
		v := x.Params[name]
		switch p.Kind {
		case "enum":
			if !contains(p.Values, v) {
				return false
			}
		case "object":
			o, ok := i.object(p.ObjectID)
			if !ok {
				return false
			}
			if contains([]string{"root_id", "endpoint_id", "install_id"}, name) {
				if v != o.ID {
					return false
				}
			} else if v != o.Canonical && !contains(o.Aliases, v) {
				return false
			}
		case "log_since", "log_lines": // checked together above
		default:
			return false
		}
	}
	return true
}

// Resolve exact registered aliases before consulting the safety catalog. The
// catalog's systemd normalization remains a hard-denial tool, never a grant.
func (i Inventory) effectObject(host, identity string, catalog SafetyCatalog) (Object, error) {
	actual, known := catalog.identity(identity)
	if !known {
		return Object{}, errors.New("unknown effect identity")
	}
	var result Object
	matches := 0
	for _, o := range i.Objects {
		if o.Host != host {
			continue
		}
		canonicalID, ok := catalog.identity(o.Canonical)
		if ok && canonicalID == actual {
			matches++
		}
		if identity == o.Canonical || contains(o.Aliases, identity) {
			result = o
		}
	}
	expected, ok := catalog.identity(result.Canonical)
	if result.ID == "" || !ok || expected != actual || matches != 1 {
		return Object{}, errors.New("effect is unregistered or ambiguously mapped")
	}
	for _, alias := range result.Aliases {
		id, known := catalog.identity(alias)
		if !known || id != expected {
			return Object{}, errors.New("registered alias disagrees with safety catalog")
		}
	}
	return result, nil
}

func equalEffectParameters(a, b map[string]string) bool {
	// An absent observation denotes only the empty parameter set.
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}

func (i Inventory) matchEffects(host string, catalog SafetyCatalog, expected []ScopedEffect, actual []Effect) error {
	if len(expected) != len(actual) {
		return errors.New("incomplete or extra effects")
	}
	used := make([]bool, len(expected))
	for _, e := range actual {
		if e.Host != host {
			return errors.New("effect host differs from signed target")
		}
		o, err := i.effectObject(host, e.Identity, catalog)
		if err != nil {
			return err
		}
		found := false
		for n, grant := range expected {
			if !used[n] && grant.ObjectID == o.ID && grant.Operation == e.Operation && grant.SourceDigest == e.SourceDigest && equalEffectParameters(grant.Parameters, e.Parameters) {
				used[n], found = true, true
				break
			}
		}
		if !found {
			return errors.New("effect outside signed phase/target/operation/parameters")
		}
	}
	return nil
}

func (i Inventory) matchActionScope(o Object, x Request, catalog SafetyCatalog, forward, rollback []Effect, now time.Time) error {
	var matched *ActionScope
	for n := range o.ActionScopes {
		s := &o.ActionScopes[n]
		if s.Action == x.Action && i.scopeParamsMatch(*s, x, now) {
			if matched != nil {
				return errors.New("ambiguous signed action scope")
			}
			matched = s
		}
	}
	if matched == nil {
		return errors.New("unsupported: no signed action scope matches")
	}
	// Check request aliases against the same trusted identity mapping as effects.
	for _, p := range matched.Params {
		if p.Kind == "object" {
			target, _ := i.object(p.ObjectID)
			if _, err := i.effectObject(x.Host, target.Canonical, catalog); err != nil {
				return err
			}
		}
	}
	for name, p := range matched.Params {
		if p.Kind == "object" && (name == "service" || name == "project") {
			resolved, err := i.effectObject(x.Host, x.Params[name], catalog)
			if err != nil || resolved.ID != p.ObjectID {
				return errors.New("request alias disagrees with signed target")
			}
		}
	}
	for phase, pair := range []struct {
		want []ScopedEffect
		got  []Effect
	}{{matched.Forward, forward}, {matched.Rollback, rollback}} {
		if err := i.matchEffects(x.Host, catalog, pair.want, pair.got); err != nil {
			return fmt.Errorf("phase %d: %w", phase, err)
		}
	}
	return nil
}
