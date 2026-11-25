package catalog

import (
	"sync"
	"time"
)

// KeptTTL is how long a read of the state store is answered from memory.
// The workers write the map once a round (Every); the read routes asked the
// cluster for every ConfigMap on every request, and the cluster client lets
// about five through a second for the whole process (2026-11-24, UI/World:
// the City's 14 domain reads at once came back one by one, 16 to 44 seconds
// in all, 2.8 seconds alone).
const KeptTTL = 30 * time.Second

// cachedKept answers reads from memory for KeptTTL, a missing name too, and
// keeps what this process writes. A read that fails is not kept.
type cachedKept struct {
	under Kept
	now   func() time.Time
	mu    sync.Mutex
	got   map[string]keptRead
}

type keptRead struct {
	raw []byte
	at  time.Time
}

func newCachedKept(under Kept, now func() time.Time) *cachedKept {
	return &cachedKept{under: under, now: now, got: map[string]keptRead{}}
}

func (c *cachedKept) Read(name string) ([]byte, error) {
	now := c.now()
	c.mu.Lock()
	r, ok := c.got[name]
	c.mu.Unlock()
	if ok && now.Sub(r.at) < KeptTTL {
		return r.raw, nil
	}
	raw, err := c.under.Read(name)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.got[name] = keptRead{raw: raw, at: now}
	c.mu.Unlock()
	return raw, nil
}

func (c *cachedKept) Write(name string, raw []byte) error {
	if err := c.under.Write(name, raw); err != nil {
		c.mu.Lock()
		delete(c.got, name)
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.got[name] = keptRead{raw: raw, at: c.now()}
	c.mu.Unlock()
	return nil
}
