package agentd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
)

// The write wall (TD-371; agent-config/work/multi-agent/
// CUT-agentd-confine-2026-10-19.md). Attempts run as one user, so without a
// wall any process of one may write the user's whole home: the tokens, the
// registry, another attempt's checkout, the executors' configuration. On
// Linux agentd runs each attempt's whole process tree, its setup and its
// executor, in a Landlock domain that may write only the attempt's workspace,
// its output directory and the siblings it was given, and a few devices:
// agentd confine --write <dir>... -- <argv>, between the launcher (outside,
// the group's leader as before) and the executor. Reading is not governed
// (the same user may read: v5 §6.2). Each attempt also has a home of its own
// under its output (HOME, TMPDIR, the Go caches, codex's and claude's
// configuration directories), new, and empty but for what agentd puts there,
// so nothing an earlier run wrote is read. An executor shares the host
// (share.go) only when ProveWall found the wall holding for it on this host
// and the spawn built for the attempt carries every pin (checkPins).

// ConfineArg is the first argument that makes agentd run argv inside the
// write wall.
const ConfineArg = "confine"

// Wall is what ProveWall found on this host: the kernel's Landlock ABI and
// the executors that run inside the wall. A nil Wall walls nothing.
type Wall struct {
	ABI       int
	Executors map[string]bool
}

func (w *Wall) holds(executor string) bool { return w != nil && w.Executors[executor] }

// wallArgv is the start of a walled command: argv follows it.
func wallArgv(self string, writes []string) []string {
	argv := []string{self, ConfineArg}
	for _, w := range writes {
		argv = append(argv, "--write", w)
	}
	return append(argv, "--")
}

// RunConfine is agentd confine --write <dir> [--write <dir>...] -- <argv>: it
// holds itself to writing beneath the directories (each absolute, a directory
// and not a link) and execs argv (argv[0] looked up on PATH) in its place. It
// returns only when it does not exec; when the wall cannot be raised argv
// never runs.
func RunConfine(args []string) int {
	// Landlock restricts the calling thread; the exec below runs on it.
	runtime.LockOSThread()
	var writes []string
	i := 0
	for ; i+1 < len(args) && args[i] == "--write"; i += 2 {
		writes = append(writes, args[i+1])
	}
	if len(writes) == 0 || i >= len(args)-1 || args[i] != "--" {
		fmt.Fprintln(os.Stderr, "usage: agentd confine --write <dir> [--write <dir>...] -- <command> [args...]")
		return 2
	}
	for _, w := range writes {
		fi, err := os.Lstat(w)
		if !filepath.IsAbs(w) || err != nil || !fi.IsDir() {
			fmt.Fprintf(os.Stderr, "agentd confine: %q is not a directory to write in\n", w)
			return 126
		}
	}
	if err := restrictWrites(writes); err != nil {
		fmt.Fprintln(os.Stderr, "agentd confine:", err)
		return 126
	}
	argv := args[i+1:]
	path, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentd confine:", err)
		return 127
	}
	err = syscall.Exec(path, argv, os.Environ())
	fmt.Fprintln(os.Stderr, "agentd confine:", err)
	return 127
}

// An attempt's own directories, under its output.
func attemptHome(out string) string { return filepath.Join(out, "home") }
func attemptTmp(out string) string  { return filepath.Join(out, "tmp") }
func codexHomeOf(out string) string { return filepath.Join(attemptHome(out), ".codex") }
func claudeDirOf(out string) string { return filepath.Join(attemptHome(out), ".claude") }

// homePins are the variables every executor's environment has set to the
// attempt's own directories.
func homePins(out string) [][2]string {
	home := attemptHome(out)
	return [][2]string{{"HOME", home}, {"TMPDIR", attemptTmp(out)}, {"GOPATH", filepath.Join(home, "go")},
		{"GOMODCACHE", filepath.Join(home, "go", "pkg", "mod")}, {"GOCACHE", filepath.Join(home, ".cache", "go-build")}}
}

// attemptEnv is env with the attempt's own home, temporary directory and Go
// caches, and without CODEX_HOME (codex's executor sets its own); it makes
// the home and the temporary directory.
func attemptEnv(env []string, out string) ([]string, error) {
	if err := ownDir(attemptHome(out)); err != nil {
		return nil, err
	}
	if err := ownDir(attemptTmp(out)); err != nil {
		return nil, err
	}
	env = withoutEnv(env, "CODEX_HOME")
	for _, kv := range homePins(out) {
		env = withEnv(env, kv[0], kv[1])
	}
	return env, nil
}

// ownDir makes dir, or takes it when it is already a directory (not a link).
func ownDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err == nil || !errors.Is(err, fs.ErrExist) {
		return err
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("agentd: %s is not a directory of the attempt's own", dir)
	}
	return nil
}

