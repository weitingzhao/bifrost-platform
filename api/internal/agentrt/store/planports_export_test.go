package store

// The plan code's ports as the tests reach them (planports_test.go): each
// port as the plan files call it, and the same call through a port value the
// test gives, so the refusal of an unbound port is shown without unbinding
// the package's own.
var (
	PlanRuleInForceForTest      = planRuleInForce
	PlanRuleViaForTest          = planRuleVia
	CardAutoKindsInForceForTest = cardAutoKindsInForce
	CardAutoKindsViaForTest     = cardAutoKindsVia
)

// The lineage ports (external work, recorder import), and the recorder
// import's own functions and the plan's copies of lineage words, for the
// tests that hold each port to its provider.
var (
	CheckExternalNoteForTest           = checkExternalNote
	CheckExternalNoteViaForTest        = checkExternalNoteVia
	CheckExternalDescriptionForTest    = checkExternalDescription
	CheckExternalDescriptionViaForTest = checkExternalDescriptionVia
	ExternalWorkSnapshotForTest        = externalWorkSnapshot
	ExternalWorkSnapshotViaForTest     = externalWorkSnapshotVia
	ExternalActivityBucketsForTest     = externalActivityBuckets
	ExternalActivityBucketsViaForTest  = externalActivityBucketsVia
	RecorderImportLogForTest           = recorderImportLog
	RecorderImportLogViaForTest        = recorderImportLogVia
	DecideRecorderImportLogForTest     = decideRecorderImportLog
	DecideRecorderImportLogViaForTest  = decideRecorderImportLogVia
	RecorderImportOfForTest            = recorderImportOf
	DecideRecorderImportForTest        = decideRecorderImport
)

const (
	CollectorActorPrefixForTest = collectorActorPrefix
	RecorderApplyLabelForTest   = recorderApplyLabel
	RecorderRefuseLabelForTest  = recorderRefuseLabel
	RecorderStreamPrefixForTest = recorderStreamPrefix
)

// The coordination port.
var (
	AlertClaimForTest    = alertClaim
	AlertClaimViaForTest = alertClaimVia
)
