package controller

import (
	"time"
)

// AssessEvery is how often the assessments pass runs (TD-391): a loop of its
// own, since what it reads outside the runtime (main's commits, the ledger)
// can take tens of seconds.
const AssessEvery = 30 * time.Second

// EvidenceTimeout bounds one assessment's read of the repositories.
const EvidenceTimeout = 45 * time.Second
