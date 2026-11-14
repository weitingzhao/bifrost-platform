package store

import ()

// Attempts that failed before their executor started (TD-394; the Owner
// 2026-10-20 21:3x 「TD-394 B」).
//
// agentd ends an attempt whose setup, checkout or sibling clone failed
// prepare_failed, and hands in what failed as its last checkpoint (agentd
// prepare.go). Such an end is the host's or the repository's preparation,
// not the executor's: TD-381 and TD-395 count only exit and hard_limit
// (executorEnds), so it withdraws no executor (PROD M-22: codex, cursor and
// claude were each withdrawn three times for a setup that never let one of
// them start). Three in a row, of the attempts leased since the task last
// went back to ready from a block, block it at the next claim or in
// BlockWithdrawn's pass, with prepareBlockMark; its card says so and quotes
// the last one's report (lastPrepare), and one more round tries it three
// times more.

// prepareFailed is the end of an attempt that failed before its executor
// started.
const prepareFailed = "prepare_failed"
