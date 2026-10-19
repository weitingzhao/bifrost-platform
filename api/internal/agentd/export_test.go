package agentd

// Running is running for the external tests: the processes of pids that
// still run (not zombies).
func Running(pids []int) []int { return running(pids) }

// Protected is protected: what the benchmark's judgement never takes from a change.
var Protected = protected
