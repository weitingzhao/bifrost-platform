package server

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deploysource"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/runtimedb"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/schemawatch"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasetable"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// agentRuntime is the agent runtime as this process runs it.
type agentRuntime struct {
	api  *agentrtapi.API
	stop func()
	// env is AGENTRT_ENV: whose runtime this platform runs.
	env string
	// schema is what the platform last read of the database's agentrt
	// schema (TD-411, agentrt/schemawatch); nil without AGENTRT_DSN.
	schema *schemawatch.Watch
	// evidenceCI is the ledger answering "did CI pass" (evidence_gate.go);
	// nil in a process with no delivery service: then CI is not read.
	evidenceCI delivery.CIReader
	// late is the runtime's database as it connects: its routes are ready
	// once it is (/health says agentrt_ready); nil without AGENTRT_DSN.
	late *runtimedb.Late
	// fullCI is the controller's daily full CI once the workers process
	// has wired it (wireFullCI), for the gauge /metrics serves; nil in a
	// process that runs no controller, or where the loop is off.
	fullCI atomic.Pointer[fullci.FullCI]
}

// wireAgentRuntime connects the agent runtime (design v5 §5.2) when
// AGENTRT_DSN is set (a libpq connection string without the password; the
// password comes from AGENTRT_DB_PASSWORD, which the Deployment fills from
// the Secret agentrt-db-runtime): a pool, refused unless it is the runtime role of
// AGENTRT_ENV on that environment's database; the routes; and in the workers
// process the controller loops. Without it the routes answer 503 and nothing
// runs, so the code ships before the database is wired into the Deployment.
//
// The controller reads approvals, questions, CI verdicts and deployments from the
// platform's own stores (agentRuntimeSources); a record it cannot read leaves
// the rule atoms on it unknown, so nothing is judged done on facts it cannot
// see.
func (s *Server) wireAgentRuntime(role config.Role) {
	rt := &agentRuntime{api: &agentrtapi.API{Logf: func(format string, args ...any) {
		slog.Warn("agentrt", "detail", fmt.Sprintf(format, args...))
	}, Ask: s.questionRuntime.Ask(), Anchor: releasepolicy.OwnerKeyFingerprint,
		DiskFloor: deliveryview.DiskFloorFromEnv(), MailWake: s.mailWake}, env: strings.TrimSpace(os.Getenv("AGENTRT_ENV"))}
	s.agentRuntime = rt
	rt.evidenceCI = s.wireEvidenceGate()
	if s.releases != nil && s.releases.Service() != nil {
		rt.api.ProductionDelivery = func(ctx context.Context) deliveryview.DeliveryProduction {
			return deploysource.ProductionEvidence(ctx, s.releases.Service())
		}
	}
	if s.delivery != nil {
		s.delivery.ReadConfigurationMainThrough(func(gitea delivery.MainHeadReader) delivery.MainHeadReader {
			return func(ctx context.Context, repo string) (string, error) {
				return s.mainOf("config-boundaries", gitea)(ctx, repo)
			}
		})
		rt.api.ConfigurationBoundaries = s.delivery.ConfigurationBoundaries
	}
	if s.approvals != nil {
		s.approvals.Assessment = s.assessmentReader(assessmentreadings.AssessApproval)
	}
	s.wireQuestionAssessments()
	dsn := strings.TrimSpace(os.Getenv("AGENTRT_DSN"))
	if dsn == "" {
		slog.Info("agentrt: AGENTRT_DSN is not set; its routes answer 503")
		return
	}
	rt.schema = &schemawatch.Watch{}
	// The routes answer 503 until the database is connected; the platform
	// keeps connecting in the background, so a refusal at start (a new pod
	// the network policy does not know yet, a database restarting) heals by
	// itself instead of leaving the runtime off until the next restart.
	late := &runtimedb.Late{}
	rt.api.DB, rt.api.Back, rt.late = late, late.Back, late
	s.wireThreadInbox(late)
	s.wireWindowNotices(late)
	if s.delivery != nil && role.RunsWorkers() {
		s.delivery.EnableWindowQueueAdvance()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	// STG's card-auto-answer rule answers the runtime's cards within
	// seconds: there the question store takes them past its quotas (TD-393).
	s.questionRuntime.TakeCardsByRule(env == "stg")
	safego.Go("agentrt-connect", func() {
		defer close(done)
		pool := runtimedb.Connect(ctx, dsn, env)
		if pool == nil {
			return
		}
		// The routes open, and /health says agentrt_ready, with the moment
		// they did already recorded: the API's readers count a host's silence
		// from no earlier (review 528 A1b).
		late.Open(pool, time.Now())
		// The seats the token table names that the platform registers
		// itself (coord.EnsureSeats): the coordinator's proposer seat.
		if seats, err := coord.EnsureSeats(ctx, pool, s.auth.Seats()); err != nil {
			slog.Error("agentrt: a configured seat could not be registered", "err", err)
		} else if len(seats) > 0 {
			slog.Info("agentrt: seats registered", "seats", seats)
		}
		// Both processes say whether the database has this binary's
		// migrations (TD-411), in their health and their log.
		rt.schema.Announce = schemawatch.AnnounceMigrations(pool)
		safego.Go("agentrt-schema", func() { rt.schema.Run(ctx, pool) })
		if s.releases != nil {
			s.releases.Service().SetTable(releasetable.Writer(pool))
			s.releases.Service().SetTableReader(releasetable.Rows{Pool: pool})
		}
		s.startBus(ctx, pool, env, role)
		if !role.RunsWorkers() {
			<-ctx.Done()
			pool.Close()
			return
		}
		c := agentRuntimeController(env, pool, s.agentRuntimeSources())
		// The old landing lane is retired (the Owner 2026-11-13): its loops
		// (drill, live claims, cards, grant requests, reconcile, the reading
		// of main for landed results) are not wired any more. Changes reach
		// main through ready/ and the bifrost-land pipeline.
		s.wirePipelineRecords(c)
		s.wireFullCI(c)
		rt.fullCI.Store(c.FullCI)
		wait := c.Start(ctx)
		<-ctx.Done()
		wait()
		pool.Close()
	})
	rt.stop = func() {
		cancel()
		<-done
	}
}

// agentRuntimeController is the platform-workers' controller. Every
// environment runs the same passes.
func agentRuntimeController(env string, db eventlog.DB, src controller.Sources) *controller.Controller {
	return &controller.Controller{DB: db, Sources: src, Env: env, Logf: func(format string, args ...any) {
		slog.Warn("agentrt controller", "detail", fmt.Sprintf(format, args...))
	}, Infof: func(format string, args ...any) {
		slog.Info("agentrt controller", "detail", fmt.Sprintf(format, args...))
	}}
}

// agentRuntimeSources are the server's stores as the controller's sources.
// A nil store is passed as a nil reader, never as a typed nil.
func (s *Server) agentRuntimeSources() controller.Sources {
	var (
		ap  approvalReader
		ci  delivery.CIReader
		rel currentDeployment
	)
	if s.approvals != nil {
		ap = s.approvals
	}
	if s.agentRuntime != nil {
		ci = s.agentRuntime.evidenceCI
	}
	if s.releases != nil && s.releases.Service() != nil {
		rel = s.releases.Service()
	}
	src := agentRuntimeSources(ap, s.questionRuntime.Question(), ci, rel)
	s.wireExternalRecords(&src)
	src.Ack = s.questionRuntime.Ack()
	src.Unacked = s.questionRuntime.Unacked()
	src.Ask = s.questionRuntime.Ask()
	src.Withdraw = s.questionRuntime.Withdraw()
	src.Plan = s.agentRuntimePlanSources()
	return src
}

func (s *Server) mountAgentRuntime(r chi.Router) {
	if s.agentRuntime == nil {
		return
	}
	agentrtapi.Mount(r, s.auth, s.agentRuntime.api)
}

func (s *Server) closeAgentRuntime() {
	if s.agentRuntime != nil && s.agentRuntime.stop != nil {
		s.agentRuntime.stop()
	}
}

// agentRuntimeSchema is the health of the agent runtime's schema.
func (s *Server) agentRuntimeSchema() map[string]any {
	if s.agentRuntime == nil {
		return (*schemawatch.Watch)(nil).Health()
	}
	return s.agentRuntime.schema.Health()
}
