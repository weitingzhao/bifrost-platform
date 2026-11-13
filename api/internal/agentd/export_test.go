package agentd

// Running is running for the external tests: the processes of pids that
// still run (not zombies).
func Running(pids []int) []int { return running(pids) }

// Protected is protected: what the benchmark's judgement never takes from a change.
var Protected = protected

// RewrittenNote is the line a report or an answer whose paths agentd
// rewrote starts with.
const RewrittenNote = rewrittenNote

// ProcIdentity is procIdentity: a process's identity as an exam run's record
// keeps its leader's.
var ProcIdentity = procIdentity

// PlanReply is where a plan's follow-up writes its reply (planfix.go).
const PlanReply = planReply

// BuildOf is buildOf: the build that build information and a binary make.
var BuildOf = buildOf
