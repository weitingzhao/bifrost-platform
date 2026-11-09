package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/slots"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// BoundaryRule keeps the effective version separate from the latest version:
// a scheduled or expired rule must never be presented as current authority.
type BoundaryRule struct {
	ID      string         `json:"rule_id"`
	Current map[string]any `json:"in_force"`
	Latest  map[string]any `json:"latest"`
	Draft   map[string]any `json:"draft"`
	Status  string         `json:"status"`
	Notes   []string       `json:"notes"`
}

type BoundaryValue struct {
	Key      string `json:"key"`
	Value    int    `json:"value"`
	Default  int    `json:"default"`
	Min      int    `json:"min"`
	Max      int    `json:"max"`
	Unit     string `json:"unit"`
	Source   string `json:"source"`
	Consumer string `json:"consumer"`
}

type DeliveryBoundaries struct {
	AsOf       time.Time        `json:"as_of"`
	Rules      []BoundaryRule   `json:"rules"`
	Tuning     []BoundaryValue  `json:"tuning"`
	Landing    []map[string]any `json:"landing"`
	Hosts      json.RawMessage  `json:"hosts"`
	Pools      []PoolView       `json:"pools"`
	DiskFloor  int64            `json:"disk_floor_bytes"`
	Unknown    []string         `json:"unknown"`
	CodeLimits []map[string]any `json:"code_limits"`
}

func boundaryRule(id string, current, latest *StoredRule, now time.Time) BoundaryRule {
	v := BoundaryRule{ID: id, Status: "not_in_force", Notes: []string{}}
	if current != nil {
		v.Current = current.Doc()
		v.Status = "in_force"
	}
	if latest != nil {
		v.Latest = latest.Doc()
		v.Draft = latest.Doc()
		v.Draft["rule_version"] = latest.Version + 1
		v.Draft["direction"] = rules.Loosen
		if latest.Effective.After(now) {
			v.Notes = append(v.Notes, "The latest version is scheduled; the effective version is shown separately.")
		}
		if !latest.Expires.After(now) {
			v.Notes = append(v.Notes, "The latest version has expired. Review its expiry before proposing a replacement.")
		}
		if latest.Kind == rules.KindBudget {
			v.Notes = append(v.Notes, "Mission max_attempts, max_wall_hours and auto_grant are retained for compatibility; they no longer cap mission execution. attempt_hard_limit_s is still consumed.")
		}
	}
	if id == rules.KindTuning && latest == nil {
		v.Status = "code_defaults"
		values := map[string]int{}
		for _, key := range rules.TuningKeys() {
			values[key.Name] = key.Default
		}
		v.Draft = map[string]any{"rule_id": id, "rule_version": 1, "direction": rules.Initial, "effective_at": now.UTC().Format(time.RFC3339), "expires_at": "", "values": values}
		v.Notes = append(v.Notes, "No tuning rule is installed. Defaults below are read from the runtime registry. The first version requires a signature and an explicit expiry.")
	}
	return v
}

