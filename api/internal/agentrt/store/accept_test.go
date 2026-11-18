package store_test

// tool is one executor a host can run, with the model it would use: what the
// fixtures' host job rows are written from.
type tool struct {
	Executor string // actor id, e.g. exec:codex
	Provider string // model provider
	Model    string // may be empty until the executor starts
}

var deterministic = tool{Executor: "exec:deterministic", Provider: "local"}
