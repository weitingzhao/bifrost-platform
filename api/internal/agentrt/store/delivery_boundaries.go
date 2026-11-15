package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
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
	Pools      []map[string]any `json:"pools"`
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
	out := DeliveryBoundaries{Rules: []BoundaryRule{}, Tuning: []BoundaryValue{}, Landing: []map[string]any{}, Hosts: json.RawMessage("[]"), Pools: []map[string]any{}, DiskFloor: diskFloor,
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
	out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "release.compare_completeness", "value": releasepolicy.ComparisonFileCap(), "unit": "files (at or above, the complete difference of the two trees is read as well; unreadable refuses automatic approval)", "source": "releasepolicy.ComparisonFileCap", "adjustment": "Completeness safeguard, not the landing file allowance. Requires a code/API evidence change; a larger landing rule does not override it."})
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
	for _, id := range ids {
		if rules.Retired(id) {
			// Signed history of a kind nothing reads any more: named, never
			// parsed, so a row of it cannot fail this view.
			out.Rules = append(out.Rules, BoundaryRule{ID: id, Status: "retired",
				Notes: []string{"This rule kind was retired with the dispatch lane (2026-11-15). Its signed versions stay in the record; nothing reads them."}})
			continue
		}
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
	return out, nil
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
			"max_files": limits.MaxFiles, "max_changed_lines": limits.MaxChangedLines, "configured_changed_lines": configured.MaxChangedLines, "code_clamped": false,
			"max_in_flight": limits.MaxInFlightPerRepo,
			"source":        "rules.Landing.CheckedLimits", "note": "Size limits are not in effect. This is not a task authorization."})
	}
	return rows
}
