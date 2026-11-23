package ports

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// For delivery's boundaries view (deliveryview.BoundarySource, bound in
// server/agentrtapi_boundarysource.go): the rule kinds on record, each one's
// version in force and newest version, the tuning rule's keys and the landing
// waits compiled into the code.

// RuleVersion is one stored version of a rule as the boundaries view shows it.
type RuleVersion struct {
	Version            int
	Effective, Expires time.Time
	// Doc is the version as a document; each call is a new map.
	Doc func() map[string]any
}

// BoundaryRule is one rule kind as of a moment: its version in force and its
// newest version (nil when there is none), and the tuning values the version
// in force sets (nil when it sets none).
type BoundaryRule struct {
	Current, Latest *RuleVersion
	Tuning          map[string]int
}

// TuningKey is one key of the tuning rule.
type TuningKey struct {
	Name                  string
	Default, Min, Ceiling int
	Unit, ReadBy          string
}

// Boundaries is what the boundaries view reads of the rules.
type Boundaries struct {
	// RuleIDs are the rule kinds on record, in order.
	RuleIDs func(ctx context.Context, tx pgx.Tx) ([]string, error)
	// Retired says a kind nothing reads any more.
	Retired func(id string) bool
	// Rule reads one kind as of now, with the same CurrentRule parser as the
	// rules' consumers.
	Rule func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (BoundaryRule, error)
	// Tuning lists the tuning rule's keys; TuningKind is its kind.
	Tuning     []TuningKey
	TuningKind string
	// Initial and Loosen are the directions of a first version and of one
	// that loosens.
	Initial, Loosen string
	// WaitBoundaries are the landing waits compiled into the code.
	WaitBoundaries map[string]time.Duration
}

// DeliveryBoundaries binds the boundaries view's reads to the rule store, the
// rule registry and the landing waits. Moved out of server
// (agentrtapi_boundarysource.go) on 2026-11-23, unchanged.
func DeliveryBoundaries() Boundaries {
	var keys []TuningKey
	for _, k := range rules.TuningKeys() {
		keys = append(keys, TuningKey{Name: k.Name, Default: k.Default, Min: k.Min, Ceiling: k.Ceiling, Unit: k.Unit, ReadBy: k.ReadBy})
	}
	return Boundaries{
		RuleIDs: store.RuleIDs,
		Retired: rules.Retired,
		Rule: func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (BoundaryRule, error) {
			current, err := store.CurrentRule(ctx, tx, id, now)
			if err != nil {
				return BoundaryRule{}, err
			}
			latest, err := store.LatestRule(ctx, tx, id)
			if err != nil {
				return BoundaryRule{}, err
			}
			var values map[string]int
			if current != nil && current.Tuning != nil {
				values = current.Tuning.Values
				if values == nil {
					values = map[string]int{}
				}
			}
			return BoundaryRule{Current: versionOf(current), Latest: versionOf(latest), Tuning: values}, nil
		},
		Tuning:         keys,
		TuningKind:     rules.KindTuning,
		Initial:        rules.Initial,
		Loosen:         rules.Loosen,
		WaitBoundaries: landing.WaitBoundaries(),
	}
}

func versionOf(r *store.StoredRule) *RuleVersion {
	if r == nil {
		return nil
	}
	return &RuleVersion{Version: r.Version, Effective: r.Effective, Expires: r.Expires, Doc: r.Doc}
}
