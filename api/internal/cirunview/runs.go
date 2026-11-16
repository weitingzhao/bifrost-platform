// Package cirunview serves cached CI run observations, without deciding or executing releases.
package cirunview

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"strconv"
	"sync"
	"time"
)

// Keep bounds the existing run-view cache lifetime. It protects cluster reads.
const Keep = 30 * time.Second

type Answer struct {
	Repo string              `json:"repo"`
	AsOf string              `json:"as_of"`
	Runs []landingci.RunView `json:"runs"`
}

type Cache struct {
	mu   sync.Mutex
	kept map[string]keptAnswer
}

type keptAnswer struct {
	at     time.Time
	answer Answer
}

// Read answers from what was kept when that is recent enough, and reads
// otherwise. A failed read is not kept.
func (c *Cache) Read(ctx context.Context, read func(ctx context.Context, repo string, limit int) ([]landingci.RunView, error),
	now time.Time, repo string, limit int) (Answer, error) {
	key := repo + "\x00" + strconv.Itoa(limit)
	c.mu.Lock()
	if k, ok := c.kept[key]; ok && now.Sub(k.at) < Keep {
		c.mu.Unlock()
		return k.answer, nil
	}
	c.mu.Unlock()
	runs, err := read(ctx, repo, limit)
	if err != nil {
		return Answer{}, err
	}
	answer := Answer{Repo: repo, AsOf: now.UTC().Format(time.RFC3339), Runs: runs}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.kept == nil || len(c.kept) > 64 {
		c.kept = map[string]keptAnswer{}
	}
	c.kept[key] = keptAnswer{at: now, answer: answer}
	return answer, nil
}
