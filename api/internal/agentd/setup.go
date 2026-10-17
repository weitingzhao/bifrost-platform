package agentd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"time"
)

// A task's siblings and setup (CUT-agentd-console-checks-2026-10-16). The
// siblings are git, with the Gitea credentials agentd keeps from executors, so
// agentd brings them up to date itself, beside the checkout, before the
// attempt starts: a failure there ends the run like a failed checkout. The
// setup commands need no credentials but the network, so they run as the
// attempt's first act, inside its process group under the launcher: agentd
// setup runs them, writes setup.json and, when they pass, execs the executor
// in its own place. A stop, a lost lease or the hard limit ends a setup like
// any executor, and a failed one ends the attempt with its exit code (3): no
// report, so the attempt failed.

// SetupArg is the first argument that makes agentd run a task's setup and then
// its executor.
const SetupArg = "setup"

// SetupLimit is how long a task's setup may run, all of its commands.
const SetupLimit = 10 * time.Minute

// setupFailed is the exit code of a setup that did not pass.
const setupFailed = 3

// prepare is the spec whose siblings and setup an attempt prepares: a check's
// subject spec, else its own.
func (a *Attempt) prepare() TaskSpec {
	if a.Subject != nil {
		return *a.Subject
	}
	return a.Spec
}

// siblings brings each sibling up to date beside the checkout and records the
// commits in siblings.json in the attempt's output.
func (d *Daemon) siblings(ctx context.Context, a *Attempt, sibs []Sibling) error {
	if len(sibs) == 0 {
		return nil
	}
	heads := map[string]string{}
	for _, s := range sibs {
		dir := filepath.Join(filepath.Dir(a.Dir), s.Repo)
		head, err := PrepareSibling(ctx, dir, d.Remote(s.Repo), s.Ref, d.GitEnv)
		if err != nil {
			return fmt.Errorf("agentd: sibling %s at %s: %w", s.Repo, s.Ref, err)
		}
		heads[s.Repo] = head
		d.logf("agentd: %s: sibling %s at %s is %s", a.Claim.AttemptID, s.Repo, s.Ref, head)
	}
	body, err := json.MarshalIndent(heads, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.Out, "siblings.json"), body, 0o600)
}

// withSetup makes sp run the setup commands first: agentd setup, with the
// commands in setup-spec.json, then sp's own argv.
func withSetup(self, out string, setup [][]string, sp Spawn) (Spawn, error) {
	body, err := json.Marshal(TaskSpec{Text: "setup", Verify: setup, TimeoutS: int(SetupLimit / time.Second)})
	if err != nil {
		return sp, err
	}
	path := filepath.Join(out, "setup-spec.json")
	if err = os.WriteFile(path, body, 0o600); err != nil {
		return sp, err
	}
	argv := []string{self, SetupArg, "--spec", path, "--out", filepath.Join(out, "setup.json"), "--"}
	sp.Argv = append(argv, sp.Argv...)
	return sp, nil
}

// RunSetup is agentd setup --spec <file> --out <file> -- <argv>: it runs the
// spec's commands in the current directory as Verify does, writes their
// record to --out and, when they all pass, execs argv (argv[0] looked up on
// PATH) in its place. It returns only when it does not exec, with the exit
// code to use.
func RunSetup(args []string) int {
	i := slices.Index(args, "--")
	fs := flag.NewFlagSet(SetupArg, flag.ContinueOnError)
	specPath := fs.String("spec", "", "the setup commands, as a spec")
	outPath := fs.String("out", "", "where to write setup.json")
	if i < 0 || i == len(args)-1 || fs.Parse(args[:i]) != nil || *specPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: agentd setup --spec <file> --out <file> -- <command> [args...]")
		return 2
	}
	body, err := os.ReadFile(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd setup:", err)
		return 2
	}
	spec, err := ParseSpec(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd setup:", err)
		return 2
	}
	limit := time.Duration(spec.TimeoutS) * time.Second
	if limit <= 0 {
		limit = SetupLimit
	}
	// The setup sees the allow-listed environment only: a vendor key the
	// executor's own environment carries (cursor's, claude's) is for the
	// executor, not for the install scripts of the checkout's dependencies
	// (review 227 A2). The executor gets its environment back when it is
	// exec'd below.
	v := Verify(context.Background(), ".", AllowedEnv(os.Environ()), spec.Verify, limit)
	if out, merr := json.MarshalIndent(v, "", "  "); merr != nil {
		err = merr
	} else {
		err = os.WriteFile(*outPath, out, 0o600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd setup:", err)
		return 2
	}
	if !v.Pass {
		fmt.Fprintf(os.Stderr, "agentd setup: %s; the executor did not start\n", v.Reason)
		if n := len(v.Commands); n > 0 {
			fmt.Fprintln(os.Stderr, v.Commands[n-1].Tail)
		}
		return setupFailed
	}
	argv := args[i+1:]
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd setup:", err)
		return 127
	}
	err = syscall.Exec(path, argv, os.Environ())
	fmt.Fprintln(os.Stderr, "agentd setup:", err)
	return 127
}
