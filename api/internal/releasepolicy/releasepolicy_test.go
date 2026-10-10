package releasepolicy_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

const pipeline = "deliver-app-prod"

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type fixture struct {
	key   rptest.Key
	cms   *rptest.ConfigMaps
	facts *rptest.Facts
	eng   *releasepolicy.Engine
	clock time.Time
}

// newFixture: a policy signed an hour ago for 90 days, no freeze, window held,
// main moved by one docs file, CI green, the one committed DB step done.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{key: rptest.NewKey(t), cms: rptest.NewConfigMaps(), clock: now}
	f.signPolicy(t, f.key, rptest.PolicyText("rp-20261008-1100", now.Add(-time.Hour), 90, pipeline, "deliver-with-db"))
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	f.facts = &rptest.Facts{
		Heads:   map[string]string{"repo-app": newSHA},
		Files:   map[string][]string{"repo-app": {"docs/readme.md"}},
		Green:   map[string]bool{"repo-app@" + newSHA: true},
		Records: map[string]map[string]string{pipeline: {"repo-app": oldSHA}, "deliver-with-db": {"repo-app": oldSHA}},
		Dirs:    map[string][]string{"repo-ops@main:db-steps.d": {"README.md", "2026-10-01-a.md"}},
		Text: map[string]string{
			"repo-ops@main:db-steps.d/2026-10-01-a.md": "---\nid: 2026-10-01-a\nenvs: stg prod\nwhen: before\ndone: stg prod\n---\n# a\n",
		},
	}
	f.eng = f.engine(f.key.Fingerprint)
	return f
}

func (f *fixture) engine(anchor string) *releasepolicy.Engine {
	return releasepolicy.New(releasepolicy.Deps{
		ConfigMaps: f.cms, Writer: rptest.Writer{CMs: f.cms}, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
		Anchor: anchor, Now: func() time.Time { return f.clock },
	})
}

func (f *fixture) signPolicy(t *testing.T, k rptest.Key, text string) {
	f.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
		"policy.yaml":     text,
		"policy.sig":      k.Sign(t, []byte(text), releasepolicy.NamespacePolicy),
		"allowed_signers": k.AllowedSigners(),
	})
}

func (f *fixture) decide(params map[string]any) releasepolicy.Decision {
	if params == nil {
		params = map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}
	}
	return f.eng.Decide(context.Background(), "start_pipeline_run", actions.TierC, params)
}

func wantAuto(t *testing.T, d releasepolicy.Decision) {
	t.Helper()
	if !d.Auto || len(d.Reasons) != 0 {
		t.Fatalf("want auto-approve, got reasons %q", d.Reasons)
	}
}

func wantWait(t *testing.T, d releasepolicy.Decision, substr string) {
	t.Helper()
	if d.Auto {
		t.Fatalf("auto-approved; want a wait containing %q", substr)
	}
	if !strings.Contains(strings.Join(d.Reasons, "\n"), substr) {
		t.Fatalf("reasons %q do not mention %q", d.Reasons, substr)
	}
}

func TestValidPolicyAutoApprovesAndNamesTheCommits(t *testing.T) {
	f := newFixture(t)
	d := f.decide(nil)
	wantAuto(t, d)
	if d.PolicyID != "rp-20261008-1100" || d.SHAText() != "repo-app="+rptest.SHA("new") {
		t.Fatalf("decision = %+v sha=%s", d, d.SHAText())
	}
	if got, want := d.ClauseText(), "signature,unexpired,not_frozen,allow:"+pipeline+
		",window_held_by_requester,revision,ci_succeeded,no_ddl,no_d10_paths,no_trust_anchor_change"; got != want {
		t.Fatalf("clauses = %s, want %s", got, want)
	}
	// A pipeline that delivers an env with DB steps names that clause too.
	if c := f.decide(map[string]any{"name": "deliver-with-db", "revision": "main", "who": "agent@mac"}).ClauseText(); !strings.Contains(c, "no_pending_before_db_steps") {
		t.Fatalf("db pipeline clauses = %s", c)
	}
	// A tag and a SHA equal to the head of main are both releasable revisions.
	f.facts.Tags = map[string]map[string]string{"repo-app": {"v1.2.0": rptest.SHA("new")}}
	wantAuto(t, f.decide(map[string]any{"name": pipeline, "revision": "v1.2.0", "who": "agent@mac"}))
	wantAuto(t, f.decide(map[string]any{"name": pipeline, "revision": rptest.SHA("new"), "who": "agent@mac"}))
	// Nothing changed since the record: no diff to check, still allowed.
	f.facts.Records[pipeline]["repo-app"] = rptest.SHA("new")
	wantAuto(t, f.decide(nil))
}

