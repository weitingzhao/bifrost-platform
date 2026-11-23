package assembly

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// The landing CI's client as the platform builds it: the cluster's transports
// and the bus run cache are lent by server, the client is verification's.
// Moved out of server (ci_client.go, landingCIClient) on 2026-11-23,
// unchanged: a new client for each use, as before.

// CILent is what the platform lends the landing CI's client.
type CILent struct {
	// Namespace is the stack's namespace; "" is the landing CI's default.
	Namespace string
	// GateRuns reads the daily full CI's runs through the bus run cache;
	// nil lists them.
	GateRuns func(pipeline string, keep func(*unstructured.Unstructured) bool, listed func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error)
	// Dynamic and Kubernetes are the cluster's clients, made when called.
	Dynamic    func() (dynamic.Interface, error)
	Kubernetes func() (kubernetes.Interface, error)
}

func (l CILent) client() *landingci.Client {
	namespace := landingci.DefaultNamespace
	if l.Namespace != "" {
		namespace = l.Namespace
	}
	return &landingci.Client{Namespace: namespace, GateRuns: l.GateRuns, Dynamic: l.Dynamic, Logs: landingci.PodLogs(l.Kubernetes, namespace)}
}

// FullCICheckSources are the reads of GET /units/full-ci/check: firstParents
// from Gitea, the runs' modules and failed tasks from the landing CI. Moved
// out of server (ci_read_wire.go, handleUnitFullCICheck) on 2026-11-23.
func FullCICheckSources(ci CILent, firstParents func(ctx context.Context, repo, green, red string, max int) ([][2]string, error)) fullci.CheckSources {
	c := ci.client()
	return fullci.CheckSources{FirstParents: firstParents, RunModules: c.RunModulesOf, FailedTasks: c.FullRunFailedTasks}
}

// RunsKept keeps the run view's answers a short while (cirunview.Cache) for
// GET /units/runs. Moved out of server (units_runs.go, unitRunsCache) on
// 2026-11-23.
type RunsKept struct{ cache cirunview.Cache }

// Read is the newest runs of repo, at most limit, read with the landing CI's
// client unless kept.
func (k *RunsKept) Read(ctx context.Context, ci CILent, now time.Time, repo string, limit int) (cirunview.Answer, error) {
	return k.cache.Read(ctx, ci.client().RecentRuns, now, repo, limit)
}
