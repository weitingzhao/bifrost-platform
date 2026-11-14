package store

import (
	"errors"
)

// ErrNoMergeUncertain requires Owner reconciliation, never an automatic retry
// or a claim that the approved batch failed without changing anything.
var ErrNoMergeUncertain = errors.New("store: no_merge outcome unknown; Owner must check the missions and approval events")
