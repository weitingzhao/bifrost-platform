package store_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The eta from real history (2.5 G8, review 350 B3): three accepted
// implementations of 100 s make the usual time, and a mission with two
// queued implementations should be done in 200 to 300 s; with two accepted
// there is no usual time and the eta says so.
func TestProgressETAFromHistory(t *testing.T) {
	t.Parallel()
	for _, accepted := range []int{2, 3} {
		t.Run(fmt.Sprint(accepted), func(t *testing.T) {
			t.Parallel()
			w := newKernelWorld(t)
			for range accepted {
				w.ended(t, "done")
			}
			w.ready(t, store.Task{})
			w.ready(t, store.Task{})
			before := time.Now()
			doc := w.progress(t)
			var eta map[string]any
			for _, m := range doc.Work.Missions {
				if m.MissionID == "M-1" {
					eta = m.ETA
				}
			}
			if accepted < store.UsualMinSamples {
				if eta["state"] != "unknown" || !strings.Contains(fmt.Sprint(eta["reason"]), "no usual time for implement go tasks") {
					t.Fatalf("eta with %d accepted = %v; want unknown, no usual time", accepted, eta)
				}
				return
			}
			want := []any{map[string]any{"role": "implement", "task_class": "go", "usual_s": float64(100), "samples": float64(3)}}
			if eta["state"] != "estimated" || !reflect.DeepEqual(eta["basis"], want) {
				t.Fatalf("eta = %v; want estimated from implement go, 100 s, 3 samples", eta)
			}
			for field, d := range map[string]time.Duration{"earliest": 200 * time.Second, "latest": 300 * time.Second} {
				at, err := time.Parse(time.RFC3339, fmt.Sprint(eta[field]))
				ok(t, err)
				if off := at.Sub(before.Add(d)); off < -2*time.Second || off > 5*time.Second {
					t.Errorf("%s is %s from now; want about %s", field, at.Sub(before), d)
				}
			}
		})
	}
}
