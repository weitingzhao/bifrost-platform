package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// A task's siblings and setup (CUT-agentd-console-checks-2026-10-16). The
// siblings are git, with the Gitea credentials agentd keeps from executors, so
// agentd clones them afresh itself, beside the checkout and inside its own
// directory, before the attempt starts: a failure there ends the run like a
// failed checkout. The
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

// siblings makes each sibling a fresh clone beside the checkout, inside
// agentd's own directory, and records the commits in siblings.json in the
// attempt's output.
func (d *Daemon) siblings(ctx context.Context, a *Attempt, sibs []Sibling) error {
	if len(sibs) == 0 {
		return nil
	}
	heads := map[string]string{}
	for _, s := range sibs {
		rel, err := filepath.Rel(d.Root, filepath.Join(filepath.Dir(a.Dir), s.Repo))
		if err != nil {
			return fmt.Errorf("agentd: sibling %s: %w", s.Repo, err)
		}
		head, err := PrepareSibling(ctx, d.Root, rel, d.Remote(s.Repo), s.Ref, d.GitEnv)
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

// setupOpts are what a setup gets besides its commands (TD-371): a home of
// its own (Home), never the executor's, so it cannot leave configuration
// there; when the attempt is walled, the narrower wall each command runs in
// (Writes: the checkout, the siblings and the attempt's temporary
// directory, not the executor's home nor the attempt's output); and the paths
// that may not be in the checkout once the commands are done (Refuse:
// cursor's project hooks and MCP servers).
type setupOpts struct {
	Home   string
	Writes []string
	Refuse []string
}

// flags are opts on agentd setup's command line.
func (o setupOpts) flags() []string {
	var f []string
	if o.Home != "" {
		f = append(f, "--home", o.Home)
	}
	for _, w := range o.Writes {
		f = append(f, "--write", w)
	}
	for _, r := range o.Refuse {
		f = append(f, "--refuse", r)
	}
	return f
}

// setupArgv is the start of a command that runs the setup in out first.
func setupArgv(self, out string, o setupOpts) []string {
	argv := append([]string{self, SetupArg, "--spec", filepath.Join(out, "setup-spec.json"), "--out", filepath.Join(out, "setup.json")}, o.flags()...)
	return append(argv, "--")
}

// withSetup makes sp run the setup commands first: agentd setup, with the
// commands in setup-spec.json, then sp's own argv.
func withSetup(self, out string, setup [][]string, sp Spawn, o setupOpts) (Spawn, error) {
	body, err := json.Marshal(TaskSpec{Text: "setup", Verify: setup, TimeoutS: int(SetupLimit / time.Second)})
	if err != nil {
		return sp, err
	}
	if err = os.WriteFile(filepath.Join(out, "setup-spec.json"), body, 0o600); err != nil {
		return sp, err
	}
	sp.Argv = append(setupArgv(self, out, o), sp.Argv...)
	return sp, nil
}

// stringsFlag is a flag given once per value.
type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

// RunSetup is agentd setup --spec <file> --out <file> [--home <dir>]
// [--write <dir>...] [--refuse <path>...] -- <argv>: it runs the spec's
// commands in the current directory as Verify does (with HOME the given
// home; each inside agentd confine over the --write directories when there
// are any), writes their record to --out and, when they all pass and no
// --refuse path is in the checkout, execs argv (argv[0] looked up on PATH)
// in its place. It returns only when it does not exec, with the exit code to
// use.
func RunSetup(args []string) int {
	i := slices.Index(args, "--")
	fs := flag.NewFlagSet(SetupArg, flag.ContinueOnError)
	specPath := fs.String("spec", "", "the setup commands, as a spec")
	outPath := fs.String("out", "", "where to write setup.json")
	home := fs.String("home", "", "the commands' HOME")
	var writes, refuse stringsFlag
	fs.Var(&writes, "write", "a directory the commands may write (repeated); none: no wall of their own")
	fs.Var(&refuse, "refuse", "a path that may not be there once they are done (repeated)")
	if i < 0 || i == len(args)-1 || fs.Parse(args[:i]) != nil || *specPath == "" || *outPath == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: agentd setup --spec <file> --out <file> [--home <dir>] [--write <dir>...] [--refuse <path>...] -- <command> [args...]")
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
	env := AllowedEnv(os.Environ())
	if *home != "" {
		if err = os.MkdirAll(*home, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "agentd setup:", err)
			return 2
		}
		env = withEnv(env, "HOME", *home)
	}
	v, err := walledVerify(context.Background(), env, spec.Verify, writes, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd setup:", err)
		return 2
	}
	// A path that may not be in the checkout fails the setup as a command
	// does, on its record too: setup.json is how agentd tells a failed setup
	// from its executor's failure (TD-394, prepare.go).
	for _, r := range refuse {
		if !v.Pass {
			break
		}
		if _, lerr := os.Lstat(r); !errors.Is(lerr, os.ErrNotExist) {
			v.Pass, v.Reason = false, fmt.Sprintf("the checkout has %s after the setup (or it cannot be looked at: %v)", r, lerr)
		}
	}
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
		if n := len(v.Commands); n > 0 && (v.Commands[n-1].ExitCode != 0 || v.Commands[n-1].TimedOut) {
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