// ReadDeliveryBoundaries is a read-only projection, not a release preflight.
// It calls the same CurrentRule parser and effective-value helpers as consumers.
// Repeatable read keeps versions and their derived values in one DB snapshot.
func ReadDeliveryBoundaries(ctx context.Context, db DB, diskFloor int64, availableSince time.Time) (DeliveryBoundaries, error) {
	out := DeliveryBoundaries{Rules: []BoundaryRule{}, Tuning: []BoundaryValue{}, Landing: []map[string]any{}, Pools: []PoolView{}, DiskFloor: diskFloor,
		Unknown: []string{
			"This is a policy snapshot, not authorization for a particular task or release. Task-specific grants, checks and release windows remain authoritative.",
			"Current disk free space and the agentd process settings are not persisted in host telemetry; disk alerts do not establish a current measurement.",
			"Registered CI settings are read separately from Git and installed Tekton objects below. Existing run specifications, release-policy decisions and unregistered settings must not be inferred from these runtime rules.",
		}}
	if out.DiskFloor <= 0 {
		out.DiskFloor = DefaultDiskFloor
	}
	for id, value := range landing.WaitBoundaries() {
		out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "landing." + id, "value": value.Seconds(), "unit": "seconds", "source": "landing.WaitBoundaries (API binary)", "adjustment": "Code change required; no runtime rule consumes this value yet."})
	}
	out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "landing.batch_members", "value": landing.BatchMax, "unit": "members", "source": "landing.BatchMax (API binary)", "adjustment": "The rule can impose a smaller max_batch; raising the code ceiling requires a code change."})
	out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "release.compare_completeness", "value": releasepolicy.ComparisonFileCap(), "unit": "files (at or above refuses automatic approval)", "source": "releasepolicy.ComparisonFileCap", "adjustment": "Completeness safeguard, not the landing file allowance. Requires a code/API evidence change; a larger landing rule does not override it."})
	for _, limit := range []struct {
		id           string
		value        float64
		unit, source string
	}{
		{"proposal.tasks", MaxBatch, "tasks", "store.MaxBatch"},
		{"proposal.attempt_hard_limit", MaxHardLimit.Seconds(), "seconds", "store.MaxHardLimit"},
		{"proposal.default_mission_days", DefaultMissionDays, "days", "store.DefaultMissionDays (explicit proposal deadline overrides this default)"},
		{"host.signal_freshness", SignalsFresh.Seconds(), "seconds", "store.SignalsFresh"},
		{"host.slot_ceiling", rules.PerHostCeiling, "slots", "rules.PerHostCeiling"},
	} {
		out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": limit.id, "value": limit.value, "unit": limit.unit, "source": limit.source, "adjustment": "Code-defined bound or default; changing a different rule cannot bypass this consumer."})
	}
	sort.Slice(out.CodeLimits, func(i, j int) bool {
		return out.CodeLimits[i]["boundary_id"].(string) < out.CodeLimits[j]["boundary_id"].(string)
	})
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&out.AsOf); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, "SELECT rule_id FROM agentrt.rule UNION SELECT 'tuning' ORDER BY rule_id")
	if err != nil {
		return out, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	var tuning *rules.Tuning
	var concurrency *StoredRule
	for _, id := range ids {
		current, e := CurrentRule(ctx, tx, id, out.AsOf)
		if e != nil {
			return out, e
		}
		latest, e := latestRule(ctx, tx, id)
		if e != nil {
			return out, e
		}
		out.Rules = append(out.Rules, boundaryRule(id, current, latest, out.AsOf))
		if current == nil {
			continue
		}
		if current.Tuning != nil {
			tuning = current.Tuning
		}
		if current.Kind == rules.KindConcurrency {
			concurrency = current
		}
		if current.Landing != nil {
			out.Landing = append(out.Landing, landingBoundaryRows(current.Rule)...)
		}
	}
	for _, k := range rules.TuningKeys() {
		source := "code_default"
		if tuning != nil {
			if _, ok := tuning.Values[k.Name]; ok {
				source = "tuning_rule"
			}
		}
		out.Tuning = append(out.Tuning, BoundaryValue{k.Name, tuning.Int(k.Name), k.Default, k.Min, k.Ceiling, k.Unit, source, k.ReadBy})
	}
	if err = tx.QueryRow(ctx, HostsSQL("transaction_timestamp()"), HostsArgs(availableSince)...).Scan(&out.Hosts); err != nil {
		return out, err
	}
	if out.Hosts, err = boundaryHosts(out.Hosts, concurrency, out.AsOf); err != nil {
		return out, err
	}
	out.Pools, err = listPools(ctx, tx)
	return out, err
}

func landingBoundaryRows(rule rules.Rule) []map[string]any {
	rows := []map[string]any{}
	if rule.Landing == nil {
		return rows
	}
	for _, repo := range rule.Landing.Repos {
		configured := rule.Landing.EffectiveLimits(repo.Repo)
		limits := rule.Landing.CheckedLimits(repo.Repo)
		rows = append(rows, map[string]any{"rule_id": rule.ID, "rule_version": rule.Version, "repo": repo.Repo,
			"max_files": limits.MaxFiles, "max_changed_lines": limits.MaxChangedLines, "configured_changed_lines": configured.MaxChangedLines, "code_clamped": limits.MaxChangedLines != configured.MaxChangedLines,
			"max_in_flight": limits.MaxInFlightPerRepo, "max_landings_per_day": limits.MaxLandingsPerDay, "max_batch": limits.MaxBatch,
			"source": "rules.Landing.CheckedLimits", "note": "Size is a soft refusal; an existing scoped grant may authorize it. This is not a task authorization."})
	}
	return rows
}

func boundaryHosts(raw json.RawMessage, concurrency *StoredRule, now time.Time) (json.RawMessage, error) {
	var hosts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, err
	}
	for _, host := range hosts {
		var capacity struct {
			Slots     *int `json:"slots"`
			Effective *int `json:"effective_slots"`
		}
		if err := json.Unmarshal(host["capacity_json"], &capacity); err != nil {
			return nil, err
		}
		stated := rules.PerHostCeiling
		if capacity.Slots != nil {
			stated = *capacity.Slots
		}
		granted := 0
		if concurrency != nil {
			granted = grantOf(slots.ConfigOf(concurrency.Rule), stated, capacity.Effective)
		}
		host["projected_slot_limit"], _ = json.Marshal(granted)
		var signal struct {
			At time.Time `json:"at"`
		}
		state := "unknown"
		if json.Unmarshal(host["signals_json"], &signal) == nil && !signal.At.IsZero() {
			state = "stale"
			if !signal.At.Before(now.Add(-SignalsFresh)) {
				state = "fresh"
			}
		}
		host["signal_state"], _ = json.Marshal(state)
	}
	return json.Marshal(hosts)
}
