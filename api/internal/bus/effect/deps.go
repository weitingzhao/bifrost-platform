// Package effect is the Bus page's two computed readings: what the bus's
// work does (GET /api/v1/bus/effect) and the communication layer's change of
// generation (GET /api/v1/bus/nextgen, from its registers, here since
// 2026-11-20). Each reading is taken from its source when asked; one that
// cannot be taken here says so instead of a number. The server hands in what the readings stand
// on (Prometheus, the run cache, the agent runtime's wake records).
package effect

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Wakes is what the bus's wakes did since a time (the agent runtime's
// records of them).
type Wakes struct {
	Delivered, Undeliverable, Acked int
	AckMedianSecs                   float64
	BySeat                          map[string]int
}

// Deps is what the readings stand on. A nil field is a source this process
// does not have: its readings say unmeasured.
type Deps struct {
	// Promql answers an instant query as values by site/route/type/reader label.
	Promql func(ctx context.Context, query string) (map[string]float64, error)
	// CachedRuns lists a pipeline's runs from the bus run cache ("" all);
	// false when the cache is not synced in this process.
	CachedRuns func(pipeline string) ([]unstructured.Unstructured, bool)
	// ProblemEvidence is the evidence for the problem channels (their own
	// proofs read from the ledger), falling back to base for the rest.
	ProblemEvidence func(ctx context.Context, now time.Time, base func(string) Evidence) func(string) Evidence
	// Wakes reads the bus's wakes since a time.
	Wakes func(ctx context.Context, since time.Time) (Wakes, error)
}

// PromqlLabels are the labels the readings key a query's series by, first that
// a series has.
var PromqlLabels = []string{"site", "route", "type", "reader", "__name__"}
