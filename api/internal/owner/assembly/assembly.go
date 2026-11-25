// Package assembly is the owner domain's assembly: it mounts the owner's
// routes under /api/v1 (governance, the approval readings, the action
// record, the instruments, the generic write actions), mounts the Owner's
// questions, builds the question runtime and wires the action catalog and
// the generic write actions. Moved out of server (mount_owner.go and
// questions_wire.go) on 2026-11-19, unchanged: the same paths, gates,
// handlers, order and log lines (LAW-10: a domain's routes sit in its
// own package, so the platform's assembly root wires domains instead of
// holding them).
//
// What the owner reaches outside itself is lent to it: the gates, the
// handlers server still answers with (the trust-anchored action wiring among
// them), the action record's database, the cluster's clients, and what the
// delivery domain does with the policy and the mirrors (server keeps that
// glue).
package assembly

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// Gates are what the platform lends the owner's routes: the viewer,
// reporter and operator roles' gates and the catalogued action guard.
type Gates struct {
	Viewer   func(http.Handler) http.Handler
	Reporter func(http.Handler) http.Handler
	Operator func(http.Handler) http.Handler
	Guard    func(id string, next http.HandlerFunc) http.HandlerFunc
}

// Handlers are the owner's handlers server holds or answers with: the
// governance handler, the approval readings and instruments answers, the
// action record's database, and the generic write actions (actions_wire.go,
// trust-anchored, stays in server).
type Handlers struct {
	Governance       *agentgovernance.Handler
	ApprovalReadings http.HandlerFunc
	RecordDB         func() eventlog.DB
	Instruments      http.HandlerFunc
	PlanGet          http.HandlerFunc
	Plan             http.HandlerFunc
	Apply            http.HandlerFunc
	CreateJob        http.HandlerFunc
	DeleteFinished   http.HandlerFunc
	Probe            http.HandlerFunc
}

// Mount registers the owner domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func Mount(r chi.Router, g Gates, h Handlers) {
	r.Get("/agent/governance/trust-matrix", h.Governance.HandleTrustMatrix)
	// What the approval records say, counted by day and requester, each
	// count with the request numbers behind it. Read-only; viewer and above.
	r.With(g.Viewer).Get("/readings/approvals", h.ApprovalReadings)
	// The action record (action_record.go): what was done, by whom, why and on
	// which law, newest first, by seat, law, action and time. Read-only.
	r.With(g.Viewer).Get("/records", actionrecord.ListHandler(h.RecordDB))
	// The laws, Skills and MCP tools the source at main declares, for the Crew
	// page (governance_instruments.go). Read-only.
	r.With(g.Viewer).Get("/governance/instruments", h.Instruments)
	r.Get("/actuation/manifests/plans/{id}", h.PlanGet)
	// Recording a skill's own outcome is evidence, not actuation. It sits
	// below operator on purpose: the workload that reports must not also be
	// able to scale deployments or write its own trust override.
	r.Group(func(r chi.Router) {
		r.Use(g.Reporter)
		r.Post("/agent/governance/skill-runs", h.Governance.HandleRecordSkillRun)
	})
	r.Group(func(r chi.Router) {
		r.Use(g.Operator)
		r.Post("/actuation/manifests/plan", g.Guard("plan_manifest", h.Plan))
		r.Post("/actuation/manifests/apply", g.Guard("apply_manifest", h.Apply))
		r.Post("/actuation/jobs/from-cronjob", g.Guard("create_job_from_cronjob", h.CreateJob))
		r.Post("/actuation/jobs/delete-finished", g.Guard("delete_finished_jobs", h.DeleteFinished))
		r.Post("/actuation/probes", g.Guard("run_probe_pod", h.Probe))
	})
}

