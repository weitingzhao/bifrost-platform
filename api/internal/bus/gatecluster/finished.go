package gatecluster

import (
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// FinishedRuns is the queue of runs gate-cluster said finished, for the
// pipeline records (bus step 2). The zero value is ready.
type FinishedRuns struct {
	once sync.Once
	runs chan string
}

// Queue is the queue itself.
func (q *FinishedRuns) Queue() chan string {
	q.once.Do(func() { q.runs = make(chan string, 256) })
	return q.runs
}

// Add is gate-cluster's OnFinished: it queues the run and returns at once; a
// run that finds the queue full is left to the reconciliation, which counts
// it as missed.
func (q *FinishedRuns) Add(run, _ string) {
	select {
	case q.Queue() <- run:
		readerseen.Saw("records")
	default:
	}
}
