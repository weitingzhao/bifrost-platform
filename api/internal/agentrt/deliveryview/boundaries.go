package deliveryview

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// defaultDiskFloor is the floor when the platform's configuration names none:
// 15 GiB, below which a test database, a Go build or a clone can fail.
// Historical dispatch disk value retained for event and boundary readers;
// claims no longer allocate work or page the Owner on disk changes.
const defaultDiskFloor int64 = 15 << 30

// TuningKey is one tuning value the snapshot lists.
type TuningKey struct {
	Name                  string
	Default, Min, Ceiling int
	Unit, ReadBy          string
}

// BoundarySource is what the snapshot reads from outside this package; the
// caller (agentrtapi) injects it from the rule store and the landing code, so
// Delivery's projection names neither.
type BoundarySource struct {
	// RuleIDs are the rule kinds on record, in order.
	RuleIDs func(ctx context.Context, tx pgx.Tx) ([]string, error)
	// Retired says a kind nothing reads any more.
	Retired func(id string) bool
	// Rule is one kind as of now: its projection and, when its version in
	// force sets tuning values, those values.
	Rule func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (BoundaryRule, map[string]int, error)
	// Tuning lists the keys of the tuning rule.
	Tuning []TuningKey
	// WaitBoundaries are the landing waits compiled into the code.
	WaitBoundaries map[string]time.Duration
	// ComparisonFileCap is the release gate's completeness cap.
	ComparisonFileCap int
}

// ReadBoundaries is a read-only projection, not a release preflight.
// Repeatable read keeps versions and their derived values in one DB snapshot.
func ReadBoundaries(ctx context.Context, db eventlog.DB, src BoundarySource, diskFloor int64) (DeliveryBoundaries, error) {
	out := DeliveryBoundaries{Rules: []BoundaryRule{}, Tuning: []BoundaryValue{}, Landing: []map[string]any{}, Hosts: json.RawMessage("[]"), Pools: []map[string]any{}, DiskFloor: diskFloor,
		Unknown: []string{
			"This is a policy snapshot, not authorization for a particular task or release. Task-specific grants, checks and release windows remain authoritative.",
			"Current disk free space and the agentd process settings are not persisted in host telemetry; disk alerts do not establish a current measurement.",
			"Registered CI settings are read separately from Git and installed Tekton objects below. Existing run specifications, release-policy decisions and unregistered settings must not be inferred from these runtime rules.",
		}}
	if out.DiskFloor <= 0 {
		out.DiskFloor = defaultDiskFloor
	}
	for id, value := range src.WaitBoundaries {
		out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "landing." + id, "value": value.Seconds(), "unit": "seconds", "source": "landing.WaitBoundaries (API binary)", "adjustment": "Code change required; no runtime rule consumes this value yet."})
	}
	out.CodeLimits = append(out.CodeLimits, map[string]any{"boundary_id": "release.compare_completeness", "value": src.ComparisonFileCap, "unit": "files (at or above, the complete difference of the two trees is read as well; unreadable refuses automatic approval)", "source": "releasepolicy.ComparisonFileCap", "adjustment": "Completeness safeguard, not the landing file allowance. Requires a code/API evidence change; a larger landing rule does not override it."})
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
	ids, err := src.RuleIDs(ctx, tx)
	if err != nil {
		return out, err
	}
	var tuning map[string]int
	for _, id := range ids {
		if src.Retired(id) {
			// Signed history of a kind nothing reads any more: named, never
			// parsed, so a row of it cannot fail this view.
			out.Rules = append(out.Rules, BoundaryRule{ID: id, Status: "retired",
				Notes: []string{"This rule kind was retired with the dispatch lane (2026-11-15). Its signed versions stay in the record; nothing reads them."}})
			continue
		}
		rule, values, e := src.Rule(ctx, tx, id, out.AsOf)
		if e != nil {
			return out, e
		}
		out.Rules = append(out.Rules, rule)
		if values != nil {
			tuning = values
		}
	}
	for _, k := range src.Tuning {
		source, value := "code_default", k.Default
		if v, ok := tuning[k.Name]; ok {
			source, value = "tuning_rule", v
		}
		out.Tuning = append(out.Tuning, BoundaryValue{Key: k.Name, Value: value, Default: k.Default, Min: k.Min, Max: k.Ceiling, Unit: k.Unit, Source: source, Consumer: k.ReadBy})
	}
	return out, nil
}