func TestPolicyFailuresWait(t *testing.T) {
	cases := map[string]struct {
		mutate func(t *testing.T, f *fixture)
		want   string
	}{
		"expired": {func(t *testing.T, f *fixture) { f.clock = now.Add(91 * 24 * time.Hour) }, "expired at"},
		"missing": {func(t *testing.T, f *fixture) { f.cms.Set(releasepolicy.PolicyConfigMap, nil) }, "no signed policy"},
		"no signature": {func(t *testing.T, f *fixture) {
			d, _, _ := f.cms.ConfigMap(context.Background(), releasepolicy.PolicyConfigMap)
			d["policy.sig"] = ""
			f.cms.Set(releasepolicy.PolicyConfigMap, d)
		}, "policy.sig is missing"},
		"tampered": {func(t *testing.T, f *fixture) {
			d, _, _ := f.cms.ConfigMap(context.Background(), releasepolicy.PolicyConfigMap)
			d["policy.yaml"] = strings.Replace(d["policy.yaml"], `"valid_days": 90`, `"valid_days": 900`, 1)
			f.cms.Set(releasepolicy.PolicyConfigMap, d)
		}, "does not verify"},
		"foreign key": {func(t *testing.T, f *fixture) {
			f.signPolicy(t, rptest.NewKey(t), rptest.PolicyText("rp-x", now.Add(-time.Hour), 7, pipeline))
		}, "not the compiled-in Owner key"},
		"wrong namespace": {func(t *testing.T, f *fixture) {
			text := rptest.PolicyText("rp-x", now.Add(-time.Hour), 7, pipeline)
			f.cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
				"policy.yaml": text, "policy.sig": f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze),
			})
		}, "namespace"},
		"signed in the future": {func(t *testing.T, f *fixture) {
			f.signPolicy(t, f.key, rptest.PolicyText("rp-x", now.Add(time.Hour), 7, pipeline))
		}, "in the future"},
		"unreadable": {func(t *testing.T, f *fixture) {
			f.cms.Err[releasepolicy.PolicyConfigMap] = errors.New("forbidden")
		}, "cannot read"},
		"not allowed": {func(t *testing.T, f *fixture) {
			f.signPolicy(t, f.key, rptest.PolicyText("rp-x", now.Add(-time.Hour), 7, "some-other-pipeline"))
		}, "not in the policy's allow list"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.mutate(t, f)
			wantWait(t, f.decide(nil), tc.want)
		})
	}
	t.Run("no anchor compiled", func(t *testing.T) {
		f := newFixture(t)
		f.eng = f.engine("")
		wantWait(t, f.decide(nil), "no trust anchor")
	})
}

func TestConditionFailuresWait(t *testing.T) {
	cases := map[string]struct {
		mutate func(f *fixture)
		params map[string]any
		want   string
	}{
		"ddl deleted": {func(f *fixture) { f.facts.Files["repo-app"] = []string{"src/app/ddl_orders.py"} }, nil, "is deleted"},
		"sql drop": {func(f *fixture) {
			f.facts.Files["repo-app"] = []string{"db/seed.sql"}
			f.facts.Text = map[string]string{"repo-app@" + rptest.SHA("new") + ":db/seed.sql": "DROP TABLE orders;\n"}
		}, nil, "not additive-only"},
		"d10 path":     {func(f *fixture) { f.facts.Files["repo-app"] = []string{"engine/orders/place.go"} }, nil, "hits no_d10_paths"},
		"trust anchor": {func(f *fixture) { f.facts.Files["repo-app"] = []string{"internal/releasepolicy/anchor.go"} }, nil, "hits no_trust_anchor_change"},
		"ci red":       {func(f *fixture) { f.facts.Green = nil }, nil, "no Succeeded ci run"},
		"window":       {func(f *fixture) { f.facts.Window = "REFUSED: release window held by someone else" }, nil, "release window"},
		"no record":    {func(f *fixture) { delete(f.facts.Records, pipeline) }, nil, "no release record"},
		"record gap":   {func(f *fixture) { f.facts.Missing = []string{"repo-lib"} }, nil, "repo-lib: the newest release record"},
		"diff error":   {func(f *fixture) { f.facts.FilesErr = errors.New("status 500") }, nil, "cannot read the diff"},
		"empty diff":   {func(f *fixture) { f.facts.Files["repo-app"] = nil }, nil, "lists no files"},
		"huge diff": {func(f *fixture) {
			files := make([]string, 100)
			for i := range files {
				files[i] = "docs/page" + string(rune('a'+i%26)) + ".md"
			}
			f.facts.Files["repo-app"] = files
		}, nil, "may be truncated"},
		"branch revision": {func(*fixture) {}, map[string]any{"name": pipeline, "revision": "feature/x", "who": "agent@mac"}, "is not main, a tag"},
		"stale sha": {func(f *fixture) {
			f.facts.Commits = map[string]bool{"repo-app@" + rptest.SHA("mid"): true}
		}, map[string]any{"name": pipeline, "revision": rptest.SHA("mid"), "who": "agent@mac"}, "is not the head of main"},
		"db step pending": {func(f *fixture) {
			f.facts.Text["repo-ops@main:db-steps.d/2026-10-01-a.md"] = "---\nid: 2026-10-01-a\nenvs: stg prod\nwhen: before\ndone: stg\n---\n"
		}, map[string]any{"name": "deliver-with-db", "revision": "main", "who": "agent@mac"}, "not marked done on main: 2026-10-01-a"},
		"db steps unreadable": {func(f *fixture) { f.facts.Dirs = nil }, map[string]any{"name": "deliver-with-db", "revision": "main", "who": "agent@mac"}, "cannot list repo-ops/db-steps.d"},
		"db step bad front matter": {func(f *fixture) {
			f.facts.Text["repo-ops@main:db-steps.d/2026-10-01-a.md"] = "no front matter"
		}, map[string]any{"name": "deliver-with-db", "revision": "main", "who": "agent@mac"}, "no readable front matter"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.mutate(f)
			wantWait(t, f.decide(tc.params), tc.want)
		})
	}
}

