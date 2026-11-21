package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// CIRun and Succeeded answer from the reader set by SetCIReader, whatever it
// says: its verdict and run, no verdict when it knows none, and no verdict
// (never the status it returned) when it fails.
func TestCIRunAsksTheReader(t *testing.T) {
	s := &Service{}
	facts := s.PolicyFacts()
	asked := ""
	s.SetCIReader(func(_ context.Context, repo, sha string) (string, string, error) {
		asked = repo + "@" + sha
		return CISucceeded, "ci-platform-1", nil
	})
	status, run, err := facts.CIRun(context.Background(), "bifrost-platform", factSHA1)
	if err != nil || status != CISucceeded || run != "ci-platform-1" || asked != "bifrost-platform@"+factSHA1 {
		t.Fatalf("CIRun: %q %q %v, asked %q", status, run, err, asked)
	}
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err != nil || !ok {
		t.Fatalf("Succeeded: %v %v", ok, err)
	}
	s.SetCIReader(func(context.Context, string, string) (string, string, error) { return CIFailed, "ci-platform-2", nil })
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err != nil || ok {
		t.Fatalf("a failed commit is green: %v %v", ok, err)
	}
	s.SetCIReader(func(context.Context, string, string) (string, string, error) { return "", "", nil })
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err != nil || ok {
		t.Fatalf("a commit the reader knows no verdict for is green: %v %v", ok, err)
	}
	// A reader that fails while naming a pass: the error wins.
	s.SetCIReader(func(context.Context, string, string) (string, string, error) {
		return CISucceeded, "ci-platform-3", errors.New("the evidence ledger is not connected")
	})
	if status, run, err := facts.CIRun(context.Background(), "bifrost-platform", factSHA1); err == nil || status != "" || run != "" {
		t.Fatalf("a failed read answered %q %q %v", status, run, err)
	}
	if ok, err := facts.Succeeded(context.Background(), "bifrost-platform", factSHA1); err == nil || ok {
		t.Fatalf("a failed read is green: %v %v", ok, err)
	}
}

// 2026-11-16: the runs reading was kept as the way back from the evidence
// ledger; after incremental landing it could not stand in for it, so it is
// retired; a CI question with no ledger answers no verdict, never a pass.
//
// With no reader set (a test, a process the ledger is not wired into) CIRun
// answers no verdict and an error, Succeeded is false, and the release
// policy's ci_succeeded does not hold: the release waits. The positive
// control is the same request with a reader that answers Succeeded.
func TestCIRunWithNoReaderIsNoVerdict(t *testing.T) {
	ctx := context.Background()
	s := &Service{}
	facts := s.PolicyFacts()
	status, run, err := facts.CIRun(ctx, "repo-app", factSHA1)
	if !errors.Is(err, ErrNoCIReader) || status != "" || run != "" {
		t.Fatalf("CIRun with no reader = %q %q %v; want no verdict and ErrNoCIReader", status, run, err)
	}
	if ok, err := facts.Succeeded(ctx, "repo-app", factSHA1); ok || !errors.Is(err, ErrNoCIReader) {
		t.Fatalf("Succeeded with no reader = %v %v", ok, err)
	}
	// After a reader was set and removed, the same: nothing is fallen back on.
	s.SetCIReader(func(context.Context, string, string) (string, string, error) { return CISucceeded, "ci-1", nil })
	s.SetCIReader(nil)
	if status, _, err := facts.CIRun(ctx, "repo-app", factSHA1); !errors.Is(err, ErrNoCIReader) || status != "" {
		t.Fatalf("CIRun after the reader was removed = %q %v", status, err)
	}

	const pipeline = "deliver-app-prod"
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	key, cms := rptest.NewKey(t), rptest.NewConfigMaps()
	text := rptest.PolicyText("rp-20261116-1200", now.Add(-time.Hour), 90, pipeline)
	cms.Set(releasepolicy.PolicyConfigMap, map[string]string{
		"policy.yaml": text, "policy.sig": key.Sign(t, []byte(text), releasepolicy.NamespacePolicy), "allowed_signers": key.AllowedSigners(),
	})
	cms.Set(releasepolicy.FreezeConfigMap, map[string]string{"frozen": "false"})
	oldSHA, newSHA := rptest.SHA("old"), rptest.SHA("new")
	world := &rptest.Facts{
		Heads: map[string]string{"repo-app": newSHA}, Files: map[string][]string{"repo-app": {"docs/readme.md"}},
		Records: map[string]map[string]string{pipeline: {"repo-app": oldSHA}},
		Dirs:    map[string][]string{"repo-ops@main:db-steps.d": {"README.md"}},
	}
	decide := func() releasepolicy.Decision {
		eng := releasepolicy.New(releasepolicy.Deps{
			ConfigMaps: cms, Writer: rptest.Writer{CMs: cms}, Git: world, CI: facts, Window: world, Deployed: world,
			Anchor: key.Fingerprint, Now: func() time.Time { return now },
		})
		return eng.Decide(ctx, "start_pipeline_run", actions.TierC, map[string]any{"name": pipeline, "revision": "main", "who": "agent@mac"})
	}
	d := decide()
	if reasons := strings.Join(d.Reasons, "\n"); d.Auto || !strings.Contains(reasons, "repo-app: cannot read CI") || !strings.Contains(reasons, ErrNoCIReader.Error()) {
		t.Fatalf("with no CI reader the policy decided auto=%v, reasons %q", d.Auto, d.Reasons)
	}
	if strings.Contains(d.ClauseText(), "ci_succeeded") {
		t.Fatalf("ci_succeeded is among the clauses that held: %s", d.ClauseText())
	}
	// Positive control: the same world with a reader that says Succeeded.
	s.SetCIReader(func(_ context.Context, repo, sha string) (string, string, error) {
		if repo == "repo-app" && sha == newSHA {
			return CISucceeded, "ci-app-1", nil
		}
		return "", "", nil
	})
	if d = decide(); !d.Auto || !strings.Contains(d.ClauseText(), "ci_succeeded") {
		t.Fatalf("control: with a green reader the policy decided auto=%v, reasons %q, clauses %s", d.Auto, d.Reasons, d.ClauseText())
	}
}
