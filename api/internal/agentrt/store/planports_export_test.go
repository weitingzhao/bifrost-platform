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
