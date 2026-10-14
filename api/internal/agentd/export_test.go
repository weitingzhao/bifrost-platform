package agentd

// Running is running for the external tests: the processes of pids that
// still run (not zombies).
func Running(pids []int) []int { return running(pids) }
