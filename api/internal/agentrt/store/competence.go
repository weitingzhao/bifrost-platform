package store

// Competence at routing (design v5 §5.1.2 「分流顺序」 ⑤; W-31 2.4 slice ②).
// A tenant's routing rule may set a threshold (rules.Competence). Then each
// candidate's sample is what the latest in-job computation (scores.go) says
// of it on tasks like this one: same job, task class and core label, the
// candidate's executor and provider, a model its pattern matches; attempts
// and first passes summed over those rows. With at least min_samples attempts
// a candidate whose score (the Wilson bound of the summed counts) is below
// min_score_percent is excluded; with fewer, one on probation takes no core
// work. The threshold and the samples are route inputs, so a new computation
// that changes a sample, or a new version of the rule, decides the route
// again (Reroute). Without a threshold the input records nothing of it and
// routes as before.

// threshold is a routing rule's competence step.
type threshold struct {
	minPercent, minSamples int
	rule                   ruleRef
}

// scoreLine is one group of a computation's or a run's samples.
type scoreLine struct {
	source, executor, provider, model, job, class string
	core                                          bool
	attempts, firstPass                           int
}
