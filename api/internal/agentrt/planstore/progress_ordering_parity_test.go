package planstore

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"
)

func frozenSortedLive(live map[string]*ProgressAttempt) []*ProgressAttempt {
	list := make([]*ProgressAttempt, 0, len(live))
	for _, a := range live {
		list = append(list, a)
	}
	slices.SortFunc(list, func(x, y *ProgressAttempt) int {
		switch {
		case x.started.IsZero() != y.started.IsZero():
			if x.started.IsZero() {
				return 1
			}
			return -1
		case !x.started.Equal(y.started):
			return x.started.Compare(y.started)
		}
		if x.AttemptID < y.AttemptID {
			return -1
		}
		return 1
	})
	return list
}

func TestProgressDisplayOrderingFrozen(t *testing.T) {
	t.Parallel()
	rank := []string{StatusHostLost, StatusMaybeStuck, StatusUnknown, StatusWaitingOwner, StatusStopped, statusBlocked, StatusSlow, StatusWorking, StatusWaitingRole, StatusQueued}
	for _, a := range append(append([]string{}, rank...), "unrecognized", "") {
		for _, b := range append(append([]string{}, rank...), "unrecognized", "") {
			best := len(rank)
			for _, s := range []string{a, b} {
				if i := slices.Index(rank, s); i >= 0 && i < best {
					best = i
				}
			}
			want := ""
			if best < len(rank) {
				want = rank[best]
			}
			if got := worst([]string{a, b}); got != want {
				t.Fatalf("rank %q %q = %q != %q", a, b, got, want)
			}
		}
	}
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for n := 0; n < 12; n++ {
		m := map[string]*ProgressAttempt{}
		for i := 0; i < n; i++ {
			a := &ProgressAttempt{AttemptID: fmt.Sprintf("a-%02d", i)}
			if i%3 != 0 {
				a.started = now.Add(time.Duration(i%4) * time.Second)
			}
			m[a.AttemptID] = a
		}
		if got, want := sortedLive(m), frozenSortedLive(m); !reflect.DeepEqual(got, want) {
			t.Fatal("live ordering differs")
		}
	}
}
