package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// The plan code's ports as the tests reach them (planports_test.go): each
// port as the plan files call it, and the same call through a port value the
// test gives, so the refusal of an unbound port is shown without unbinding
// the package's own.
var (
	PlanRuleInForceForTest      = planstore.PlanRuleInForce
	CardAutoKindsInForceForTest = planstore.CardAutoKindsInForce
)

// The lineage ports (external work, recorder import), and the recorder
// import's own functions and the plan's copies of lineage words, for the
// tests that hold each port to its provider.
var (
	CheckExternalNoteForTest        = planstore.CheckExternalNote
	CheckExternalDescriptionForTest = planstore.CheckExternalDescription
	ExternalWorkSnapshotForTest     = planstore.ExternalWorkSnapshot
	ExternalActivityBucketsForTest  = planstore.ExternalActivityBuckets
	RecorderImportLogForTest        = planstore.RecorderImportLog
	DecideRecorderImportLogForTest  = planstore.DecideRecorderImportLog
	RecorderImportOfForTest         = recorderImportOf
	DecideRecorderImportForTest     = externalwork.DecideRecorderImport
)

const (
	CollectorActorPrefixForTest = planstore.CollectorActorPrefix
	RecorderApplyLabelForTest   = planstore.RecorderApplyLabel
	RecorderRefuseLabelForTest  = planstore.RecorderRefuseLabel
	RecorderStreamPrefixForTest = planstore.RecorderStreamPrefix
)

// The coordination port.
var (
	AlertClaimForTest = planstore.AlertClaim
)

// The question store's port and the plan's copies of its two words.
var (
	CardBodyBytesForTest = planstore.CardBodyBytes
)

const (
	QuestionBodyLimitForTest = planstore.QuestionBodyLimit
	RuleAnswerChannelForTest = planstore.RuleAnswerChannel
)

// The loop alerts' port.
var (
	ControllerLoopsFailingForTest = planstore.ControllerLoopsFailing
)
