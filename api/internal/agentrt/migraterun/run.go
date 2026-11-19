// Package migraterun runs the agentrt migrations of a commit on the platform's
// own environment (the action run_agentrt_migration): it creates the Job the
// environment's manifest describes, with the api image the source environment
// delivered, and reads from its log what the Job applied.
package migraterun

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deploysource"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
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
	// Where the platform's repository keeps the migrations it embeds
	// (agentrt/migrations), and where the Job that runs them is described.
	MigrationsDir = "api/internal/agentrt/migrations"
	migrateRepo   = "bifrost-trade-infra"
	migrateJobDir = "k8s/agentrt-migrate"
	// The Job's container that runs `platform-api agentrt-migrate`, and the
	// image of deploysource's workloads it runs.
	migrateContainer = "migrate"
	migrateImage     = "platform-api"
	// DefaultWait and DefaultPoll: how long the action waits for the Job, and how
	// often it looks. The Job's own deadline (activeDeadlineSeconds) is longer; a
	// Job still running when the wait ends is reported as unknown, not as failed.
	DefaultWait      = 5 * time.Minute
	DefaultPoll      = 3 * time.Second
	annMigrateCommit = "bifrost.io/migrate-commit"
)

var (
	imageDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// The line `platform-api agentrt-migrate` prints when it succeeds.
	migrateLine = regexp.MustCompile(`^agentrt-migrate: env=([a-z]+) database=[a-z_]+ applied=\[([0-9 ]*)\] seeded=(?:true|false)$`)
)

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Errors are how the action says what went wrong: the owner domain's
// classification of an error as transient (try again), uncertain (it may have
// happened) or the Job's creation failing, and the identity of the caller
// recorded on the Job.
type Errors struct {
	Transient      func(reason string) error
	Uncertain      func(reason string) error
	ClassifyCreate func(err error) error
	Identity       func(ctx context.Context) map[string]string
}

// PodLog reads the migrate container's log of one pod.
func PodLog(ctx context.Context, cs kubernetes.Interface, namespace, pod string) (string, error) {
	lines, limit := int64(60), int64(16<<10)
	raw, err := cs.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: migrateContainer, TailLines: &lines, LimitBytes: &limit,
	}).DoRaw(ctx)
	return string(raw), err
}

// OwnSource is what the release policy reads of this platform's own
// environment, from what the server has wired: the pipeline the release rules
// say delivers it, the repository, and the migrations its database has applied
// (agentrt_schema_applied in /health). An unread schema is an error, so the
// policy covers nothing then.
type OwnSource struct {
	// Applied is the migrations the database had applied at the last read;
	// wired is false without the agent runtime, read false when no read has
	// succeeded or the last one failed.
	Applied func() (applied map[int]string, read, wired bool)
	// Env is whose runtime this platform runs.
	Env func() string
	// Pipeline is the pipeline delivering lane in env; the error says the
	// release records are not wired.
	Pipeline func(lane, env string) (string, bool, error)
}

// Own is what the release policy reads (releasepolicy.OwnMigrations).
func (o OwnSource) Own(context.Context) (releasepolicy.OwnMigrations, error) {
	applied, read, wired := o.Applied()
	if !wired {
		return releasepolicy.OwnMigrations{}, errors.New("the agent runtime's database is not wired on this platform")
	}
	if !read {
		return releasepolicy.OwnMigrations{}, errors.New("the database's applied migrations have not been read (agentrt_schema is unknown)")
	}
	pipeline, ok, err := o.Pipeline(deploysource.Lane, o.Env())
	if err != nil {
		return releasepolicy.OwnMigrations{}, err
	}
	if !ok {
		return releasepolicy.OwnMigrations{}, fmt.Errorf("no release rule names the pipeline that delivers %s/%s", deploysource.Lane, o.Env())
	}
	return releasepolicy.OwnMigrations{Pipeline: pipeline, Repo: deploysource.Repo, Dir: MigrationsDir, Applied: applied}, nil
}

// Runner is the action with its sources, so a test replaces each.
type Runner struct {
	Env       string
	Namespace string // the platform's own namespace
	// state compares a commit's migrations with the applied ones; source is
	// the pipeline the signed policy pins a pipeline to.
	State    func(ctx context.Context, commit string) (releasepolicy.MigrationState, error)
	Source   func(ctx context.Context, pipeline string) (string, bool)
	Records  func(ctx context.Context) ([]releases.Record, error)
	MainHead func(ctx context.Context, repo string) (string, error)
	FileAt   func(ctx context.Context, repo, ref, path string) (string, bool, error)
	Kube     func() (kubernetes.Interface, error)
	// PodLog is the migrate container's log of one pod.
	PodLog     func(ctx context.Context, cs kubernetes.Interface, namespace, pod string) (string, error)
	Wait, Poll time.Duration
	// Errors classify what the action fails with (actions.Transient and the
	// others); the owner domain owns those, so they are handed in.
	Errors Errors
}

