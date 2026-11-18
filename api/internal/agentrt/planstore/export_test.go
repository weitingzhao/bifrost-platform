package planstore

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// What planobservations_ledger_test.go reaches of the plan observations. It
// is a test outside the package (planstore_test) because it writes through
// the ledger, whose vocabulary package store declares, and a test inside
// this package cannot import store (store imports this package).
type (
	PlanObservationForTest   = planObservation
	PlanFactsSnapshotForTest = planFactsSnapshot
)

var (
	ObservationKeyForTest        = observationKey
	ObservationStreamForTest     = observationStream
	RecordPlanObservationForTest = recordPlanObservation
	ReadPlanFactsForTest         = readPlanFacts
	JSONObjectForTest            = jsonObject
	TimeTextForTest              = timeText
)

// What the plan tests moved from package store reach of the package's own
// names (TD-582): no other package uses these.
const OpImportForTest = opImport

var (
	ImportOfForTest   = importOf
	StreamRootForTest = streamRoot
)

// ReadingForTest is the snapshot's reading of a binding.
func ReadingForTest(s *planFactsSnapshot, b planview.Binding) (plan.Tri, string, planObservation) {
	return s.reading(b)
}