// withoutEnv is env without key.
func withoutEnv(env []string, key string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, key+"=") })
}

// envValues are the values env gives key, in order.
func envValues(env []string, key string) []string {
	var out []string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			out = append(out, v)
		}
	}
	return out
}

// siblingDirs are where an attempt's siblings were cloned (setup.go).
func siblingDirs(a *Attempt, sibs []Sibling) []string {
	out := make([]string, 0, len(sibs))
	for _, s := range sibs {
		out = append(out, filepath.Join(filepath.Dir(a.Dir), s.Repo))
	}
	return out
}

// checkPins says what the spawn of a walled attempt lacks: the wall over
// exactly writes, outermost; every variable of homePins, and codex's or
// claude's configuration directory, set once to the attempt's own; codex's and
// claude's flags (executors.go). An executor this does not know has no pins.
func checkPins(executor, self string, a *Attempt, writes []string, sp Spawn) error {
	var lacks []string
	wall := wallArgv(self, writes)
	if len(sp.Argv) <= len(wall) || !slices.Equal(sp.Argv[:len(wall)], wall) {
		lacks = append(lacks, "the wall over its own directories")
	}
	want := map[string]string{}
	for _, kv := range homePins(a.Out) {
		want[kv[0]] = kv[1]
	}
	var flags [][]string
	switch executor {
	case "exec:codex":
		want["CODEX_HOME"] = codexHomeOf(a.Out)
		flags = append(flags, []string{"--ignore-rules"}, []string{"-c", codexWalled}, []string{"-c", codexRoots(attemptTmp(a.Out))})
		for _, p := range codexPins {
			flags = append(flags, []string{"-c", p})
		}
	case "exec:claude":
		want["CLAUDE_CONFIG_DIR"] = claudeDirOf(a.Out)
		flags = claudePins
	case "exec:cursor", "exec:deterministic":
	default:
		return fmt.Errorf("agentd: %s has no pins to hold it to its workspace", executor)
	}
	for _, k := range []string{"HOME", "TMPDIR", "GOPATH", "GOMODCACHE", "GOCACHE", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		got := envValues(sp.Env, k)
		if v, ok := want[k]; ok && (len(got) != 1 || got[0] != v) || !ok && len(got) > 0 {
			lacks = append(lacks, k)
		}
	}
	inner := sp.Argv[min(len(wall), len(sp.Argv)):]
	for _, f := range flags {
		if !hasRun(inner, f) {
			lacks = append(lacks, strings.Join(f, " "))
		}
	}
	if len(lacks) > 0 {
		return fmt.Errorf("agentd: %s runs walled, but its command lacks: %s", executor, strings.Join(lacks, "; "))
	}
	return nil
}

// hasRun: run appears in argv, its words next to each other.
func hasRun(argv, run []string) bool {
	for i := 0; i+len(run) <= len(argv); i++ {
		if slices.Equal(argv[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// afterExecutor is what agentd does with an attempt's output once its
// executor and its whole group are gone: codex's login, which codex may have
// refreshed, goes back to the host's (codexlogin.go), and the attempt's build
// caches and temporary files go (each attempt fills its own: TD-371). The
// codex home stays: measure reads its session. It may run again for the same
// attempt (a restart): what is gone stays gone.
func afterExecutor(executor, codexHome, out string, logf func(format string, args ...any)) {
	if executor == "exec:codex" && codexHome != "" {
		if err := keepCodexLogin(codexHome, codexHomeOf(out)); err != nil {
			logf("agentd: codex's login of %s is not kept: %v", out, err)
		}
	}
	r, err := os.OpenRoot(out)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logf("agentd: %s: %v", out, err)
		return
	}
	defer func() { _ = r.Close() }()
	for _, rel := range []string{"home/go", "home/.cache", "home/.npm", "tmp"} {
		if err := removeIn(r, rel); err != nil {
			logf("agentd: %s/%s: %v", out, rel, err)
		}
	}
}

// removeIn removes rel and what is beneath it, inside r only (a link an
// executor left on the way does not lead out), whatever the modes inside say
// (Go's module cache is read only).
func removeIn(r *os.Root, rel string) error {
	fi, err := r.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.IsDir() {
		if err = r.Chmod(rel, 0o700); err != nil {
			return err
		}
		d, oerr := r.Open(rel)
		if oerr != nil {
			return oerr
		}
		names, rerr := d.Readdirnames(-1)
		_ = d.Close()
		if rerr != nil {
			return rerr
		}
		for _, n := range names {
			if err = removeIn(r, filepath.Join(rel, n)); err != nil {
				return err
			}
		}
	}
	return r.Remove(rel)
}
