package threadinbox

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestScanResumesAfterBudgetAndFollowsCurrentRecords(t *testing.T) {
	t.Parallel()
	var scan Scan
	items := []Conclusion{{Key: "approval:a:failed"}, {Key: "approval:b:failed"}, {Key: "approval:c:failed"}}
	load := func(context.Context) ([]Conclusion, error) { return items, nil }
	ctx, cancel := context.WithCancel(context.Background())
	var got []string
	err := scan.Run(ctx, load, func(_ context.Context, key, _, _ string) error {
		got = append(got, key)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || len(got) != 1 {
		t.Fatalf("first pass: %v %v", got, err)
	}
	// New records before the cursor cannot prevent later records progressing;
	// deleted sources are never replayed from a saved snapshot/queue.
	items = []Conclusion{{Key: "approval:0:failed"}, {Key: "approval:c:failed"}}
	if err := scan.Run(context.Background(), load, func(_ context.Context, key, _, _ string) error {
		got = append(got, key)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"approval:a:failed", "approval:c:failed", "approval:0:failed"}) {
		t.Fatalf("cursor skipped a current record or replayed a pruned one: %v", got)
	}
}

func TestScanFailureDoesNotStarveLaterRecords(t *testing.T) {
	t.Parallel()
	var scan Scan
	items := []Conclusion{{Key: "question:a:answered"}, {Key: "question:b:answered"}}
	load := func(context.Context) ([]Conclusion, error) { return items, nil }
	var got []string
	send := func(_ context.Context, key, _, _ string) error {
		got = append(got, key)
		if key == items[0].Key {
			return errors.New("unavailable")
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := scan.Run(context.Background(), load, send); err == nil {
			t.Fatal("failure hidden")
		}
	}
	if !reflect.DeepEqual(got, []string{items[0].Key, items[1].Key, items[0].Key}) {
		t.Fatalf("failure starved a later record: %v", got)
	}
}

func TestScanBudgetIncludesDiscoveryAndExcludesOverlappingPass(t *testing.T) {
	t.Parallel()
	var scan Scan
	called := false
	load := func(ctx context.Context) ([]Conclusion, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Fatal("discovery has no bounded budget")
		}
		if err := scan.Run(ctx, func(context.Context) ([]Conclusion, error) {
			called = true
			return nil, nil
		}, nil); err != nil {
			t.Fatal(err)
		}
		return nil, fmt.Errorf("discovery unavailable")
	}
	if err := scan.Run(context.Background(), load, nil); err == nil || called {
		t.Fatal("discovery failure hidden or overlapping scan started")
	}
	// Failure releases the scan lock and leaves the next read possible.
	if err := scan.Run(context.Background(), func(context.Context) ([]Conclusion, error) {
		called = true
		return nil, nil
	}, nil); err != nil || !called {
		t.Fatal("failed scan retained its lock")
	}
}

func TestScanHasNoReceiptCache(t *testing.T) {
	t.Parallel()
	var scan Scan
	load := func(context.Context) ([]Conclusion, error) {
		return []Conclusion{{Key: "approval:a:executed"}, {Key: "approval:a:unknown"}}, nil
	}
	var got []string
	send := func(_ context.Context, key, _, _ string) error { got = append(got, key); return nil }
	for i := 0; i < 2; i++ {
		if err := scan.Run(context.Background(), load, send); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, []string{"approval:a:unknown", "approval:a:executed", "approval:a:unknown", "approval:a:executed"}) {
		t.Fatalf("PG receipt is not the sole authority or late states are reversed: %v", got)
	}
}
