package controller

import (
	"time"
)

// SlotsEvery is how often the controller looks at the hosts' slots (R5
// phase 2 step 3b, store/hostslots.go); a host's count moves at most once
// every interval_s of the concurrency rule's slots block.
const SlotsEvery = 30 * time.Second
