// s5check reads immutable Git objects and a protected policy/checker installation.
// It never executes candidate source itself; in CI mode the protected behavior
// suite runs candidate source in its own separate process.
//
// Modes:
//
//	s5check --ci-materials <policyRoot> <TOOLS.lock.json>   (image build)
//	s5check --ci-request-stdin                              (Tekton s5-check)
//	s5check [flags]                                         (static inspection only)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--ci-materials":
			return ciMaterials(args[1:], stdout, stderr)
		case "--ci-request-stdin":
			return ciRequest(args[1:], stdin, stdout, stderr)
		}
	}
	return inspect(args, stdout, stderr)
}

// ciMaterials prints the canonical pinned material of the image installation.
// The image build stores it as /opt/s5/materials.json; the build pipeline
// reports it so infra pins.py records it next to the image digest.
func ciMaterials(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || !filepath.IsAbs(args[0]) || !filepath.IsAbs(args[1]) {
		fmt.Fprintln(stderr, "usage: s5check --ci-materials <absolute policy root> <absolute TOOLS.lock.json>")
		return 2
	}
	lock, err := os.ReadFile(args[1])
	if err != nil {
		return fail(stderr, err)
	}
	materials, err := s5check.ComputeMaterials(filepath.Clean(args[0]), lock)
	if err != nil {
		return fail(stderr, err)
	}
	if err = emit(stdout, materials); err != nil {
		return fail(stderr, err)
	}
	return 0
}

// ciRequest reads one canonical request, emits the schema-2 ContentEvidence on
// stdout and exits non-zero unless the evidence passed. Diagnostics go to stderr.
func ciRequest(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: s5check --ci-request-stdin")
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return fail(stderr, err)
	}
	req, err := s5check.DecodeCIRequest(raw)
	if err != nil {
		return fail(stderr, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 280*time.Second)
	defer cancel()
	evidence, err := s5check.RunCIRequest(ctx, req)
	if err != nil {
		return fail(stderr, err)
	}
	for _, f := range evidence.Findings {
		fmt.Fprintf(stderr, "%s %s:%d:%d %s\n", f.Code, f.Path, f.Line, f.Column, f.Detail)
	}
	if err = emit(stdout, evidence); err != nil {
		return fail(stderr, err)
	}
	if !evidence.Passed {
		return 1
	}
	return 0
}

// inspect is static inspection only. It has no behavior runner, so it never
// produces ContentEvidence; its StaticEvidence always carries passed=false and
// exit zero means only that static checking found nothing.
func inspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("s5check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repo := flags.String("repo", "", "absolute local read-only Git repository")
	policyPath := flags.String("policies", "", "protected manifest.json")
	checkerPath := flags.String("checker", "", "protected ts/check.cjs")
	checkerHash := flags.String("checker-sha256", "", "fixed checker entrypoint digest (required)")
	modulePath := flags.String("node-module-path", "", "absolute protected node_modules directory")
	nodeBinary := flags.String("node", "node", "trusted Node executable")
	baseline := flags.String("baseline", "", "full M0 commit SHA")
	result := flags.String("result", "", "full R commit SHA")
	paths := flags.String("paths", "", "comma-separated exact paths requesting S5 exemption")
	timeout := flags.Duration("timeout", 2*time.Minute, "checker process deadline")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *repo == "" || *policyPath == "" || *checkerPath == "" || *checkerHash == "" || *baseline == "" || *result == "" {
		fmt.Fprintln(stderr, "repo, policies, checker, checker-sha256, baseline and result are required")
		return 2
	}
	repoAbs, err := filepath.Abs(*repo)
	if err != nil {
		return fail(stderr, err)
	}
	checkerAbs, err := filepath.Abs(*checkerPath)
	if err != nil {
		return fail(stderr, err)
	}
	manifest, err := os.ReadFile(*policyPath)
	if err != nil {
		return fail(stderr, err)
	}
	policy := s5check.TrustedPolicy{Manifest: manifest, Checker: s5check.NodeChecker{NodeBinary: *nodeBinary, Script: checkerAbs, ScriptDigest: *checkerHash, ModulePath: *modulePath, Timeout: *timeout}}
	if *paths != "" {
		policy.CandidatePaths = strings.Split(*paths, ",")
	}
	reader := s5check.GitTreeReader{Directory: repoAbs}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout+2*time.Minute)
	defer cancel()
	out, err := s5check.CheckStaticTrees(ctx, reader, policy, *baseline, *result)
	if err != nil {
		return fail(stderr, err)
	}
	if err = emit(stdout, out); err != nil {
		return fail(stderr, err)
	}
	if len(out.Findings) > 0 {
		return 1
	}
	return 0
}

func emit(stdout io.Writer, value any) error {
	raw, err := s5check.CanonicalJSON(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(raw))
	return err
}

func fail(stderr io.Writer, err error) int { fmt.Fprintln(stderr, err); return 2 }
