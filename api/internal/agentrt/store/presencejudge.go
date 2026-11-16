package store

// Historical presence stream names and thresholds. The dispatch presence
// controller no longer emits these judgments; retained rows keep their names.

// The alerts' phrases.
const (
	presenceUnjudgedPhrase = "host presence cannot be judged"
	presenceDeferredPhrase = "host presence judgment is deferred"
)
