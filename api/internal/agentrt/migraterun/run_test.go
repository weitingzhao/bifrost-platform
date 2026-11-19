package migraterun

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

const (
	migCommit = "1111111111111111111111111111111111111111"
	migHead   = "2222222222222222222222222222222222222222"
	migDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	migNS     = "platform-prod-ns"
	// The shape of bifrost-trade-infra k8s/agentrt-migrate/prod/job.yaml.
	migManifest = `# Owner-created migration Job
apiVersion: batch/v1
kind: Job
metadata:
  generateName: agentrt-migrate-
  namespace: platform-prod-ns
  labels:
    app.kubernetes.io/name: agentrt-migrate
spec:
  backoffLimit: 0
  activeDeadlineSeconds: 900
  template:
    spec:
      restartPolicy: Never
      initContainers:
        - name: wait-pg
          image: example.org/postgresql:17.9
      containers:
        - name: migrate
          image: registry.example:30500/the-api:prod
          command: [/usr/local/bin/platform-api]
          args: [agentrt-migrate]
          env:
            - name: AGENTRT_ENV
              value: prod
`
)

// migrationFixture is the runner on a fake cluster whose Job finishes as
// finish says, with one pending migration and a successful source delivery.
type migrationFixture struct {
	m       *Runner
	cs      *fake.Clientset
	state   releasepolicy.MigrationState
	records []releases.Record
	finish  batchv1.JobConditionType // "" leaves the Job running
	log     string
	created []*batchv1.Job
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	f := &migrationFixture{
		cs: fake.NewSimpleClientset(),
		state: releasepolicy.MigrationState{
			Own:     releasepolicy.OwnMigrations{Pipeline: "deliver-prod", Repo: "the-repo", Dir: "migrations", Applied: map[int]string{1: "x"}},
			Pending: []string{"migrations/0002_notes.sql"},
		},
		records: []releases.Record{
			{Run: "stg-newer", Pipeline: "deliver-stg", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: "3333333333333333333333333333333333333333"}}},
			{Run: "stg-run", Pipeline: "deliver-stg", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: migCommit}}, Images: map[string]string{"platform-api": migDigest}},
			{Run: "prod-run", Pipeline: "deliver-prod", Repos: map[string]releases.RepoBuild{"the-repo": {SHA: migCommit}}, Images: map[string]string{"platform-api": "sha256:" + strings.Repeat("b", 64)}},
		},
		finish: batchv1.JobComplete,
		log:    "ready\nagentrt-migrate: env=prod database=bifrost_platform applied=[2] seeded=false\n",
	}
	// The fake keeps generateName as it is; name the Job as the API server
	// would, and finish it as the fixture says.
	f.cs.PrependReactor("create", "jobs", func(a k8stesting.Action) (bool, runtime.Object, error) {
		job := a.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		f.created = append(f.created, job.DeepCopy())
		job.Name = job.GenerateName + "abcde"
		if f.finish != "" {
			job.Status.Conditions = []batchv1.JobCondition{{Type: f.finish, Status: corev1.ConditionTrue}}
		}
		return false, nil, nil
	})
	if _, err := f.cs.CoreV1().Pods(migNS).Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "agentrt-migrate-abcde-x1", Namespace: migNS, Labels: map[string]string{"job-name": "agentrt-migrate-abcde"},
	}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.m = &Runner{
		Env: "prod", Namespace: migNS,
		State: func(context.Context, string) (releasepolicy.MigrationState, error) { return f.state, nil },
		Source: func(_ context.Context, pipeline string) (string, bool) {
			return "deliver-stg", pipeline == "deliver-prod"
		},
		Records:  func(context.Context) ([]releases.Record, error) { return f.records, nil },
		MainHead: func(context.Context, string) (string, error) { return migHead, nil },
		FileAt: func(_ context.Context, repo, ref, path string) (string, bool, error) {
			if repo != migrateRepo || ref != migHead || path != "k8s/agentrt-migrate/prod/job.yaml" {
				return "", false, nil
			}
			return migManifest, true, nil
		},
		Kube: func() (kubernetes.Interface, error) { return f.cs, nil },
		PodLog: func(_ context.Context, _ kubernetes.Interface, namespace, pod string) (string, error) {
			if namespace != migNS || pod != "agentrt-migrate-abcde-x1" {
				return "", errors.New("no such pod")
			}
			return f.log, nil
		},
		Wait: 200 * time.Millisecond, Poll: 10 * time.Millisecond,
		Errors: testErrors,
	}
	return f
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// The Job created is the environment's manifest with one change, the image:
// the api build the source environment's successful delivery of the commit
// verified, by digest; and the answer names the Job and what it applied.
func TestMigrationRunnerCreatesTheJobAndReadsWhatItApplied(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.WithValue(context.Background(), approvalKey{}, "apr_1")
	out, err := f.m.Run(ctx, migCommit)
	if err != nil {
		t.Fatal(err)
	}
	if out["job"] != "agentrt-migrate-abcde" || out["namespace"] != migNS || out["already_current"] != false ||
		!slices.Equal(out["applied"].([]int), []int{2}) || out["image"] != "registry.example:30500/the-api@"+migDigest {
		t.Fatalf("answer = %v", out)
	}
	if len(f.created) != 1 {
		t.Fatalf("created %d jobs", len(f.created))
	}
	job := f.created[0]
	c := job.Spec.Template.Spec.Containers
	if job.GenerateName != "agentrt-migrate-" || job.Name != "" || job.Namespace != migNS || len(c) != 1 ||
		c[0].Image != "registry.example:30500/the-api@"+migDigest || !slices.Equal(c[0].Args, []string{"agentrt-migrate"}) ||
		job.Spec.Template.Spec.InitContainers[0].Image != "example.org/postgresql:17.9" || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("job = %+v", job)
	}
	if job.Annotations[annMigrateCommit] != migCommit || job.Annotations["approval"] != "apr_1" {
		t.Fatalf("annotations = %v", job.Annotations)
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// A database that is current for the commit gets no Job, and the answer says so.
func TestMigrationRunnerDoesNothingWhenCurrent(t *testing.T) {
	f := newMigrationFixture(t)
	f.state.Pending = nil
	out, err := f.m.Run(context.Background(), migCommit)
	if err != nil || out["already_current"] != true || out["job"] != "" || len(f.created) != 0 {
		t.Fatalf("answer = %v, %v; created %d", out, err, len(f.created))
	}
}

// Owner 2026-11-15: a migration that cannot destroy rows reaches PROD without
// a card; the platform runs it (cards #493 #554 #558 were Owner-run Jobs).
// Each case is the run above with one thing wrong. A refusal before the Job
// creates nothing; a Job that fails is an error with its log; a Job whose
// end was not seen is unknown, never retried under another name.
func TestMigrationRunnerRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		edit      func(f *migrationFixture)
		commit    string
		want      string
		created   int
		uncertain bool
	}{
		"not a commit id": {commit: "main", want: "40-character"},
		"the comparison fails": {edit: func(f *migrationFixture) {
			f.m.State = func(context.Context, string) (releasepolicy.MigrationState, error) {
				return releasepolicy.MigrationState{}, errors.New("0001_core.sql ran on this environment with other bytes")
			}
		}, want: "other bytes"},
		"the environment is ahead of the commit": {edit: func(f *migrationFixture) { f.state.Extra = []int{9} },
			want: "has applied migrations [9]"},
		"no policy pins the pipeline": {edit: func(f *migrationFixture) {
			f.m.Source = func(context.Context, string) (string, bool) { return "", false }
		}, want: "no signed release policy pins deliver-prod"},
		"the commit was never delivered to the source": {edit: func(f *migrationFixture) { f.records = f.records[:1] },
			want: "no successful delivery of 111111111111 by deliver-stg"},
		"only this environment's own pipeline built the commit": {edit: func(f *migrationFixture) { f.records = f.records[2:] },
			want: "no successful delivery"},
		"the source record names no api digest": {edit: func(f *migrationFixture) { f.records[1].Images = map[string]string{"platform-console": migDigest} },
			want: "names no platform-api image digest"},
		"no manifest directory for this environment": {edit: func(f *migrationFixture) { f.m.Env = "dev" },
			want: "has no k8s/agentrt-migrate/dev/job.yaml at main"},
		"the manifest is for another namespace": {edit: func(f *migrationFixture) { f.m.Namespace = "platform-stg-ns" },
			want: `is for namespace "platform-prod-ns" and this platform runs in "platform-stg-ns"`},
		"the platform does not know its namespace": {edit: func(f *migrationFixture) { f.m.Namespace = "" },
			want: "this platform runs in"},
		"the manifest is for another environment": {edit: func(f *migrationFixture) {
			f.m.FileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "value: prod", "value: stg", 1), true, nil
			}
		}, want: "does not set AGENTRT_ENV=prod"},
		"the manifest has no migrate container": {edit: func(f *migrationFixture) {
			f.m.FileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "- name: migrate", "- name: other", 1), true, nil
			}
		}, want: "0 containers named migrate"},
		"the manifest has a fixed name": {edit: func(f *migrationFixture) {
			f.m.FileAt = func(context.Context, string, string, string) (string, bool, error) {
				return strings.Replace(migManifest, "generateName: agentrt-migrate-", "name: agentrt-migrate", 1), true, nil
			}
		}, want: "no generateName"},
		"the manifest is not a Job": {edit: func(f *migrationFixture) {
			f.m.FileAt = func(context.Context, string, string, string) (string, bool, error) {
				return "apiVersion: v1\nkind: Pod\nmetadata:\n  generateName: x-\n  namespace: platform-prod-ns\n", true, nil
			}
		}, want: "not a batch/v1 Job"},
		"the Job fails": {edit: func(f *migrationFixture) {
			f.finish, f.log = batchv1.JobFailed, "agentrt-migrate: migrate: 0002_notes.sql: ERROR: agentrt.note has 1 row(s)\n"
		}, want: "failed; every migration file runs in one transaction, so a file that failed changed nothing. Log tail:\nagentrt-migrate: migrate: 0002_notes.sql: ERROR: agentrt.note has 1 row(s)", created: 1},
		"the Job is still running when the wait ends": {edit: func(f *migrationFixture) { f.finish = "" },
			want: "still running after 200ms", created: 1, uncertain: true},
		"the Job completes without the migrator's line": {edit: func(f *migrationFixture) { f.log = "ready\n" },
			want: "does not carry the agentrt-migrate line for prod", created: 1, uncertain: true},
		"the line is another environment's": {edit: func(f *migrationFixture) {
			f.log = "agentrt-migrate: env=stg database=bifrost_platform_stg applied=[2] seeded=false\n"
		}, want: "does not carry the agentrt-migrate line for prod", created: 1, uncertain: true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMigrationFixture(t)
			if c.edit != nil {
				c.edit(f)
			}
			commit := c.commit
			if commit == "" {
				commit = migCommit
			}
			out, err := f.m.Run(context.Background(), commit)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("answer = %v, error = %v, want %q", out, err, c.want)
			}
			if len(f.created) != c.created || isUncertain(err) != c.uncertain {
				t.Fatalf("created %d jobs (want %d), uncertain = %v (want %v): %v", len(f.created), c.created, isUncertain(err), c.uncertain, err)
			}
		})
	}
}

