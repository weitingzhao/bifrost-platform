package controller_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
)

const (
	agentdProd = "1111111111111111111111111111111111111111"
	agentdOld  = "2222222222222222222222222222222222222222"
	agentdSHA  = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// agentdApprovals is the approvals store for deploy_agentd, in memory.
type agentdApprovals struct {
	cards []controller.AgentdCard
	keys  []string
}

func agentdController(builds map[string]string, published bool, ap *agentdApprovals) *controller.Controller {
	c := &controller.Controller{}
	c.Sources.Deployed = func(_ context.Context, env string) (string, map[string]any, bool) {
		if env != "prod" {
			return "", nil, false
		}
		return agentdProd, nil, true
	}
	c.AgentdDeploy = &controller.AgentdDeploy{
		Hosts: []string{"mini-1"},
		Build: func(_ context.Context, host string) (string, error) { return builds[host], nil },
		Published: func(_ context.Context, commit string) (string, bool, error) {
			return agentdSHA, published && commit == agentdProd, nil
		},
		Cards: func(context.Context) ([]controller.AgentdCard, error) { return ap.cards, nil },
		Request: func(_ context.Context, key string, p map[string]any, _ string) (string, error) {
			ap.keys = append(ap.keys, key)
			ap.cards = append(ap.cards, controller.AgentdCard{Host: p["host"].(string), Commit: p["commit"].(string), Status: "pending"})
			return "a1", nil
		},
	}
	return c
}

// A host on another build than PROD's published one gets one card, keyed by
// host and commit, and the next passes ask nothing more: not while it is
// open, and not after the Owner rejected it.
func TestAgentdDeployProposesOneCard(t *testing.T) {
	ap := &agentdApprovals{}
	c := agentdController(map[string]string{"mini-1": agentdOld}, true, ap)
	ctx := context.Background()
	n, err := c.AgentdDeployPass(ctx)
	if err != nil || n != 1 {
		t.Fatalf("first pass: %d %v", n, err)
	}
	if len(ap.keys) != 1 || ap.keys[0] != "mini-1|"+agentdProd {
		t.Fatalf("keys %v", ap.keys)
	}
	if n, err := c.AgentdDeployPass(ctx); err != nil || n != 0 {
		t.Fatalf("second pass: %d %v", n, err)
	}
	ap.cards[0].Status = "rejected"
	if n, err := c.AgentdDeployPass(ctx); err != nil || n != 0 || len(ap.keys) != 1 {
		t.Fatalf("after the rejection: %d %v %v", n, err, ap.keys)
	}
}

// Nothing is asked while the host runs PROD's build, reports none, or no
// build of PROD's commit is published; nor while another card is open for
// the host.
func TestAgentdDeployAsksNothingWithoutCause(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]*controller.Controller{
		"same build":    agentdController(map[string]string{"mini-1": agentdProd}, true, &agentdApprovals{}),
		"no build":      agentdController(map[string]string{}, true, &agentdApprovals{}),
		"not published": agentdController(map[string]string{"mini-1": agentdOld}, false, &agentdApprovals{}),
		"open card": agentdController(map[string]string{"mini-1": agentdOld}, true,
			&agentdApprovals{cards: []controller.AgentdCard{{Host: "mini-1", Commit: agentdOld, Status: "approved"}}}),
	} {
		if n, err := c.AgentdDeployPass(ctx); err != nil || n != 0 {
			t.Errorf("%s: %d %v", name, n, err)
		}
	}
}
