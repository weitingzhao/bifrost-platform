package controller_test

import "testing"

// This covers the existing landing-batch part of wave acceptance. The wave
// lifecycle and release attribution are not yet available in this checkout.
// Reuse the batch rig so the three-task path exercises real grant persistence,
// a replayed landing stream, fake Git, and fake CI with the same constraints as
// the two-task batch regression.
func TestWaveThreeTasksOneCIOneGrantOnePush(t *testing.T) {
	testBatchCoresOneCardOnePush(t, 3)
}
