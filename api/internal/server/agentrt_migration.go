package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
)

// run_agentrt_migration (Owner 2026-11-15): the platform runs, on its own
// environment, the agentrt migrations of a commit that environment's
// database has not applied. Until then PROD's migrations were a Job the
// Owner created by hand from an exec card (#493, #554, #558), and the PROD
// deliver pipeline refused a build whose migrations were missing. The Job is
// the one bifrost-trade-infra describes for the environment
// (k8s/agentrt-migrate/<env>/job.yaml at main), with the api image the
// source environment's delivery of that commit built and verified; the
// platform creates it with its own identity, in its own namespace, where it
// may already create Jobs. Tier C: the signed policy covers it for
// migrations that cannot destroy rows (releasepolicy.evaluateMigration);
// anything else is a card the Owner approves on his phone, and the platform
// runs it then.

const (
	migrationActionID = "run_agentrt_migration"
	// Where the platform's repository keeps the migrations it embeds
	// (agentrt/migrations), and where the Job that runs them is described.
	agentrtMigrationsDir = "api/internal/agentrt/migrations"
	agentrtMigrateRepo   = "bifrost-trade-infra"
	agentrtMigrateJobDir = "k8s/agentrt-migrate"
	// The Job's container that runs `platform-api agentrt-migrate`, and the
	// image of agentrtWorkloads it runs.
	agentrtMigrateContainer = "migrate"
	agentrtMigrateImage     = "platform-api"
	// How long the action waits for the Job, and how often it looks. The
	// Job's own deadline (activeDeadlineSeconds) is longer; a Job still
	// running when the wait ends is reported as unknown, not as failed.
	agentrtMigrateWait = 5 * time.Minute
	agentrtMigratePoll = 3 * time.Second
	annMigrateCommit   = "bifrost.io/migrate-commit"
)

var (
	imageDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// The line `platform-api agentrt-migrate` prints when it succeeds.
	migrateLine = regexp.MustCompile(`^agentrt-migrate: env=([a-z]+) database=[a-z_]+ applied=\[([0-9 ]*)\] seeded=(?:true|false)$`)
)

// ownMigrations is what the release policy reads of this platform's own
// environment: the pipeline the release rules say delivers it, the
// repository, and the migrations its database has applied, as /health
// reports them (agentrt_schema_applied). An unread schema is an error, so
// the policy covers nothing then.
type ownMigrations struct{ s *Server }

func (o ownMigrations) Own(context.Context) (releasepolicy.OwnMigrations, error) {
	rt := o.s.agentRuntime
	if rt == nil || rt.schema == nil {
		return releasepolicy.OwnMigrations{}, errors.New("the agent runtime's database is not wired on this platform")
	}
	applied, ok := rt.schema.applied()
	if !ok {
		return releasepolicy.OwnMigrations{}, errors.New("the database's applied migrations have not been read (agentrt_schema is unknown)")
	}
	if o.s.releases == nil || o.s.releases.Service() == nil {
		return releasepolicy.OwnMigrations{}, errors.New("the release records are not wired")
	}
	pipeline, ok := o.s.releases.Service().PipelineOf(agentrtDeployLane, rt.env)
	if !ok {
		return releasepolicy.OwnMigrations{}, fmt.Errorf("no release rule names the pipeline that delivers %s/%s", agentrtDeployLane, rt.env)
	}
	return releasepolicy.OwnMigrations{Pipeline: pipeline, Repo: agentrtDeployRepo, Dir: agentrtMigrationsDir, Applied: applied}, nil
}

