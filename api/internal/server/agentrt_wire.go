package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/exam"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// agentrtDatabase is the state database of each environment (design v5
// §5.1.1); the migrate subcommand keeps the same table.
var agentrtDatabase = map[string]string{
	"prod": "bifrost_platform",
	"stg":  "bifrost_platform_stg",
	"dev":  "bifrost_platform_dev",
}

// agentRuntime is the agent runtime as this process runs it.
type agentRuntime struct {
	api           *agentrtapi.API
	stop          func()
	materialCache store.GrantMaterialCache
	// env is AGENTRT_ENV: whose runtime this platform runs (an approved
	// void_exam_paper names it, agentrt_voidpaper.go).
	env string
	// schema is what the platform last read of the database's agentrt
	// schema (TD-411, agentrt_schema.go); nil without AGENTRT_DSN.
	schema *schemaWatch
	// late is the runtime's database as it connects: its routes are ready
	// once it is (/health says agentrt_ready); nil without AGENTRT_DSN.
	late *lateDB
}

// wireAgentRuntime connects the agent runtime (design v5 §5.2) when
// AGENTRT_DSN is set (a libpq connection string without the password; the
// password comes from AGENTRT_DB_PASSWORD, which the Deployment fills from
// the Secret agentrt-db-runtime): a pool, refused unless it is the runtime role of
// AGENTRT_ENV on that environment's database; the routes; and in the workers
// process the controller loops. Without it the routes answer 503 and nothing
// runs, so the code ships before the database is wired into the Deployment.
//
// The controller reads approvals, questions, CI runs and deployments from the
// platform's own stores (agentRuntimeSources); a record it cannot read leaves
// the rule atoms on it unknown, so nothing is judged done on facts it cannot
// see. Pushed branches are checked on Gitea in the organization
// AGENTRT_GIT_ORG (agentrt_heads.go); without it, persist and salvage of a
// branch answer 503.
//
// With AGENTRT_REDIS_ADDR (redis-agents of the environment; the password in
// AGENTRT_REDIS_PASSWORD, from the Secret redis-agents) the workers process
// also runs the relay: committed outbox rows go to redis-agents as signals,
// and a new or wiped Redis is rebuilt from PostgreSQL first (design v5
// §5.1.4, W-31 2.3). PostgreSQL stays the record; without Redis nothing is
// lost, readers only wait for their next poll.
func (s *Server) wireAgentRuntime(role config.Role) {
	rt := &agentRuntime{api: &agentrtapi.API{Logf: func(format string, args ...any) {
		slog.Warn("agentrt", "detail", fmt.Sprintf(format, args...))
	}, Ask: s.questionRuntime.agentRuntimeAsk(), Anchor: releasepolicy.OwnerKeyFingerprint, Head: s.agentRuntimeHeads(), Commits: s.agentRuntimeCommits(), MainHead: s.agentRuntimeMains(), AgentHeads: s.agentRuntimeAgentHeads(),
		MainResults: s.agentRuntimeMainResults(),
		Exams:       examSetFromEnv(), DiskFloor: diskFloorFromEnv()}, env: strings.TrimSpace(os.Getenv("AGENTRT_ENV"))}
	s.agentRuntime = rt
	rt.api.Grants = s.landingGrantSources
	rt.api.CheckAssessment = s.checkAssessment
	if s.approvals != nil {
		s.approvals.Assessment = s.assessmentReader(store.AssessApproval)
	}
	s.wireQuestionAssessments()
	// Rule signing takes an S5-enrolling landing rule only once this
	// binary's embedded S5 trust root is usable (S5 design §9 order).
	store.S5SigningReady = landingS5Deployable
	dsn := strings.TrimSpace(os.Getenv("AGENTRT_DSN"))
	if dsn == "" {
		slog.Info("agentrt: AGENTRT_DSN is not set; its routes answer 503")
		return
	}
	rt.schema = &schemaWatch{}
	// The routes answer 503 until the database is connected; the platform
	// keeps connecting in the background, so a refusal at start (a new pod
	// the network policy does not know yet, a database restarting) heals by
	// itself instead of leaving the runtime off until the next restart.
	late := &lateDB{}
	rt.api.DB, rt.api.Back, rt.late = late, late.Back, late
	s.wireThreadInbox(late)
	rdb, prefix, rerr := agentrtRedisFromEnv()
	switch {
	case rerr != nil:
		slog.Error("agentrt: redis-agents is configured wrongly; no relay, PostgreSQL alone carries the runtime", "err", rerr)
	case rdb == nil:
		slog.Info("agentrt: AGENTRT_REDIS_ADDR is not set; no relay, PostgreSQL alone carries the runtime")
	}
	if rdb != nil {
		rt.api.Actions = &actions.Store{Redis: rdb, Prefix: prefix}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	// STG's card-auto-answer rule answers the runtime's cards within
	// seconds: there the question store takes them past its quotas (TD-393).
	s.questionRuntime.takeCardsByRule(env == "stg")
	safego.Go("agentrt-connect", func() {
		defer close(done)
		pool := connectAgentRuntime(ctx, func(ctx context.Context) (*pgxpool.Pool, error) {
			return openAgentRuntime(ctx, dsn, env)
		}, agentrtRetry)
		if pool == nil {
			if rdb != nil {
				_ = rdb.Close()
			}
			return
		}
		// The routes open, and /health says agentrt_ready, with the moment
		// they did already recorded: the API's readers count a host's silence
		// from no earlier (review 528 A1b).
		late.open(pool, time.Now())
		// The seats the token table names that the platform registers
		// itself (store.EnsureSeats): the coordinator's proposer seat.
		if seats, err := store.EnsureSeats(ctx, pool, s.auth.Seats()); err != nil {
			slog.Error("agentrt: a configured seat could not be registered", "err", err)
		} else if len(seats) > 0 {
			slog.Info("agentrt: seats registered", "seats", seats)
		}
		// Both processes say whether the database has this binary's
		// migrations (TD-411), in their health and their log.
		safego.Go("agentrt-schema", func() { rt.schema.run(ctx, pool) })
		if !role.RunsWorkers() {
			<-ctx.Done()
			pool.Close()
			if rdb != nil {
				_ = rdb.Close()
			}
			return
		}
		c := agentRuntimeController(env, pool, s.agentRuntimeSources())
		if env == "prod" {
			c.LandingCards = s.questionRuntime.landingCardQuestions()
		}
		s.wireLandingDrill(c)
		if env == "prod" {
			q := c.LandingCards
			repos := map[string]bool{}
			for _, repo := range landing.LandingRepoNames() {
				repos[repo] = c.Live && c.DrillGit[repo] != nil
			}
			h := map[string]any{"repos": repos, "live": c.Live, "cards": map[string]bool{"ask": q.AskRaw != nil, "lookup_key": q.LookupKey != nil,
				"lookup": q.Lookup != nil, "withdraw": q.Withdraw != nil, "ack": q.Ack != nil}}
			if !c.Live {
				h["live_off"] = s.landingLiveOff(c)
			}
			s.landingHealth.Store(h)
		}
		c.PlatformUp, c.PlatformProbe = platformProbe(role, late)
		wait := c.Start(ctx)
		waitSTG := s.startSTGRelease(ctx, c)
		relayDone := make(chan struct{})
		if rdb == nil {
			close(relayDone)
		} else {
			r := &relay.Relay{PG: pool, Redis: rdb, Prefix: prefix}
			safego.Go("agentrt-relay", func() {
				defer close(relayDone)
				slog.Info("agentrt relay: started", "prefix", prefix)
				_ = r.Run(ctx)
			})
		}
		<-ctx.Done()
		wait()
		waitSTG()
		<-relayDone
		pool.Close()
		if rdb != nil {
			_ = rdb.Close()
		}
	})
	rt.stop = func() {
		cancel()
		<-done
	}
}

// agentRuntimeController is the platform-workers' controller for env
// (AGENTRT_ENV). What is STG's alone: the test switch that forces a rework
// (T26), answering cards by rule (TD-390: 「PROD 不变」; no PROD rule file
// names the kind either, review 442 C1), and leaving unassessed the cards
// that rule answers (TD-401's second half: the Owner 2026-10-20 20:0x
// 「PROD 不变」). PROD and DEV assess every card, whatever rule is in force.
func agentRuntimeController(env string, db store.DB, src controller.Sources) *controller.Controller {
	stg := env == "stg"
	return &controller.Controller{DB: db, Sources: cardRuleOnlyOnSTG(env, src), Briefs: true, FailFirstVerify: stg,
		SkipCardsTheRuleAnswers: stg, Env: env, GateRepo: agentrtDeployRepo, PageOwner: diskPage, WatchPresence: true, Logf: func(format string, args ...any) {
			slog.Warn("agentrt controller", "detail", fmt.Sprintf(format, args...))
		}}
}

// cardRuleOnlyOnSTG leaves the controller the card-auto-answer loop's
// sources on STG alone (TD-390 「PROD 不变」, TD-393: every kind, every five
// seconds, still STG's only): elsewhere it reads no cards and answers none.
// The assessments (TD-391) stay: they answer nothing.
func cardRuleOnlyOnSTG(env string, src controller.Sources) controller.Sources {
	if env != "stg" {
		src.Cards, src.RuleAnswer = nil, nil
	}
	return src
}

// examSetFromEnv is the chief exam's question set (W-31 2.4 ④; the exam's
// cut §6 甲), read once at start from the directory AGENTRT_EXAM_DIR names:
// the questions and their grading keys are in no repository, since the agent
// host can check out any of them and an examinee could read them there (the
// coordinating thread 2026-10-19). On K3s the directory is a mounted Secret.
// Unset, or a set that does not load, leaves the exam routes answering 503;
// the log says which, or the loaded set's sha256.
func examSetFromEnv() store.ExamSet {
	dir := strings.TrimSpace(os.Getenv("AGENTRT_EXAM_DIR"))
	if dir == "" {
		slog.Info("agentrt: AGENTRT_EXAM_DIR is not set; the exam routes answer 503")
		return nil
	}
	set, err := loadExamSet(dir)
	if err != nil {
		// Where, never what: a loader's error can quote a rule's pattern,
		// which is a grading key (review 406 B1).
		slog.Error("agentrt: the exam set did not load; the exam routes answer 503", "dir", dir, "where", examLoadWhere(err))
		return nil
	}
	slog.Info("agentrt: exam set loaded", "dir", dir, "sha256", set.Digest())
	return set
}

// examFile reads the file out of an exam loader's error ("exam: q3.json:
// …"); a file name of this shape carries no content of the set.
var examFile = regexp.MustCompile(`^exam: (q[A-Za-z0-9._-]{0,60}\.json): `)

// examLoadWhere is where a set failed to load, without the error's text: the
// file at most, never an item's or a rule's id, which the loader does not
// hold to safe characters (review 409 B1).
func examLoadWhere(err error) string {
	if m := examFile.FindStringSubmatch(err.Error()); m != nil {
		return m[1]
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "the directory or a file in it does not exist"
	}
	return "the set (no file named)"
}

// loadExamSet reads the set in dir (its q*.json files) as the platform gives
// and grades it.
func loadExamSet(dir string) (store.ExamSet, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	s, err := exam.Load(os.DirFS(dir))
	if err != nil {
		return nil, err
	}
	return exam.Platform(s)
}

// agentrtRedisPrefix is a key prefix of the runtime: "agrt:" in PROD and STG
// (each has its own redis-agents), "agrt-dev:" on DEV's shared Redis.
var agentrtRedisPrefix = regexp.MustCompile(`^agrt(-[a-z]+)?:$`)

// agentrtRedisFromEnv is the redis-agents client of AGENTRT_REDIS_ADDR, or nil
// when it is not set. The password is required (redis-agents refuses to start
// without one) and the prefix must be the runtime's, so the relay cannot write
// over another tenant's keys on a shared Redis.
func agentrtRedisFromEnv() (*redis.Client, string, error) {
	addr := strings.TrimSpace(os.Getenv("AGENTRT_REDIS_ADDR"))
	if addr == "" {
		return nil, "", nil
	}
	prefix := strings.TrimSpace(os.Getenv("AGENTRT_REDIS_PREFIX"))
	if prefix == "" {
		prefix = "agrt:"
	}
	if !agentrtRedisPrefix.MatchString(prefix) {
		return nil, "", fmt.Errorf("AGENTRT_REDIS_PREFIX %q is not a runtime prefix (agrt: or agrt-<env>:)", prefix)
	}
	pw := os.Getenv("AGENTRT_REDIS_PASSWORD")
	if pw == "" {
		return nil, "", errors.New("AGENTRT_REDIS_PASSWORD is empty")
	}
	// ContextTimeoutEnabled: a request's deadline bounds the dial, the reads
	// and the writes; without it go-redis keeps its own timeouts and a
	// heartbeat's 2-second budget for actions held 5.25 s against a silent
	// Redis (review 264 A2).
	return redis.NewClient(&redis.Options{Addr: addr, Password: pw, DialTimeout: 5 * time.Second, ContextTimeoutEnabled: true}), prefix, nil
}

// agentrtRetry is how long connectAgentRuntime waits between attempts at
// first, and at most.
var agentrtRetry = [2]time.Duration{2 * time.Second, time.Minute}

// connectAgentRuntime opens until it can (or ctx ends: nil), each attempt
// with the start budget of review 156, waiting a little longer after each
// failure, and saying so in the log once a minute at most.
func connectAgentRuntime(ctx context.Context, open func(context.Context) (*pgxpool.Pool, error), retry [2]time.Duration) *pgxpool.Pool {
	wait, logged := retry[0], time.Time{}
	for attempt := 1; ; attempt++ {
		openCtx, openCancel := context.WithTimeout(ctx, 15*time.Second)
		pool, err := open(openCtx)
		openCancel()
		if err == nil {
			if attempt > 1 {
				slog.Info("agentrt: connected", "attempts", attempt)
			}
			return pool
		}
		if errors.Is(err, errMisconfigured) {
			// Retrying cannot mend the environment, the connection string or
			// the identity: the routes stay 503 until it is fixed and the
			// platform restarted (review 191 B2).
			slog.Error("agentrt: not connected and not retrying: the configuration is wrong; its routes answer 503", "err", err)
			return nil
		}
		if time.Since(logged) >= time.Minute {
			slog.Error("agentrt: not connected yet; its routes answer 503 and the platform keeps trying", "attempt", attempt, "err", err)
			logged = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = min(2*wait, retry[1])
	}
}

// lateDB is the runtime's database before and after it is connected.
type lateDB struct {
	pool atomic.Pointer[pgxpool.Pool]
	back atomic.Pointer[time.Time]
}

// open makes the routes ready on pool, at, the moment recorded first: no
// reader sees them ready without it (review 528 A1b).
func (l *lateDB) open(pool *pgxpool.Pool, at time.Time) {
	l.back.Store(&at)
	l.pool.Store(pool)
}

// Back is when the routes became ready; zero before.
func (l *lateDB) Back() time.Time {
	if t := l.back.Load(); t != nil {
		return *t
	}
	return time.Time{}
}

// errNotConnected is a use of the database before it is connected (the
// routes check Ready first).
var errNotConnected = errors.New("agentrt: the database is not connected yet")

// errMisconfigured is a failure no retry mends: an unknown environment, a
// connection string that does not parse, the wrong role or database.
var errMisconfigured = errors.New("agentrt: misconfigured")

func (l *lateDB) Ready() bool { return l.pool.Load() != nil }

func (l *lateDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	p := l.pool.Load()
	if p == nil {
		return nil, errNotConnected
	}
	return p.BeginTx(ctx, o)
}

// agentrtMaxConns bounds one process's pool. The environment's runtime role
// allows 10 connections on PROD and 5 on STG and DEV (infra db-step
// 2026-10-14-w31-agentrt-1-roles), shared by platform-api and
// platform-workers; pgxpool's default is one per CPU, 16 on the cluster's
// nodes, so the two processes sat at the limit at rest and a restarted one was
// refused ("too many connections for role") for up to a minute on every
// release (TD-392). Two processes at this size leave room under the limit.
func agentrtMaxConns(env string) int32 {
	if env == "prod" {
		return 4
	}
	return 2
}

// openAgentRuntime opens the pool and refuses an identity or a database that
// is not the environment's runtime ones.
func openAgentRuntime(ctx context.Context, dsn, env string) (*pgxpool.Pool, error) {
	if _, err := migrate.RuntimeRole(env); err != nil {
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	if pw := os.Getenv("AGENTRT_DB_PASSWORD"); pw != "" {
		cfg.ConnConfig.Password = pw
	}
	cfg.MaxConns = agentrtMaxConns(env)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var user, db string
	if err = pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(&user, &db); err != nil {
		pool.Close()
		return nil, err
	}
	if err = identityFits(user, db, env); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: %v", errMisconfigured, err)
	}
	return pool, nil
}

// identityFits accepts only the environment's runtime role on the
// environment's database.
func identityFits(user, db, env string) error {
	want, err := migrate.RuntimeRole(env)
	if err != nil {
		return err
	}
	if user != want || db != agentrtDatabase[env] {
		return fmt.Errorf("connected as %s to %s; want %s to %s", user, db, want, agentrtDatabase[env])
	}
	return nil
}

// agentRuntimeSources are the server's stores as the controller's sources.
// A nil store is passed as a nil reader, never as a typed nil.
func (s *Server) agentRuntimeSources() controller.Sources {
	var (
		ap  approvalReader
		ci  ciIndexer
		rel currentDeployment
	)
	if s.approvals != nil {
		ap = s.approvals
	}
	if s.delivery != nil {
		ci = s.delivery.PolicyFacts()
	}
	if s.releases != nil && s.releases.Service() != nil {
		rel = s.releases.Service()
	}
	src := agentRuntimeSources(ap, s.questionRuntime.agentRuntimeQuestion(), ci, rel)
	src.Ack = s.questionRuntime.agentRuntimeAck()
	src.Ask = s.questionRuntime.agentRuntimeAsk()
	src.Withdraw = s.questionRuntime.agentRuntimeWithdraw()
	src.Cards = s.questionRuntime.agentRuntimeCards()
	src.RuleAnswer = s.questionRuntime.agentRuntimeRuleAnswer()
	src.MainResults = s.agentRuntimeMainResults()
	src.Plan = s.agentRuntimePlanSources(src.Deployed)
	src.Assessable = s.agentRuntimeAssessable()
	src.CheckAssessment = s.checkAssessment
	src.Evidence = s.agentRuntimeEvidence()
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

// platformProbe is how the controller of a process asks whether the platform
// takes agentd's calls now, and its name (review 528 A1b): a process that is
// the API itself asks its own routes; platform-workers asks the API through
// its in-cluster Service, AGENTRT_API_HEALTH_URL (by default
// http://platform-api:8780/health, where the checklist prober reaches it),
// whose /health says agentrt_ready once the API's runtime routes are.
func platformProbe(role config.Role, late *lateDB) (func(context.Context) error, string) {
	if role.RunsAPI() {
		return func(context.Context) error {
			if !late.Ready() {
				return errNotConnected
			}
			return nil
		}, "this process's own agentrt routes"
	}
	url := strings.TrimSpace(os.Getenv("AGENTRT_API_HEALTH_URL"))
	if url == "" {
		url = "http://platform-api:8780/health"
	}
	// No connection is kept: each probe is a connection of its own, which
	// the Service sends to any of its endpoints.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	return func(ctx context.Context) error { return serviceTakesCalls(ctx, client, url, probeConnections) }, url
}

// probeConnections is how many probes a pass sends the API's Service at once
// (review 529 A-1, A-2): an instance whose agentrt routes are not ready yet
// still answers /health, and the Service may send a probe to it while
// another instance serves. Any ready answer means the platform takes
// agentd's calls; only a pass whose every probe fails is an outage. With
// several API instances this is probabilistic, not a guarantee: with one
// ready and one unready endpoint, chosen at random for each connection, a
// pass is all unready one time in 32, and each such pass restarts the hosts'
// 30 s. Runs of them can defer a host's judgment; the controller tells a
// deferral past two minutes (store.PresenceDeferral, review 530 A-2), so it
// is never silent. The platform runs one API instance.
const probeConnections = 5

// serviceTakesCalls asks the API's Service n times at once, each probe on a
// connection of its own: nil once one answers that its agentrt routes are
// ready, else why none did (each distinct reason once).
func serviceTakesCalls(ctx context.Context, client *http.Client, url string, n int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	answers := make(chan error, n)
	for range n {
		safego.Go("agentrt-platform-probe", func() {
			err := errors.New("the probe failed")
			defer func() { answers <- err }()
			err = apiTakesCalls(ctx, client, url)
		})
	}
	seen := map[string]bool{}
	var why []string
	for range n {
		err := <-answers
		if err == nil {
			return nil
		}
		if !seen[err.Error()] {
			seen[err.Error()] = true
			why = append(why, err.Error())
		}
	}
	sort.Strings(why)
	return fmt.Errorf("none of %d probes found the API's agentrt routes ready: %s", n, strings.Join(why, "; "))
}

// apiTakesCalls asks the API at url whether its runtime routes are ready:
// nil for yes, else why not (no answer, another status or another body).
func apiTakesCalls(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Ready *bool `json:"agentrt_ready"`
	}
	switch {
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	case json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body) != nil || body.Ready == nil:
		return errors.New("its answer does not say agentrt_ready")
	case !*body.Ready:
		return errors.New("its agentrt routes are not ready")
	}
	return nil
}