// sourceDigest is the api image digest the newest successful delivery of
// commit by the source pipeline built: the records are newest first, and the
// recorder writes one only for a run that succeeded.
func sourceDigest(recs []releases.Record, from, repo, commit string) (string, error) {
	for _, r := range recs {
		if r.Pipeline != from || r.Repos[repo].SHA != commit {
			continue
		}
		if d := r.Images[migrateImage]; imageDigest.MatchString(d) {
			return d, nil
		}
		return "", fmt.Errorf("the record of %s for %s (run %s) names no %s image digest", from, commit[:12], r.Run, migrateImage)
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
		if c.Name != migrateContainer {
			continue
		}
		found++
		named := false
		for _, e := range c.Env {
			named = named || (e.Name == "AGENTRT_ENV" && e.Value == env)
		}
		if !named {
			return nil, fmt.Errorf("the manifest's %s container does not set AGENTRT_ENV=%s", migrateContainer, env)
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
			return nil, fmt.Errorf("the manifest's %s container names no image", migrateContainer)
		}
		c.Image = repo + "@" + digest
	}
	if found != 1 {
		return nil, fmt.Errorf("the manifest has %d containers named %s, not one", found, migrateContainer)
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

// Run does what the action says. It returns {already_current: true} and
// creates nothing when the environment has applied every migration of commit.
func (m *Runner) Run(ctx context.Context, commit string) (map[string]any, error) {
	if !commitID.MatchString(commit) {
		return nil, errors.New("commit is a 40-character lowercase SHA")
	}
	// The caller's context ends with its connection (the Owner's phone, an
	// agent's request); the Job does not, so the wait and the reading of its
	// log go on without it, bounded here.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.Wait+time.Minute)
	defer cancel()
	state, err := m.State(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(state.Pending) == 0 {
		return map[string]any{"env": m.Env, "commit": commit, "already_current": true, "applied": []int{}, "job": ""}, nil
	}
	if len(state.Extra) > 0 {
		return nil, fmt.Errorf("this environment has applied migrations %v, which %s does not carry: the migrator refuses an older build", state.Extra, commit[:12])
	}
	from, ok := m.Source(ctx, state.Own.Pipeline)
	if !ok {
		return nil, fmt.Errorf("no signed release policy pins %s to a source pipeline, so nothing says whose build of %s to run", state.Own.Pipeline, commit[:12])
	}
	recs, err := m.Records(ctx)
	if err != nil {
		return nil, m.Errors.Transient("release records: " + err.Error())
	}
	digest, err := sourceDigest(recs, from, state.Own.Repo, commit)
	if err != nil {
		return nil, err
	}
	path := migrateJobDir + "/" + m.Env + "/job.yaml"
	head, err := m.MainHead(ctx, migrateRepo)
	if err != nil {
		return nil, m.Errors.Transient("main of " + migrateRepo + ": " + err.Error())
	}
	manifest, found, err := m.FileAt(ctx, migrateRepo, head, path)
	if err != nil {
		return nil, m.Errors.Transient(path + " at " + head[:12] + ": " + err.Error())
	}
	if !found {
		return nil, fmt.Errorf("%s has no %s at main (%s): this environment has no migration Job", migrateRepo, path, head[:12])
	}
	job, err := migrationJob(manifest, m.Namespace, m.Env, digest, commit, m.Errors.Identity(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s at %s: %w", path, head[:12], err)
	}
	cs, err := m.Kube()
	if err != nil {
		return nil, m.Errors.Transient("cluster client: " + err.Error())
	}
	created, err := cs.BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, m.Errors.ClassifyCreate(err)
	}
	out := map[string]any{
		"env": m.Env, "commit": commit, "already_current": false, "namespace": created.Namespace, "job": created.Name,
		"manifest": migrateRepo + "@" + head[:12] + ":" + path, "pending": state.Pending,
	}
	for i := range job.Spec.Template.Spec.Containers {
		if c := job.Spec.Template.Spec.Containers[i]; c.Name == migrateContainer {
			out["image"] = c.Image
		}
	}
	done, failed, err := m.await(ctx, cs, created.Namespace, created.Name)
	log := m.jobLog(ctx, cs, created.Namespace, created.Name)
	switch {
	case err != nil || !done:
		// The Job exists and may still apply: the outcome has to be read
		// from it, and another one must not be started on a guess.
		why := "still running after " + m.Wait.String()
		if err != nil {
			why = "could not be read: " + err.Error()
		}
		return nil, m.Errors.Uncertain(fmt.Sprintf("job %s/%s %s; its log will say what it applied", created.Namespace, created.Name, why))
	case failed:
		return nil, fmt.Errorf("job %s/%s failed; every migration file runs in one transaction, so a file that failed changed nothing. Log tail:\n%s",
			created.Namespace, created.Name, tail(log, 2000))
	}
	applied, found := migrationApplied(log, m.Env)
	if !found {
		return nil, m.Errors.Uncertain(fmt.Sprintf("job %s/%s completed, and its log does not carry the agentrt-migrate line for %s. Log tail:\n%s",
			created.Namespace, created.Name, m.Env, tail(log, 2000)))
	}
	out["applied"] = applied
	return out, nil
}

// await polls the Job until it is Complete or Failed, or the wait ends.
func (m *Runner) await(ctx context.Context, cs kubernetes.Interface, namespace, name string) (done, failed bool, err error) {
	deadline := time.Now().Add(m.Wait)
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
		case <-time.After(m.Poll):
		}
	}
}

// jobLog is the migrate container's log of the Job's pods, newest last; a
// log that cannot be read is said in its place.
func (m *Runner) jobLog(ctx context.Context, cs kubernetes.Interface, namespace, job string) string {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	pods, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job})
	if err != nil {
		return "(the Job's pods could not be listed: " + err.Error() + ")"
	}
	var b strings.Builder
	for _, p := range pods.Items {
		text, err := m.PodLog(ctx, cs, namespace, p.Name)
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