// applied is the migrations the database had applied at the last read
// (number -> sha256); ok is false when no read has succeeded or the last
// one failed.
func (w *schemaWatch) applied() (map[int]string, bool) {
	if w == nil {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == nil || w.err != "" {
		return nil, false
	}
	out := make(map[int]string, len(w.status.Applied))
	for _, a := range w.status.Applied {
		out[a.ID] = a.SHA256
	}
	return out, true
}

// migrationRunner is the action with its sources, so a test replaces each.
type migrationRunner struct {
	env       string
	namespace string // the platform's own namespace
	// state compares a commit's migrations with the applied ones; source is
	// the pipeline the signed policy pins a pipeline to.
	state    func(ctx context.Context, commit string) (releasepolicy.MigrationState, error)
	source   func(ctx context.Context, pipeline string) (string, bool)
	records  func(ctx context.Context) ([]releases.Record, error)
	mainHead func(ctx context.Context, repo string) (string, error)
	fileAt   func(ctx context.Context, repo, ref, path string) (string, bool, error)
	kube     func() (kubernetes.Interface, error)
	// podLog is the migrate container's log of one pod.
	podLog     func(ctx context.Context, cs kubernetes.Interface, namespace, pod string) (string, error)
	wait, poll time.Duration
}

func (s *Server) migrationRunner() (*migrationRunner, error) {
	if s.releasePolicy == nil || s.delivery == nil || s.cluster == nil || s.releases == nil || s.releases.Service() == nil || s.agentRuntime == nil {
		return nil, errors.New("the release policy, delivery, cluster or agent runtime is not wired on this platform")
	}
	facts := s.delivery.PolicyFacts()
	return &migrationRunner{
		env: s.agentRuntime.env, namespace: k8sstate.Namespace(),
		state: s.releasePolicy.PendingMigrations, source: s.releasePolicy.SourcePipeline,
		records: s.releases.Service().List, mainHead: facts.MainHead, fileAt: facts.FileAt,
		kube: func() (kubernetes.Interface, error) {
			cs, _, err := s.cluster.Service().KubernetesClient()
			return cs, err
		},
		podLog: func(ctx context.Context, cs kubernetes.Interface, namespace, pod string) (string, error) {
			lines, limit := int64(60), int64(16<<10)
			raw, err := cs.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
				Container: agentrtMigrateContainer, TailLines: &lines, LimitBytes: &limit,
			}).DoRaw(ctx)
			return string(raw), err
		},
		wait: agentrtMigrateWait, poll: agentrtMigratePoll,
	}, nil
}

// sourceDigest is the api image digest the newest successful delivery of
// commit by the source pipeline built: the records are newest first, and the
// recorder writes one only for a run that succeeded.
func sourceDigest(recs []releases.Record, from, repo, commit string) (string, error) {
	for _, r := range recs {
		if r.Pipeline != from || r.Repos[repo].SHA != commit {
			continue
		}
		if d := r.Images[agentrtMigrateImage]; imageDigest.MatchString(d) {
			return d, nil
		}
		return "", fmt.Errorf("the record of %s for %s (run %s) names no %s image digest", from, commit[:12], r.Run, agentrtMigrateImage)
	}
	return "", fmt.Errorf("no successful delivery of %s by %s is on record: deliver the commit there first", commit[:12], from)
}

// migrationJob is the environment's Job manifest with the migrate container
// running the api image at digest. It refuses a manifest that is not one Job
// with a generated name in the platform's own namespace, or whose migrate
// container is not there exactly once or does not name the environment.
func migrationJob(manifest, namespace, env, digest, commit string, identity map[string]string) (*batchv1.Job, error) {
	var job batchv1.Job
	if err := yaml.UnmarshalStrict([]byte(manifest), &job); err != nil {
		return nil, fmt.Errorf("the manifest does not read as a Job: %w", err)
	}
	switch {
	case job.Kind != "Job" || job.APIVersion != "batch/v1":
		return nil, fmt.Errorf("the manifest is a %s %s, not a batch/v1 Job", job.APIVersion, job.Kind)
	case job.GenerateName == "" || job.Name != "":
		return nil, errors.New("the manifest's Job has no generateName, or has a fixed name")
	case namespace == "" || job.Namespace != namespace:
		return nil, fmt.Errorf("the manifest's Job is for namespace %q and this platform runs in %q", job.Namespace, namespace)
	}
	found := 0
	for i := range job.Spec.Template.Spec.Containers {
		c := &job.Spec.Template.Spec.Containers[i]
		if c.Name != agentrtMigrateContainer {
			continue
		}
		found++
		named := false
		for _, e := range c.Env {
			named = named || (e.Name == "AGENTRT_ENV" && e.Value == env)
		}
		if !named {
			return nil, fmt.Errorf("the manifest's %s container does not set AGENTRT_ENV=%s", agentrtMigrateContainer, env)
		}
		// The repository of the manifest's image, at the digest: a tag is
		// whatever was pushed last, the digest is the build that was verified.
		repo := c.Image
		if at := strings.LastIndex(repo, "@"); at >= 0 {
			repo = repo[:at]
		} else if colon := strings.LastIndex(repo, ":"); colon > strings.LastIndex(repo, "/") {
			repo = repo[:colon]
		}
		if repo == "" {
			return nil, fmt.Errorf("the manifest's %s container names no image", agentrtMigrateContainer)
		}
		c.Image = repo + "@" + digest
	}
	if found != 1 {
		return nil, fmt.Errorf("the manifest has %d containers named %s, not one", found, agentrtMigrateContainer)
	}
	if job.Annotations == nil {
		job.Annotations = map[string]string{}
	}
	job.Annotations[annMigrateCommit] = commit
	for k, v := range identity {
		job.Annotations[k] = v
	}
	return &job, nil
}

