package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// The agentd deploy proposal: when PROD's release runs a platform commit
// whose agentd build is published, and an execution host's claims report
// another build, the platform requests one deploy_agentd card for the host
// and that commit; the Owner approves it and the executor deploys it
// (agentdeploy/agentd.go). The approvals are the record: a host with a
// deploy_agentd card open asks nothing, and a host and commit that already
// had a card (whatever became of it) are never asked again, so a rejected or
// failed deploy is not proposed over and over; a newer commit is a new card.

// AgentdDeployEvery is how often the loop looks.
const AgentdDeployEvery = 5 * time.Minute

// AgentdDeployAction is the card's action.
const AgentdDeployAction = "deploy_agentd"

// AgentdCard is a deploy_agentd approval as the loop sees it.
type AgentdCard struct {
	Host, Commit, Status string
}

// open: pending, approved, running or not known yet.
func (c AgentdCard) open() bool {
	switch c.Status {
	case "pending", "approved", "running", "unknown":
		return true
	}
	return false
}

// AgentdDeploy is what the loop reads and asks; nil on the Controller runs
// no loop. Hosts are the configured execution hosts; Build reads the commit
// of the agentd build a host's claims report ("" while none); Published the
// sha256 the agentd package publishes for a commit (ok false while none is);
// Cards lists the deploy_agentd approvals; Request asks for a card with its
// idempotency key (host|commit). PROD's platform commit is
// Sources.Deployed's.
type AgentdDeploy struct {
	Hosts     []string
	Build     func(ctx context.Context, host string) (string, error)
	Published func(ctx context.Context, commit string) (sha256 string, ok bool, err error)
	Cards     func(ctx context.Context) ([]AgentdCard, error)
	Request   func(ctx context.Context, key string, params map[string]any, reason string) (string, error)

	mu    sync.Mutex
	asked map[string]bool // the keys this process asked, until Cards shows them
}

// startAgentdDeploy runs the loop when there is one, timed as the others are.
func (c *Controller) startAgentdDeploy(ctx context.Context, wg *sync.WaitGroup) {
	if c.AgentdDeploy == nil {
		return
	}
	c.every(ctx, wg, "agentrt-agentd-deploy", AgentdDeployEvery, func() error {
		passCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		_, err := c.AgentdDeployPass(passCtx)
		return err
	})
}

// AgentdDeployPass is one pass: it returns how many cards it requested. One
// host's error does not stop the others.
func (c *Controller) AgentdDeployPass(ctx context.Context) (int, error) {
	a := c.AgentdDeploy
	if a == nil || a.Build == nil || a.Published == nil || a.Cards == nil || a.Request == nil || c.Sources.Deployed == nil || len(a.Hosts) == 0 {
		return 0, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	commit, _, ok := c.boundedDeployed(ctx, "prod")
	if !ok || commit == "" {
		return 0, nil // PROD's release is not settled: nothing to compare with
	}
	cards, err := a.Cards(ctx)
	if err != nil {
		return 0, fmt.Errorf("agentd deploy cards: %w", err)
	}
	var sha string
	published := false
	asked := 0
	var errs []error
	for _, host := range a.Hosts {
		key := host + "|" + commit
		if agentdAsked(cards, host, commit) || a.asked[key] {
			continue
		}
		build, err := a.Build(ctx, host)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: build: %w", host, err))
			continue
		}
		if build == "" || build == commit {
			continue // not known yet, or already there
		}
		if !published {
			s, ok, err := a.Published(ctx, commit)
			if err != nil {
				return asked, errors.Join(append(errs, fmt.Errorf("agentd package of %s: %w", commit, err))...)
			}
			if !ok {
				return asked, errors.Join(errs...) // no build of PROD's commit published yet
			}
			sha, published = s, true
		}
		params := map[string]any{"env": "prod", "host": host, "commit": commit, "sha256": sha}
		reason := fmt.Sprintf("Host %s runs agentd %s; PROD's platform commit is %s, whose agentd build is published (sha256 %s).",
			host, shortCommit(build), shortCommit(commit), sha)
		if _, err := a.Request(ctx, key, params, reason); err != nil {
			errs = append(errs, fmt.Errorf("%s: request: %w", host, err))
			continue
		}
		if a.asked == nil {
			a.asked = map[string]bool{}
		}
		a.asked[key] = true
		asked++
	}
	return asked, errors.Join(errs...)
}

// agentdAsked: a card is open for host, or one was made for host and commit.
func agentdAsked(cards []AgentdCard, host, commit string) bool {
	for _, c := range cards {
		if c.Host == host && (c.open() || c.Commit == commit) {
			return true
		}
	}
	return false
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
