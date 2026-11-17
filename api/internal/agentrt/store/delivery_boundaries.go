package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// Compatibility contracts; the pure policy snapshot presentation belongs to Delivery.
type BoundaryRule = deliveryview.BoundaryRule
type BoundaryValue = deliveryview.BoundaryValue
type DeliveryBoundaries = deliveryview.DeliveryBoundaries

func boundaryRule(id string, current, latest *StoredRule, now time.Time) BoundaryRule {
	var c, l *deliveryview.BoundaryVersion
	if current != nil {
		c = &deliveryview.BoundaryVersion{Document: current.Doc(), Version: current.Version, Effective: current.Effective, Expires: current.Expires}
	}
	if latest != nil {
		l = &deliveryview.BoundaryVersion{Document: latest.Doc(), DraftDocument: latest.Doc(), Version: latest.Version, Effective: latest.Effective, Expires: latest.Expires}
	}
	defaults := map[string]int{}
	for _, key := range rules.TuningKeys() {
		defaults[key.Name] = key.Default
	}
	return deliveryview.ProjectBoundaryRule(id, c, l, now, rules.KindTuning, rules.Loosen, rules.Initial, defaults)
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
		out.Tuning = append(out.Tuning, BoundaryValue{Key: k.Name, Value: tuning.Int(k.Name), Default: k.Default, Min: k.Min, Max: k.Ceiling, Unit: k.Unit, Source: source, Consumer: k.ReadBy})
	}
	return out, nil
}