// run does what the action says. It returns {already_current: true} and
// creates nothing when the environment has applied every migration of commit.
func (m *migrationRunner) run(ctx context.Context, commit string) (map[string]any, error) {
	if !commitID.MatchString(commit) {
		return nil, errors.New("commit is a 40-character lowercase SHA")
	}
	// The caller's context ends with its connection (the Owner's phone, an
	// agent's request); the Job does not, so the wait and the reading of its
	// log go on without it, bounded here.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.wait+time.Minute)
	defer cancel()
	state, err := m.state(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(state.Pending) == 0 {
		return map[string]any{"env": m.env, "commit": commit, "already_current": true, "applied": []int{}, "job": ""}, nil
	}
	if len(state.Extra) > 0 {
		return nil, fmt.Errorf("this environment has applied migrations %v, which %s does not carry: the migrator refuses an older build", state.Extra, commit[:12])
	}
	from, ok := m.source(ctx, state.Own.Pipeline)
	if !ok {
		return nil, fmt.Errorf("no signed release policy pins %s to a source pipeline, so nothing says whose build of %s to run", state.Own.Pipeline, commit[:12])
	}
	recs, err := m.records(ctx)
	if err != nil {
		return nil, actions.Transient("release records: " + err.Error())
	}
	digest, err := sourceDigest(recs, from, state.Own.Repo, commit)
	if err != nil {
		return nil, err
	}
	path := agentrtMigrateJobDir + "/" + m.env + "/job.yaml"
	head, err := m.mainHead(ctx, agentrtMigrateRepo)
	if err != nil {
		return nil, actions.Transient("main of " + agentrtMigrateRepo + ": " + err.Error())
	}
	manifest, found, err := m.fileAt(ctx, agentrtMigrateRepo, head, path)
	if err != nil {
		return nil, actions.Transient(path + " at " + head[:12] + ": " + err.Error())
	}
	if !found {
		return nil, fmt.Errorf("%s has no %s at main (%s): this environment has no migration Job", agentrtMigrateRepo, path, head[:12])
	}
	job, err := migrationJob(manifest, m.namespace, m.env, digest, commit, actions.IdentityAnnotations(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", path, head[:12], err)
	}
	cs, err := m.kube()
	if err != nil {
		return nil, actions.Transient("cluster client: " + err.Error())
	}
	created, err := cs.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, actions.ClassifyCreate(err)
	}
	out := map[string]any{
		"env": m.env, "commit": commit, "already_current": false, "namespace": created.Namespace, "job": created.Name,
		"manifest": agentrtMigrateRepo + "@" + head[:12] + ":" + path, "pending": state.Pending,
	}
	for i := range job.Spec.Template.Spec.Containers {
		if c := job.Spec.Template.Spec.Containers[i]; c.Name == agentrtMigrateContainer {
			out["image"] = c.Image
		}
	}
	done, failed, err := m.await(ctx, cs, created.Namespace, created.Name)
	log := m.jobLog(ctx, cs, created.Namespace, created.Name)
	switch {
	case err != nil || !done:
		// The Job exists and may still apply: the outcome has to be read
		// from it, and another one must not be started on a guess.
		why := "still running after " + m.wait.String()
		if err != nil {
			why = "could not be read: " + err.Error()
		}
		return nil, actions.Uncertain(fmt.Sprintf("job %s/%s %s; its log will say what it applied", created.Namespace, created.Name, why))
	case failed:
		return nil, fmt.Errorf("job %s/%s failed; every migration file runs in one transaction, so a file that failed changed nothing. Log tail:\n%s",
			created.Namespace, created.Name, tail(log, 2000))
	}
	applied, found := migrationApplied(log, m.env)
	if !found {
		return nil, actions.Uncertain(fmt.Sprintf("job %s/%s completed, and its log does not carry the agentrt-migrate line for %s. Log tail:\n%s",
			created.Namespace, created.Name, m.env, tail(log, 2000)))
	}
	out["applied"] = applied
	return out, nil
}

// await polls the Job until it is Complete or Failed, or the wait ends.
func (m *migrationRunner) await(ctx context.Context, cs kubernetes.Interface, namespace, name string) (done, failed bool, err error) {
	deadline := time.Now().Add(m.wait)
	for {
		job, gerr := cs.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if gerr == nil {
			for _, c := range job.Status.Conditions {
				if c.Status != corev1.ConditionTrue {
					continue
				}
				switch c.Type {
				case batchv1.JobComplete:
					return true, false, nil
				case batchv1.JobFailed:
					return true, true, nil
				}
			}
		}
		if !time.Now().Before(deadline) {
			return false, false, gerr
		}
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case <-time.After(m.poll):
		}
	}
}

