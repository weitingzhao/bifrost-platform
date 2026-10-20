package agentd

// Running is running for the external tests: the processes of pids that
// still run (not zombies).
func Running(pids []int) []int { return running(pids) }

// Protected is protected: what the benchmark's judgement never takes from a change.
var Protected = protected

// ConfineForTest stands f for the wall and checkPins of e's runs: the spawn
// as it runs, and why its pins are not all there ("" when they are).
func (e *Exam) ConfineForTest(f func(a *Attempt, sp Spawn) (Spawn, string)) { e.confine = f }
