package store

import ()

// A gate task's result (gates.go; CUT §3): ConcludeWith hands a submitted
// verify task with no subject here. Only a member of its batch is used. The
// gate passes when its attempt concluded done and its verify.json says
// pass, with the very commands of the gate's spec, in order, each exiting 0
// and none timed out or withheld; anything else is a failure. The result is
// a gate verification of the task's own attempt (verifier NULL: an attempt
// does not verify itself), bound to the batch's environment, commit,
// deployment and spec, which GatePassed reads; a failure also leaves a note
// with the runner's verdict line, which the architecture table quotes. The
// task is then accepted: its gate ran. An attempt handed in with no report
// records nothing (there is no evidence) and its task is cancelled. That the
// report is this attempt's own run rests on agentd, which hands in what the
// run wrote (review td397-4 C2): verify.json names no attempt, commit or
// spec, and the lease's fence binds the hand-over to the attempt, not the
// bytes to a run.

// gateFailPrefix starts the note a failed gate leaves; the architecture
// table reads the rest of it.
const gateFailPrefix = "gate failed: "