// jobLog is the migrate container's log of the Job's pods, newest last; a
// log that cannot be read is said in its place.
func (m *migrationRunner) jobLog(ctx context.Context, cs kubernetes.Interface, namespace, job string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job})
	if err != nil {
		return "(the Job's pods could not be listed: " + err.Error() + ")"
	}
	var b strings.Builder
	for _, p := range pods.Items {
		text, err := m.podLog(ctx, cs, namespace, p.Name)
		if err != nil {
			text = "(the log of " + p.Name + " could not be read: " + err.Error() + ")"
		}
		b.WriteString(strings.TrimRight(text, "\n") + "\n")
	}
	if b.Len() == 0 {
		return "(the Job has no pod)"
	}
	return b.String()
}

// migrationApplied reads the migrator's success line for env from a log:
// the numbers it applied, and whether the line is there at all.
func migrationApplied(log, env string) (applied []int, found bool) {
	for _, line := range strings.Split(log, "\n") {
		m := migrateLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[1] != env {
			continue
		}
		applied = []int{}
		for _, f := range strings.Fields(m[2]) {
			n, _ := strconv.Atoi(f)
			applied = append(applied, n)
		}
		return applied, true
	}
	return nil, false
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

// runAgentrtMigration is the action's executor: after the Owner approved the
// card, and behind the direct route when the signed policy covers the call.
func (s *Server) runAgentrtMigration(ctx context.Context, params map[string]any) (any, error) {
	m, err := s.migrationRunner()
	if err != nil {
		return nil, err
	}
	return m.run(ctx, text(params["commit"]))
}

// handleAgentrtMigration is POST /api/v1/agentrt/migrations/run {commit}
// (operator, tier C: the guard lets it through only on the signed policy or
// from the approval executor). It answers when the Job has finished.
func (s *Server) handleAgentrtMigration(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is {commit}"})
		return
	}
	commit := strings.TrimSpace(body.Commit)
	if !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit is a 40-character lowercase SHA"})
		return
	}
	m, err := s.migrationRunner()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	out, err := m.run(r.Context(), commit)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, out)
	case actions.IsUncertain(err):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": err.Error()})
	case actions.IsTransient(err):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	}
}
