package hostops

import (
	"errors"
	"strings"
	"time"
)

var routineActions = []string{"host_service_restart", "host_service_state", "host_container_restart", "host_compose_reconcile", "host_image_stage", "host_monitoring_install", "host_monitoring_remove", "host_monitoring_drill", "host_agentd_update", "host_toolchain_install", "host_validation_run", "host_temp_cleanup", "host_lifecycle_collect", "host_netplan_apply", "host_new_node_join", "host_new_node_fixed_ip", "host_git_endpoint_set"}
var readActions = []string{"host_status", "host_logs", "host_storage_status", "host_network_probe", "host_integrity_check", "host_inventory_plan", "platform_state_check"}
var highActions = []string{"host_disk_provision", "host_data_delete", "host_network_security_change", "host_node_power", "host_node_retire", "host_access_change", "host_backup_verify", "host_execution_resolve"}

func knownM(a string) bool { return contains(routineActions, a) }

type Request struct {
	Action          string            `json:"action"`
	Params          map[string]string `json:"params"`
	Host            string            `json:"host"`
	Environment     string            `json:"environment"`
	ExecutorID      string            `json:"executor_id"`
	HandlerDigest   string            `json:"handler_digest"`
	BundleDigests   []string          `json:"bundle_digests"`
	WorkID          string            `json:"work_id"`
	RequesterThread string            `json:"requester_thread"`
}

// These facts are observations by each verifier's trusted adapter, never
// booleans accepted from the request. S1 defines the contract, S2/S3 supply I/O.
type Facts struct {
	InventoryDigest     string
	CatalogDigest       string
	Conditions          map[string]bool
	Fresh               bool
	AuditDurable        bool
	NotificationHealthy bool
	RollbackAvailable   bool
	LockAvailable       bool
	ClockSyncedAt       time.Time
	ClockOffset         time.Duration
	ClockRolledBack     bool
	ActiveWrites        int
	HostActiveWrites    int
	RecoveryAttempts    int
	History             []Event
}
type Event struct {
	ExecutionID       string   `json:"execution_id"`
	QuotaIDs          []string `json:"quota_ids"`
	Host              string   `json:"host"`
	Action            string   `json:"action"`
	At                string   `json:"at"`
	Outcome           string   `json:"outcome"`
	SideEffectStarted bool     `json:"side_effect_started"`
}

type Decision struct {
	Allowed  bool
	Reason   string
	QuotaIDs []string
	// Circuit is a durable latch proposal, even though the request is denied.
	// Commit via ObserveSuccesses under the state transaction before retrying.
	Circuit *Circuit
}

func denied(reason string) Decision { return Decision{Reason: reason} }
func domain(r Rule, c Clause, x Request) bool {
	if r.Environment != x.Environment || c.Action != x.Action || !contains(c.Hosts, x.Host) || !contains(r.ExecutorIDs, x.ExecutorID) || !contains(c.ExecutorIDs, x.ExecutorID) || len(c.Params) != len(x.Params) {
		return false
	}
	for k, values := range c.Params {
		v, ok := x.Params[k]
		if !ok || !contains(values, v) {
			return false
		}
	}
	return true
}

// Allow is the complete two-level intersection reference model, not an
// execution permit. R/H use their own verification paths, never this M gate.
func (p *Policy) Allow(x Request, now time.Time, state SafetyState, f Facts) Decision {
	if p == nil || state.Check(x.Environment, p.version, p.head) != nil || !f.Fresh || !f.AuditDurable || !f.NotificationHealthy || !f.RollbackAvailable || !f.LockAvailable {
		return denied("state, freeze, audit, notification, rollback or lock unavailable")
	}
	if f.clock().Check(now) != nil || f.ActiveWrites < 0 || f.HostActiveWrites < 0 || f.RecoveryAttempts < 0 {
		return denied("clock or counters invalid")
	}
	if x.WorkID == "" || x.RequesterThread == "" || !knownM(x.Action) || !validDigest(x.HandlerDigest) || !setOK(x.BundleDigests, true) {
		return denied("invalid request")
	}
	if !current(p.bundle.Meta.EffectiveAt, p.bundle.Meta.ExpiresAt, now) {
		return denied("policy bundle expired or not yet effective")
	}
	next, err := p.ObserveSuccesses(x, now, state, f)
	if err != nil {
		return denied(err.Error())
	}
	for _, circuit := range next.Circuits {
		if circuit.Host == x.Host && circuit.Action == x.Action && circuit.Open {
			return Decision{Reason: "repeated-success circuit latched", Circuit: &circuit}
		}
	}
	covered := false
	quotas := []string{}
	for ri, baseline := range p.base {
		r := p.rules[ri]
		for ci, bc := range baseline.Clauses {
			if !domain(baseline, bc, x) {
				continue
			}
			covered = true
			c := r.Clauses[ci]
			if r.Disabled || r.Tombstone || c.Disabled || c.Tombstone || !current(r.EffectiveAt, r.ExpiresAt, now) || !domain(r, c, x) {
				return denied("applicable constraint rejects (fixed baseline domain)")
			}
			if r.InventoryDigest != f.InventoryDigest || r.CatalogDigest != f.CatalogDigest || c.HandlerDigest != x.HandlerDigest || !subset(x.BundleDigests, c.BundleDigests) {
				return denied("artifact binding mismatch")
			}
			for _, cond := range c.Conditions {
				if !f.Conditions[cond] {
					return denied("condition: " + cond)
				}
			}
			if f.ActiveWrites >= r.Limits.GlobalWrites || f.HostActiveWrites >= r.Limits.PerHostWrites || f.RecoveryAttempts > r.Limits.RecoveryAttempts {
				return denied("concurrency or recovery limit")
			}
			day, week := 0, 0
			for _, event := range f.History {
				at, _ := time.Parse(time.RFC3339, event.At)
				// Stable quota IDs aggregate targets; the host/action fallback
				// also preserves history across signed clause/rule renames.
				if contains(event.QuotaIDs, c.QuotaID) || event.Host == x.Host && event.Action == x.Action {
					if at.UTC().Format("2006-01-02") == now.UTC().Format("2006-01-02") {
						day++
					}
					ay, aw := at.UTC().ISOWeek()
					ny, nw := now.UTC().ISOWeek()
					if ay == ny && aw == nw {
						week++
					}
				}
				if event.Host == x.Host && event.Action == x.Action {
					if now.Sub(at).Seconds() < float64(r.Limits.CooldownS) {
						return denied("cooldown")
					}
				}
			}
			if day >= c.MaxPerDay || week >= c.MaxPerWeek {
				return denied("quota exhausted")
			}
			if !contains(quotas, c.QuotaID) {
				quotas = append(quotas, c.QuotaID)
			}
		}
	}
	if !covered {
		return denied("no baseline coverage")
	}
	return Decision{Allowed: true, QuotaIDs: quotas}
}

