//go:build race

package notice

// diffRandomN is the seeded half under the race detector. On this machine the
// full 200_000 took longer than three minutes (about 1.2 ms each); 40_000
// keeps the package under two minutes. The non-race run stays at 200_000.
const diffRandomN = 40_000

// CleanCorpusN under the race detector. 100_000 composes per kind did not
// finish in six minutes here; 4_000 does. The non-race run stays at 100_000.
func CleanCorpusN() int { return 4_000 }
