// Package maintainer is the shared liveness helper for platform-workers loops.
//
// Each successful pass of a loop calls Success with the MAINTAINERS.yaml id
// (platform/<env>/<name>). /metrics then exposes
//
//	bifrost_maintainer_last_success_timestamp_seconds{maintainer}
//	bifrost_maintainer_runs_total{maintainer,result}
//
// The exposition is hand-written, like the rest of /metrics: this module has
// no Prometheus client. A loop that has never succeeded emits no gauge series,
// so an absent() rule can see it. Failure increments the counter and leaves
// the gauge where it was.
package maintainer

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LoopIBAutoRepair    = "ib-autorepair"
	LoopDataClone       = "data-clone-scheduler"
	LoopBackupSweep     = "failed-backup-sweep"
	LoopPatrol          = "patrol-autopilot"
	LoopChecklistProber = "checklist-prober"
	LoopReleaseRecorder = "release-recorder"

	ResultSuccess = "success"
	ResultFailure = "failure"
)

// Loop is one workers background loop this tree starts. Tests walk Loops and
// require each File to record the metric via Symbol.
type Loop struct {
	Name   string
	Symbol string
	File   string // relative to the api/ module root
}

// Loops is the startup inventory of workers loops that exist in this tree.
func Loops() []Loop {
	return []Loop{
		{Name: LoopIBAutoRepair, Symbol: "LoopIBAutoRepair", File: "internal/ibgateway/autorepair.go"},
		{Name: LoopDataClone, Symbol: "LoopDataClone", File: "internal/cluster/data_clone.go"},
		{Name: LoopBackupSweep, Symbol: "LoopBackupSweep", File: "internal/cluster/postgres_wal_repair.go"},
		{Name: LoopPatrol, Symbol: "LoopPatrol", File: "internal/patrol/handler.go"},
		{Name: LoopChecklistProber, Symbol: "LoopChecklistProber", File: "internal/checklist/prober.go"},
		{Name: LoopReleaseRecorder, Symbol: "LoopReleaseRecorder", File: "internal/releases/service.go"},
	}
}

// PlatformID builds the MAINTAINERS.yaml id for the environment this process
// is maintaining. OPS_VIEWER_ENV is prod or stg on the workers Deployments.
// An empty value (a laptop) uses local so it does not pretend to be prod.
func PlatformID(name string) string {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("OPS_VIEWER_ENV")))
	switch env {
	case "prod", "stg", "dev":
	default:
		if env == "" {
			env = "local"
		}
	}
	return "platform/" + env + "/" + name
}

var now = time.Now

type sample struct {
	lastSuccess int64
	hasSuccess  bool
	counts      map[string]uint64
}

var (
	mu      sync.Mutex
	samples = map[string]*sample{}
)

// Success records one completed pass of maintainer id.
func Success(id string) { Note(id, ResultSuccess) }

// Failure records one pass that did not complete.
func Failure(id string) { Note(id, ResultFailure) }

// Note records one pass. result success moves the last-success gauge.
func Note(id, result string) {
	id = strings.TrimSpace(id)
	result = strings.TrimSpace(result)
	if id == "" || result == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s := samples[id]
	if s == nil {
		s = &sample{counts: map[string]uint64{}}
		samples[id] = s
	}
	if s.counts == nil {
		s.counts = map[string]uint64{}
	}
	s.counts[result]++
	if result == ResultSuccess {
		s.lastSuccess = now().Unix()
		s.hasSuccess = true
	}
}

// Reset drops recorded samples. Tests use it.
func Reset() {
	mu.Lock()
	samples = map[string]*sample{}
	mu.Unlock()
}

// Write appends the two maintainer series to a Prometheus text exposition.
func Write(b *strings.Builder) {
	mu.Lock()
	defer mu.Unlock()
	ids := make([]string, 0, len(samples))
	for id := range samples {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	b.WriteString("# HELP bifrost_maintainer_last_success_timestamp_seconds Unix time of the last successful pass of a platform-workers loop.\n")
	b.WriteString("# TYPE bifrost_maintainer_last_success_timestamp_seconds gauge\n")
	for _, id := range ids {
		s := samples[id]
		if !s.hasSuccess {
			continue
		}
		b.WriteString("bifrost_maintainer_last_success_timestamp_seconds{maintainer=\"")
		b.WriteString(escapeLabel(id))
		b.WriteString("\"} ")
		b.WriteString(strconv.FormatInt(s.lastSuccess, 10))
		b.WriteByte('\n')
	}

	b.WriteString("# HELP bifrost_maintainer_runs_total Passes of a platform-workers loop, by result.\n")
	b.WriteString("# TYPE bifrost_maintainer_runs_total counter\n")
	for _, id := range ids {
		s := samples[id]
		results := make([]string, 0, len(s.counts))
		for result := range s.counts {
			results = append(results, result)
		}
		sort.Strings(results)
		for _, result := range results {
			b.WriteString("bifrost_maintainer_runs_total{maintainer=\"")
			b.WriteString(escapeLabel(id))
			b.WriteString("\",result=\"")
			b.WriteString(escapeLabel(result))
			b.WriteString("\"} ")
			b.WriteString(strconv.FormatUint(s.counts[result], 10))
			b.WriteByte('\n')
		}
	}
}

func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(v)
}