// Questions builds the Owner's question runtime under dataDir/questions and,
// in the process that runs the background loops, starts its sweep every
// sweep. Lines of server's wireQuestions until 2026-11-19, unchanged.
func Questions(dataDir string, audit *actuation.AuditLog, workers bool, sweep time.Duration) *questionbridge.Runtime {
	rt := questionbridge.New(filepath.Join(dataDir, "questions"), audit)
	if !workers || rt.Svc == nil {
		return rt
	}
	rt.StartSweep(sweep)
	return rt
}

// MountQuestions registers the Owner's questions' routes; with no runtime
// it registers nothing.
// BindQuestionInbox binds the questions' thread inbox port; the server passes
// the coordination domain's threadinbox (server/thread_inbox.go).
func BindQuestionInbox(recipient func(string) string, text func(string, int) string, newRunner func() questions.InboxRunner) {
	questions.SetInboxPorts(questions.InboxPorts{Recipient: recipient, Text: text, NewRunner: newRunner})
}

func MountQuestions(r chi.Router, auth *actuation.AuthService, rt *questionbridge.Runtime) {
	if rt == nil {
		return
	}
	questions.Mount(r, auth, rt.Svc)
}

// Cluster is what the generic write actions use of the cluster: its
// Kubernetes client and REST config. The cluster service has both.
type Cluster interface {
	KubernetesClient() (kubernetes.Interface, string, error)
	RestConfig() (*rest.Config, string, error)
}

// Work is what the action catalog and the generic write actions are given:
// the configuration directory the actuation policy is read from, the
// daemon's replicas and the D10 paths (server answers both), what the
// delivery domain does with the loaded policy and how it syncs one mirror
// commit, the cluster's clients, and the audit log.
type Work struct {
	ConfigDir      string
	DaemonReplicas func(ctx context.Context, namespace string) (int32, bool)
	D10Paths       func(ctx context.Context) actions.D10Paths
	OnPolicy       func(*actuationpolicy.Policy)
	Sync           func(ctx context.Context, repo, commit string) error
	Cluster        Cluster
	Audit          *actuation.AuditLog
}

// WireWorkActions gives the action catalog what it asks of this process (the
// daemon's replicas, the D10 paths, the actuation policy) and builds the
// generic write actions. Lines of server's wireWorkActions until 2026-11-19,
// unchanged. It answers nil when the actuation policy did not load (the
// generic write actions then refuse).
func WireWorkActions(w Work) *workactions.Handler {
	actions.SetDaemonReplicas(func(ctx context.Context, namespace string) (int32, bool) {
		return w.DaemonReplicas(ctx, namespace)
	})
	actions.SetD10Paths(w.D10Paths)
	pol, err := actuationpolicy.LoadFromConfigDir(w.ConfigDir)
	if err != nil {
		slog.Error("actuation policy not loaded; generic write actions refuse", "err", err)
		return nil
	}
	actions.SetActuationPolicy(pol)
	w.OnPolicy(pol)
	workSvc := &workactions.Service{
		Policy: pol,
		Sync:   w.Sync,
		Clients: workactions.Clients{
			Kube: func() (kubernetes.Interface, error) {
				cs, _, err := w.Cluster.KubernetesClient()
				return cs, err
			},
			Dynamic: func() (dynamic.Interface, error) {
				rc, _, err := w.Cluster.RestConfig()
				if err != nil {
					return nil, err
				}
				return dynamic.NewForConfig(rc)
			},
		},
	}
	work := &workactions.Handler{Svc: workSvc, Audit: w.Audit}
	actions.SetPlanLookup(func(ctx context.Context, planID string) (actions.PlanSummary, error) {
		sum, err := workSvc.Summarize(ctx, planID)
		if err != nil {
			return actions.PlanSummary{}, err
		}
		return actions.PlanSummary{Ready: sum.Ready, Namespaces: sum.Namespaces, Daemon: sum.Daemon, Path: sum.Path, Objects: len(sum.Objects), Migrated: sum.Migrated}, nil
	})
	return work
}
