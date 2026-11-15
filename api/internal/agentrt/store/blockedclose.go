package store

// Historical blocked-card vocabulary retained for recorded outcomes.
// The old dispatcher no longer closes missions through these cards.

// The third option of a blocked card, and its key in the question store.
const (
	CardCloseKey  = "c"
	OutcomeClosed = "closed" // the Owner closed the task's mission on its card
)
