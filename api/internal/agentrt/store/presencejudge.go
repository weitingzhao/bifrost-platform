package store

import (
	"time"
)

// Historical presence stream names and thresholds. The dispatch presence
// controller no longer emits these judgments; retained rows keep their names.

// PresenceStream holds whether host presence can be judged; DeferredStream
// whether its judgment is deferred.
const (
	PresenceStream = "presence:judging"
	DeferredStream = "presence:deferred"
)

// PresenceDeferredAfter is how long a host may be silent, not judged lost
// because the platform's recoveries keep moving its floor, before the
// judgment is said to be deferred.
const PresenceDeferredAfter = 2 * time.Minute

// The alerts' phrases.
const (
	presenceUnjudgedPhrase = "host presence cannot be judged"
	presenceDeferredPhrase = "host presence judgment is deferred"
)
