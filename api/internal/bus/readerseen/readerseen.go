// Package readerseen records, in the process doing it, that a reader the
// source switch calls moved (bus/nextgen readers.yaml) really answered from
// its new source: the run cache, or the ledger's main. The readings go out as
// metrics, so the Bus page can show when each last did it in this environment
// (2026-11-18, the coordinator: "moved" was proven only by text in the code,
// not that it runs on PROD). A reader that fell back to its old source leaves
// no mark.
package readerseen

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type mark struct{ runs, last atomic.Int64 }

var marks sync.Map // reader id → *mark

// Saw says the reader just answered from its new source.
func Saw(reader string) { sawAt(reader, time.Now()) }

func sawAt(reader string, at time.Time) {
	v, _ := marks.LoadOrStore(reader, new(mark))
	m := v.(*mark)
	m.runs.Add(1)
	m.last.Store(at.Unix())
}

// Metric names, read back by the Bus page.
const (
	MetricRuns = "bifrost_bus_reader_new_source_total"
	MetricLast = "bifrost_bus_reader_new_source_last_timestamp_seconds"
)

// WriteMetrics writes the marks of this process in the exposition format.
func WriteMetrics(b *strings.Builder) {
	var ids []string
	marks.Range(func(k, _ any) bool { ids = append(ids, k.(string)); return true })
	sort.Strings(ids)
	fmt.Fprintf(b, "# HELP %s Answers a moved reader gave from its new source\n# TYPE %s counter\n", MetricRuns, MetricRuns)
	for _, id := range ids {
		v, _ := marks.Load(id)
		fmt.Fprintf(b, "%s{reader=%q} %d\n", MetricRuns, id, v.(*mark).runs.Load())
	}
	fmt.Fprintf(b, "# HELP %s When a moved reader last answered from its new source\n# TYPE %s gauge\n", MetricLast, MetricLast)
	for _, id := range ids {
		v, _ := marks.Load(id)
		fmt.Fprintf(b, "%s{reader=%q} %d\n", MetricLast, id, v.(*mark).last.Load())
	}
}
