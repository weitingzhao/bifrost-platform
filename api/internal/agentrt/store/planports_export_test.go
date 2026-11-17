package store

// The plan code's ports as the tests reach them (planports_test.go): each
// port as the plan files call it, and the same call through a port value the
// test gives, so the refusal of an unbound port is shown without unbinding
// the package's own.
var (
	PlanRuleInForceForTest      = PlanRuleInForce
	CardAutoKindsInForceForTest = CardAutoKindsInForce
)

// The lineage ports (external work, recorder import), and the recorder
// import's own functions and the plan's copies of lineage words, for the
// tests that hold each port to its provider.
var (
	CheckExternalNoteForTest        = CheckExternalNote
	CheckExternalDescriptionForTest = CheckExternalDescription
	ExternalWorkSnapshotForTest     = ExternalWorkSnapshot
	ExternalActivityBucketsForTest  = ExternalActivityBuckets
	RecorderImportLogForTest        = RecorderImportLog
	DecideRecorderImportLogForTest  = DecideRecorderImportLog
	RecorderImportOfForTest         = recorderImportOf
	DecideRecorderImportForTest     = decideRecorderImport
)

const (
	CollectorActorPrefixForTest = CollectorActorPrefix
	RecorderApplyLabelForTest   = RecorderApplyLabel
	RecorderRefuseLabelForTest  = RecorderRefuseLabel
	RecorderStreamPrefixForTest = RecorderStreamPrefix
)

// The coordination port.
var (
	AlertClaimForTest = AlertClaim
)

// The question store's port and the plan's copies of its two words.
var (
	CardBodyBytesForTest = CardBodyBytes
)

const (
	QuestionBodyLimitForTest = QuestionBodyLimit
	RuleAnswerChannelForTest = RuleAnswerChannel
)

// The loop alerts' port.
var (
	ControllerLoopsFailingForTest = ControllerLoopsFailing
)