func TestTierAndActionScope(t *testing.T) {
	f := newFixture(t)
	params := map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"}
	ctx := context.Background()
	wantWait(t, f.eng.Decide(ctx, "start_pipeline_run", actions.TierD, params), "tier D is never auto-approved")
	wantWait(t, f.eng.Decide(ctx, "start_pipeline_run", actions.TierB, params), "only tier C")
	wantWait(t, f.eng.Decide(ctx, "cordon_node", actions.TierC, map[string]any{"name": "node-a"}), "not a release")
}

func TestFreeze(t *testing.T) {
	frozenAt := "2026-10-08T11:30:00Z"
	t.Run("frozen", func(t *testing.T) {
		f := newFixture(t)
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": frozenAt, "who": "owner", "reason": "incident"})
		wantWait(t, f.decide(nil), "releases are frozen: frozen by owner")
	})
	t.Run("missing counts as frozen", func(t *testing.T) {
		f := newFixture(t)
		f.cms.Set(releasepolicy.FreezeConfigMap, nil)
		wantWait(t, f.decide(nil), "is missing")
	})
	t.Run("unreadable counts as frozen", func(t *testing.T) {
		f := newFixture(t)
		f.cms.Err[releasepolicy.FreezeConfigMap] = errors.New("forbidden")
		wantWait(t, f.decide(nil), "is unreadable")
	})
	t.Run("unsigned unfreeze stays frozen", func(t *testing.T) {
		f := newFixture(t)
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false", "frozen_at": frozenAt})
		wantWait(t, f.decide(nil), "no signed unfreeze")
	})
	t.Run("signed unfreeze lifts it; an old one cannot lift a newer freeze", func(t *testing.T) {
		f := newFixture(t)
		text := "unfreeze frozen_at=" + frozenAt + " at=2026-10-08T11:45:00Z by=owner\n"
		lifted := map[string]string{
			"frozen": "false", "frozen_at": frozenAt,
			"unfreeze.txt": text, "unfreeze.sig": f.key.Sign(t, []byte(text), releasepolicy.NamespaceUnfreeze),
		}
		f.cms.Set(releasepolicy.FreezeConfigMap, lifted)
		wantAuto(t, f.decide(nil))
		// A policy-namespace signature over the same text is not an unfreeze.
		wrongNS := map[string]string{}
		for k, v := range lifted {
			wrongNS[k] = v
		}
		wrongNS["unfreeze.sig"] = f.key.Sign(t, []byte(text), releasepolicy.NamespacePolicy)
		f.cms.Set(releasepolicy.FreezeConfigMap, wrongNS)
		wantWait(t, f.decide(nil), "unfreeze signature")
		// New freeze is seen, then someone restores the old signed unfreeze.
		f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:50:00Z"})
		wantWait(t, f.decide(nil), "releases are frozen")
		f.cms.Set(releasepolicy.FreezeConfigMap, lifted)
		wantWait(t, f.decide(nil), "a later freeze")
	})
}

