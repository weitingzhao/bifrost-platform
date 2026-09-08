package safego

import (
	"sync"
	"testing"
)

func TestDoContainsAPanicAndReportsIt(t *testing.T) {
	before := Contained()
	if ok := Do("test.panics", func() { panic("boom") }); ok {
		t.Fatal("Do must report an unfinished fn")
	}
	if got := Contained() - before; got != 1 {
		t.Fatalf("expected 1 contained panic, got %d", got)
	}
	if ok := Do("test.clean", func() {}); !ok {
		t.Fatal("Do must report a clean run as completed")
	}
	if got := Contained() - before; got != 1 {
		t.Fatalf("a clean run must not count as a panic, got %d", got)
	}
}

// A loop that guards its body keeps ticking; that is the whole point of Do.
func TestDoLetsALoopSurviveOneBadIteration(t *testing.T) {
	ticks := 0
	for i := 0; i < 5; i++ {
		Do("test.loop", func() {
			ticks++
			if i == 2 {
				panic("bad tick")
			}
		})
	}
	if ticks != 5 {
		t.Fatalf("loop must run every iteration, got %d", ticks)
	}
}

func TestGoContainsAPanicWithoutKillingTheCaller(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	Go("test.go", func() {
		defer wg.Done()
		panic("boom")
	})
	wg.Wait() // a wedged WaitGroup here would hang the test, not fail it
}

// The pattern used at every fan-out site: Recover registered first, wg.Done
// still runs, so the caller's Wait returns instead of deadlocking.
func TestRecoverLetsWaitGroupDoneStillRun(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer Recover("test.fanout")
			defer wg.Done()
			if i == 0 {
				panic("half the fan-out dies")
			}
		}(i)
	}
	wg.Wait()
}
