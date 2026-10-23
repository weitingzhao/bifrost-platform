package controller

import (
	"context"
	"sync"
	"time"
)

// RunLoopForTest runs pass as the controller runs a loop named name, every d,
// until ctx ends, and waits for it.
func (c *Controller) RunLoopForTest(ctx context.Context, name string, d time.Duration, pass func() error) {
	var wg sync.WaitGroup
	c.every(ctx, &wg, name, d, pass)
	wg.Wait()
}

// SetPresencePassLimitForTest sets the presence pass's bound and returns the
// function that restores it.
func SetPresencePassLimitForTest(d time.Duration) func() {
	old := presencePassLimit
	presencePassLimit = d
	return func() { presencePassLimit = old }
}
