package elasticreadings

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	// hostReadingFresh is how long a reading is served without a new probe.
	hostReadingFresh = 20 * time.Second
	// hostReadingMaxAge is the oldest reading a page is shown. 15 min (was 2,
	// 2026-11-20): /api/v1/cluster is opened about every 8 min (7 calls in 1 h,
	// P50 3.26 s), so a 2 min keep made nearly every call pay the 3 s probe of
	// the powered-off host. Display only; nothing that acts reads this.
	// Between the two the reading is served and a probe runs behind it.
	hostReadingMaxAge = 15 * time.Minute
	// hostRefreshTimeout bounds a probe no request is waiting on.
	hostRefreshTimeout = 5 * time.Second
)

type hostReading struct {
	reachable  bool
	at         time.Time
	refreshing bool
}

// HostReadings keeps the last answer of each on-demand host for pages that
// poll. A powered-off host never answers, so without it every poll of the
// cluster page waits out the SSH connect timeout (2026-11-18 PROD: 655 of 658
// calls of /cluster/nodes in 6 h took 2.5-5 s). The zero value is ready.
type HostReadings struct {
	mu       sync.Mutex
	readings map[string]*hostReading
	clock    func() time.Time // unit tests only
}

func (d *HostReadings) now() time.Time {
	if d.clock != nil {
		return d.clock()
	}
	return time.Now()
}

// Read answers from the last reading while it is young enough, and otherwise
// from probe. Only a page may use it: the answer can be up to
// hostReadingMaxAge old, so nothing may act on it.
func (d *HostReadings) Read(ctx context.Context, host string, probe func(context.Context, string) bool) bool {
	host = strings.TrimSpace(host)
	d.mu.Lock()
	r := d.readings[host]
	if r != nil {
		age := d.now().Sub(r.at)
		if age < hostReadingMaxAge {
			reachable := r.reachable
			if age >= hostReadingFresh && !r.refreshing {
				r.refreshing = true
				safego.Go("elasticreadings.HostReadings.refresh", func() { d.refresh(host, probe) })
			}
			d.mu.Unlock()
			return reachable
		}
	}
	d.mu.Unlock()

	reachable := probe(ctx, host)
	// A probe cut short by its request says nothing about the host.
	if ctx.Err() == nil {
		d.store(host, reachable)
	}
	return reachable
}

func (d *HostReadings) refresh(host string, probe func(context.Context, string) bool) {
	ctx, cancel := context.WithTimeout(context.Background(), hostRefreshTimeout)
	defer cancel()
	reachable := false
	finished := false
	defer func() {
		if finished {
			d.store(host, reachable)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if r := d.readings[host]; r != nil {
			r.refreshing = false
		}
	}()
	reachable = probe(ctx, host)
	finished = ctx.Err() == nil
}

func (d *HostReadings) store(host string, reachable bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readings == nil {
		d.readings = map[string]*hostReading{}
	}
	d.readings[host] = &hostReading{reachable: reachable, at: d.now()}
}