func TestFreezeHistoryPersists(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(t.TempDir(), "release-policy-freeze")
	mk := func() *releasepolicy.Engine {
		return releasepolicy.New(releasepolicy.Deps{
			ConfigMaps: f.cms, Git: f.facts, CI: f.facts, Window: f.facts, Deployed: f.facts,
			Anchor: f.key.Fingerprint, Now: func() time.Time { return f.clock }, StatePath: path,
		})
	}
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "true", "frozen_at": "2026-10-08T11:50:00Z"})
	mk().Status(context.Background())
	// After a restart the record of that freeze is still there.
	f.cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	if st := mk().Status(context.Background()); !st.Frozen || !strings.Contains(st.FreezeReason, "record is gone") {
		t.Fatalf("status after restart = %+v", st)
	}
}

func TestVerifiesSSHKeygenSignatures(t *testing.T) {
	keygen, lookErr := exec.LookPath("ssh-keygen")
	if lookErr != nil {
		t.Skip("ssh-keygen not installed")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "throwaway")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(keygen, args...).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen %v: %v %s", args, err, out)
		}
	}
	run("-q", "-t", "ed25519", "-N", "", "-C", "test", "-f", keyPath)
	msg := []byte(rptest.PolicyText("rp-keygen", now, 7, pipeline))
	msgPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(msgPath, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	run("-Y", "sign", "-f", keyPath, "-n", releasepolicy.NamespacePolicy, msgPath)
	sig, err := os.ReadFile(msgPath + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	pubRaw, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	want, _, _, _, err := ssh.ParseAuthorizedKey(pubRaw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := releasepolicy.VerifySSHSig(sig, msg, releasepolicy.NamespacePolicy)
	if err != nil {
		t.Fatalf("verify ssh-keygen signature: %v", err)
	}
	if ssh.FingerprintSHA256(got) != ssh.FingerprintSHA256(want) {
		t.Fatalf("signer %s, want %s", ssh.FingerprintSHA256(got), ssh.FingerprintSHA256(want))
	}
	if _, err := releasepolicy.VerifySSHSig(sig, append(msg, ' '), releasepolicy.NamespacePolicy); err == nil {
		t.Fatal("a changed message verified")
	}
	if keys := releasepolicy.SignerKeys(`owner namespaces="bifrost-release-policy" ` + string(pubRaw)); len(keys) != 1 ||
		ssh.FingerprintSHA256(keys[0]) != ssh.FingerprintSHA256(want) {
		t.Fatalf("SignerKeys = %v", keys)
	}
}

// Same cases as scripts/release/test_policy_check.py in bifrost-trade-infra.
func TestGlobMatchesPolicyCheck(t *testing.T) {
	for _, tc := range []struct {
		path, glob string
		want       bool
	}{
		{"ddl.py", "**/ddl*.py", true},
		{"src/pkg/ddl_init.py", "**/ddl*.py", true},
		{"src/pkg/addl.py", "**/ddl*.py", false},
		{"a/b/c.sql", "**/*.sql", true},
		{"migrations/0001.py", "**/migrations/**", true},
		{"x/migrations/y/z.py", "**/migrations/**", true},
		{"daemon/execution/a.py", "daemon/execution/**", true},
		{"src/daemon/execution/a.py", "daemon/execution/**", false},
		{"scripts/release/release.sh", "scripts/release/release.sh", true},
		{"/scripts/release/lib.sh", "scripts/release/lib.sh", true},
		{"api/internal/server/release_policy_wire.go", "api/internal/server/release_policy*.go", true},
		{"api/internal/server/release_x/a.go", "api/internal/server/release_policy*.go", false},
	} {
		if got := releasepolicy.PathMatches(tc.path, tc.glob); got != tc.want {
			t.Errorf("PathMatches(%q, %q) = %v", tc.path, tc.glob, got)
		}
	}
}

func TestStatusAndMetrics(t *testing.T) {
	f := newFixture(t)
	st := f.eng.Status(context.Background())
	if !st.Valid || st.Frozen || st.RemainingSeconds != int64((90*24-1)*3600) || st.SignCommand != releasepolicy.SignCommand ||
		strings.Join(st.ReminderWindows, ",") != "14d,3d,1d" {
		t.Fatalf("status = %+v", st)
	}
	var b strings.Builder
	releasepolicy.WriteMetrics(&b)
	if !strings.Contains(b.String(), "bifrost_release_policy_expires_in_seconds 7772400\n") {
		t.Fatalf("metrics:\n%s", b.String())
	}
	f.clock = now.Add(91 * 24 * time.Hour)
	st = f.eng.Status(context.Background())
	if st.Valid || !st.Expired {
		t.Fatalf("expired status = %+v", st)
	}
	b.Reset()
	releasepolicy.WriteMetrics(&b)
	if !strings.Contains(b.String(), "bifrost_release_policy_expires_in_seconds 0\n") {
		t.Fatalf("metrics after expiry:\n%s", b.String())
	}
}