// Owner 2026-11-15: what the policy reads of this platform is what /health
// reports; an unwired runtime, an unread schema and unwired release records
// are each an error, and a read one names the pipeline, repository, directory
// and applied migrations.
func TestOwnSourceAnswersOnlyWhatIsWiredAndRead(t *testing.T) {
	applied := map[int]string{1: "x"}
	src := OwnSource{
		Applied:  func() (map[int]string, bool, bool) { return applied, true, true },
		Env:      func() string { return "prod" },
		Pipeline: func(lane, env string) (string, bool, error) { return "deliver-" + lane + "-" + env, true, nil },
	}
	got, err := src.Own(context.Background())
	if err != nil || got.Pipeline != "deliver-platform-prod" || got.Repo != "bifrost-platform" || got.Dir != MigrationsDir || got.Applied[1] != "x" {
		t.Fatalf("own = %+v, %v", got, err)
	}
	for name, c := range map[string]struct {
		edit func(*OwnSource)
		want string
	}{
		"no runtime": {func(o *OwnSource) { o.Applied = func() (map[int]string, bool, bool) { return nil, false, false } }, "not wired"},
		"unread":     {func(o *OwnSource) { o.Applied = func() (map[int]string, bool, bool) { return nil, false, true } }, "have not been read"},
		"no records": {func(o *OwnSource) {
			o.Pipeline = func(string, string) (string, bool, error) {
				return "", false, errors.New("the release records are not wired")
			}
		}, "release records are not wired"},
		"no rule": {func(o *OwnSource) { o.Pipeline = func(string, string) (string, bool, error) { return "", false, nil } }, "no release rule names the pipeline that delivers platform/prod"},
	} {
		o := src
		c.edit(&o)
		if _, err := o.Own(context.Background()); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want %q", name, err, c.want)
		}
	}
}

// The owner domain classifies the action's errors and names the caller on the
// Job; the server hands those in. These stand in for them, with the one
// property the runner relies on: an uncertain error is told from the others.
type uncertainErr struct{ reason string }

func (e uncertainErr) Error() string { return e.reason }

func isUncertain(err error) bool { var u uncertainErr; return errors.As(err, &u) }

type approvalKey struct{}

var testErrors = Errors{
	Transient:      func(reason string) error { return errors.New(reason) },
	Uncertain:      func(reason string) error { return uncertainErr{reason} },
	ClassifyCreate: func(err error) error { return err },
	Identity: func(ctx context.Context) map[string]string {
		if id, ok := ctx.Value(approvalKey{}).(string); ok {
			return map[string]string{"approval": id}
		}
		return nil
	},
}