var neverM = []string{"k3s", "k3s-agent", "containerd", "docker", "postgresql", "sshd", "bifrost-host-exec", "systemd-networkd"}

func serviceBase(name string) string {
	name = strings.TrimSuffix(name, ".service")
	if i := strings.IndexByte(name, '@'); i >= 0 {
		name = name[:i]
	}
	return name
}
func NeverM(canonical string) bool { return contains(neverM, serviceBase(canonical)) }

// Effect evidence must be independently derived from the actual forward AND
// rollback bytes by a versioned adapter. Unknown/opaque scripts are rejected.
// Frozen identities are injected from the application's canonical catalog;
// this generic platform package does not implement application behavior.
type Effect struct {
	Identity     string            `json:"identity"`
	Operation    string            `json:"operation"`
	SourceDigest string            `json:"source_digest"`
	Parameters   map[string]string `json:"parameters,omitempty"`
	Host         string            `json:"host"`
}
type SafetyCatalog struct {
	Digest           string
	Aliases          map[string]string
	FrozenIdentities []string
	SelfIdentities   []string
}

func (s SafetyCatalog) identity(name string) (string, bool) {
	id, ok := s.Aliases[name]
	if !ok {
		id, ok = s.Aliases[serviceBase(name)]
	}
	return id, ok && id != ""
}

// Boundary must be called independently at platform, executor and helper,
// outside both the M matcher and the H signature check. No Owner signature
// parameter is accepted, so it cannot exempt a forbidden effect.
func (s SafetyCatalog) Boundary(action, tier string, effects []Effect) error {
	if !validDigest(s.Digest) || !contains([]string{"R", "M", "H"}, tier) || len(effects) == 0 || len(s.FrozenIdentities) == 0 || len(s.SelfIdentities) == 0 {
		return errors.New("missing safety catalog/effects")
	}
	if tier == "R" && !contains(readActions, action) || tier == "M" && !knownM(action) || tier == "H" && !knownM(action) && !contains(highActions, action) {
		return errors.New("unsupported action/tier")
	}
	for _, e := range effects {
		id, ok := s.identity(e.Identity)
		if !ok || id == "" || e.Host == "" || !validDigest(e.SourceDigest) || !contains([]string{"read", "observe", "restart", "state", "recreate", "install", "remove", "restore", "join_first_start", "enable_execution", "increase_execution", "remove_guard", "access"}, e.Operation) {
			return errors.New("unproven identity or effect")
		}
		if tier == "R" && e.Operation != "read" {
			return errors.New("read action has write effect")
		}
		if e.Operation != "read" && contains(s.SelfIdentities, id) {
			return errors.New("executor self-maintenance forbidden")
		}
		if contains([]string{"enable_execution", "increase_execution", "remove_guard"}, e.Operation) || contains(s.FrozenIdentities, id) && e.Operation != "read" && e.Operation != "observe" {
			return errors.New("D10: execution effect forbidden")
		}
		if tier == "M" && NeverM(id) && !(action == "host_new_node_join" && e.Operation == "join_first_start") {
			return errors.New("never-M service")
		}
	}
	return nil
}

// InventoryScope binds the authenticated executor and exact target to the
// installed signed inventory. Lifecycle facts/join CAS remain S2/S5 work.
func (i Inventory) InventoryScope(x Request, objectID string, now time.Time) (Object, error) {
	if err := i.validate(); err != nil {
		return Object{}, err
	}
	if !current(i.Meta.EffectiveAt, i.Meta.ExpiresAt, now) || i.Meta.Environment != x.Environment {
		return Object{}, errors.New("inventory invalid/expired")
	}
	hostOK := false
	for _, h := range i.Hosts {
		if h.ID == x.Host && contains(h.ExecutorIDs, x.ExecutorID) && h.Lifecycle != "retired" && h.Lifecycle != "unknown" {
			hostOK = true
		}
	}
	if !hostOK {
		return Object{}, errors.New("host/executor outside inventory")
	}
	for _, o := range i.Objects {
		if o.ID == objectID && o.Host == x.Host && contains(o.Actions, x.Action) {
			return o, nil
		}
	}
	return Object{}, errors.New("object/action outside inventory")
}
