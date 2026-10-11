//go:build !race

package notice

// diffRandomN is the seeded half of TestRedactHidesEverySecretMainHides.
const diffRandomN = 200_000

// CleanCorpusN is how many seeded clean strings the door tests place in every
// agent-written field. The non-race run is 100_000.
func CleanCorpusN() int { return 100_000 }
