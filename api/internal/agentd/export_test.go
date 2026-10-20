package agentd

// Running is running for the external tests: the processes of pids that
// still run (not zombies).
func Running(pids []int) []int { return running(pids) }

// Protected is protected: what the benchmark's judgement never takes from a change.
var Protected = protected

// RewrittenNote is the line a report or an answer whose paths agentd
// rewrote starts with.
const RewrittenNote = rewrittenNote

// ConfineForTest stands f for the wall and checkPins of e's runs: the spawn
// as it runs, and why its pins are not all there ("" when they are).
func (e *Exam) ConfineForTest(f func(a *Attempt, sp Spawn) (Spawn, string)) { e.confine = f }

// GroupAliveForTest stands f for GroupAlive after each of e's runs ends.
func (e *Exam) GroupAliveForTest(f func(pgid int) bool) { e.groupAlive = f }

// ProcIdentity is procIdentity: a process's identity as an exam run's record
// keeps its leader's.
var ProcIdentity = procIdentity
