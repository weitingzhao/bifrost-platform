package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentdeploy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// deploy_agentd's wiring (agentdeploy/agentd.go): the executor reaches the
// agent runtime's records through the store, the hosts' ssh targets through
// AGENTD_DEPLOY_TARGETS and the published build through the Gitea agentd
// package (AGENTD_PACKAGE_SHA256_URL, {commit} in it; by default the generic
// package agentd of the bifrost org, file agentd.sha256), read with the
// dedicated package-read account. PROD's controller proposes the cards.

const agentdPackageURLEnv = "AGENTD_PACKAGE_SHA256_URL"

func agentdPackage() agentdeploy.Package {
	u := strings.TrimSpace(os.Getenv(agentdPackageURLEnv))
	if u == "" {
		u = delivery.GiteaBaseURL() + "/api/packages/bifrost/generic/agentd/{commit}/agentd.sha256"
	}
	return agentdeploy.Package{URL: u, Auth: delivery.AgentdPackageReaderFromEnv}
}

// agentdHosts is agentdeploy.HostControl over the runtime's store.
type agentdHosts struct{ db store.DB }

func (h agentdHosts) SetAside(ctx context.Context, host, reason string) (string, error) {
	return store.SetHostAside(ctx, h.db, host, reason)
}

func (h agentdHosts) SetBack(ctx context.Context, id string) error {
	return store.SetHostBack(ctx, h.db, id)
}

func (h agentdHosts) Running(ctx context.Context, host string) (int, error) {
	return store.HostLiveAttempts(ctx, h.db, host)
}

func (h agentdHosts) Build(ctx context.Context, host string) (string, error) {
	return store.HostAgentdCommit(ctx, h.db, host)
}

// agentdJobs is the process's agent deploy job store, made on first use.
var agentdJobs = sync.OnceValue(agentdeploy.NewStore)

// deployAgentd is the deploy_agentd executor.
func (s *Server) deployAgentd(ctx context.Context, params map[string]any) (any, error) {
	if _, ok := actions.CreateAttemptFrom(ctx); !ok {
		return nil, errors.New("deploy_agentd runs only as an approval's execution")
	}
	if !approvalhome.IsProd() || text(params["env"]) != "prod" {
		return nil, errors.New("deploy_agentd is PROD only; " + approvalhome.Required)
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil {
		return nil, errors.New("this platform runs no agent runtime: no host to deploy agentd on")
	}
	if rt.env != "prod" {
		return nil, fmt.Errorf("this platform runs the %q runtime, not prod: use ops.bifrost.lan", rt.env)
	}
	d := &agentdeploy.Agentd{
		Targets:       agentdeploy.TargetsFromEnv,
		PackageSHA256: agentdPackage().SHA256,
		Hosts:         agentdHosts{db: rt.api.DB},
		SSH:           agentdeploy.SSHRunner{},
		Store:         agentdJobs(),
	}
	return d.Run(ctx, params)
}

// agentdDeployCreateCheck refuses a card whose params have the wrong form or
// name a host with no configured ssh target, before the Owner sees it.
func agentdDeployCreateCheck(params map[string]any) error {
	p, err := agentdeploy.ParseAgentdParams(params)
	if err != nil {
		return err
	}
	targets, err := agentdeploy.TargetsFromEnv()
	if err != nil {
		return err
	}
	if _, ok := targets[p.Host]; !ok {
		return fmt.Errorf("host %s has no ssh target in %s", p.Host, agentdeploy.TargetsEnv)
	}
	return nil
}

// agentdDeployOpenKey: at most one open deploy_agentd card per host.
func agentdDeployOpenKey(action string, params map[string]any) string {
	if action != agentdeploy.DeployAgentdAction {
		return ""
	}
	host, _ := params["host"].(string)
	if host == "" {
		return ""
	}
	return "agentd-deploy:" + host
}

// approvalOpenKey is the one-open-request rule of every action that has one.
func approvalOpenKey(action string, params map[string]any) string {
	if k := landingGrantOpenKey(action, params); k != "" {
		return k
	}
	if k := ruleApproveOpenKey(action, params); k != "" {
		return k
	}
	return agentdDeployOpenKey(action, params)
}

// wireAgentdDeploy gives PROD's controller the deploy_agentd proposal
// (controller/agentddeploy.go) for the configured hosts; no host, another
// environment or no approvals wires none.
func (s *Server) wireAgentdDeploy(c *controller.Controller) {
	if c.Env != "prod" || c.DB == nil || s.approvals == nil {
		return
	}
	targets, err := agentdeploy.TargetsFromEnv()
	if err != nil {
		slog.Error("agentrt agentd deploy not started", "error", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	hosts := make([]string, 0, len(targets))
	for h := range targets {
		hosts = append(hosts, h)
	}
	slices.Sort(hosts)
	pkg := agentdPackage()
	db := c.DB
	c.AgentdDeploy = &controller.AgentdDeploy{
		Hosts: hosts,
		Build: func(ctx context.Context, host string) (string, error) { return store.HostAgentdCommit(ctx, db, host) },
		Published: func(ctx context.Context, commit string) (string, bool, error) {
			sha, err := pkg.SHA256(ctx, commit)
			if errors.Is(err, agentdeploy.ErrNotPublished) {
				return "", false, nil
			}
			return sha, err == nil, err
		},
		Cards: func(ctx context.Context) ([]controller.AgentdCard, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			list, err := s.approvals.Observations()
			if err != nil {
				return nil, err
			}
			var out []controller.AgentdCard
			for _, a := range list {
				if a.Drill || a.Action != agentdeploy.DeployAgentdAction {
					continue
				}
				host, _ := a.Params["host"].(string)
				commit, _ := a.Params["commit"].(string)
				out = append(out, controller.AgentdCard{Host: host, Commit: commit, Status: a.Status})
			}
			return out, nil
		},
		// The approvals keep at most one open card per host (approvalOpenKey);
		// the key host|commit is the controller's, against the cards above.
		Request: func(ctx context.Context, key string, params map[string]any, reason string) (string, error) {
			a, err := s.approvals.Request(ctx, "controller", agentdeploy.DeployAgentdAction, reason+" Key "+key+".",
				"Reject the card: the host keeps the build it runs. After a deploy, a failed check rolls back by itself (agentd-update --rollback).", params)
			return a.ID, err
		},
	}
}
