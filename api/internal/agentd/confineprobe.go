package agentd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proving the write wall on the host (TD-371): before agentd counts any
// executor as confined, it runs, inside the wall, a check that tries what an
// attempt must not be able to do, and codex's own sandbox inside the wall.
// The Owner's probe on the agent host (agentd confine-probe) runs the same
// and prints it.

// CheckArg runs the wall's checks (inside it): agentd confine-check.
const CheckArg = "confine-check"

// ProbeArg is the probe on the host: agentd confine-probe --root <dir>.
const ProbeArg = "confine-probe"

// proofLimit is how long one proof may take.
const proofLimit = time.Minute

// ProveWall finds whether the write wall holds on this host and for which
// executors, in a directory of its own under root, and says how each check
// went. It runs agentd confine-check inside agentd confine (self) with env:
// every write it tries outside the wall must be refused, and the file it
// tries them on must be unchanged after. Then, when codex is wanted, codex's
// sandbox inside the wall must run a command that writes its workspace, may
// not write the rest of the wall, and has no network. A nil Wall is no wall.
func ProveWall(ctx context.Context, self, root string, env []string, codex bool) (*Wall, []string) {
	abi, err := landlockABI()
	if err != nil {
		return nil, []string{err.Error()}
	}
	lines := []string{fmt.Sprintf("Landlock ABI %d", abi)}
	if abi < 3 {
		return nil, append(lines, "the wall needs ABI 3 (truncate)")
	}
	dir, err := os.MkdirTemp(root, "confine-proof-")
	if err != nil {
		return nil, append(lines, err.Error())
	}
	defer func() {
		if r, rerr := os.OpenRoot(root); rerr == nil {
			_ = removeIn(r, filepath.Base(dir))
			_ = r.Close()
		}
	}()
	in, outside := filepath.Join(dir, "in"), filepath.Join(dir, "outside")
	file := filepath.Join(outside, "file")
	for _, d := range []string{in, outside} {
		if err = os.Mkdir(d, 0o700); err != nil {
			return nil, append(lines, err.Error())
		}
	}
	if err = os.WriteFile(file, []byte("before\n"), 0o600); err != nil {
		return nil, append(lines, err.Error())
	}
	argv := append(wallArgv(self, []string{in}), self, CheckArg, "--in", in, "--outside", outside, "--file", file,
		"--pid", strconv.Itoa(os.Getpid()))
	out, err := runProof(ctx, withEnv(env, "TMPDIR", in), dir, argv)
	lines = append(lines, out...)
	body, rerr := os.ReadFile(file)
	entries, derr := os.ReadDir(outside)
	switch {
	case err != nil:
		return nil, append(lines, "the checks inside the wall: "+err.Error())
	case rerr != nil || string(body) != "before\n":
		return nil, append(lines, "the file outside the wall was changed")
	case derr != nil || len(entries) != 1:
		return nil, append(lines, "something was made outside the wall")
	}
	w := &Wall{ABI: abi, Executors: map[string]bool{"exec:deterministic": true, "exec:cursor": true, "exec:claude": true}}
	if codex {
		cx, cerr := proveCodex(ctx, self, dir, env)
		lines = append(lines, cx...)
		if cerr != nil {
			lines = append(lines, "codex's sandbox inside the wall: "+cerr.Error()+"; codex runs unwalled, alone")
		} else {
			w.Executors["exec:codex"] = true
		}
	}
	return w, lines
}

// proveCodex runs codex's sandbox, configured as codexExecutor configures it
// inside the wall, inside the wall; the check in it must find its workspace
// writable, the rest of the wall not, and no network.
func proveCodex(ctx context.Context, self, dir string, env []string) ([]string, error) {
	ws, home, tmp := filepath.Join(dir, "cx-ws"), filepath.Join(dir, "cx-home"), filepath.Join(dir, "cx-tmp")
	for _, d := range []string{ws, home, tmp, filepath.Join(home, ".codex")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, err
		}
	}
	argv := append(wallArgv(self, []string{ws, home, tmp}), "codex", "sandbox")
	for _, p := range append(append([]string{}, codexPins...), codexRoots(tmp), codexWalled) {
		argv = append(argv, "-c", p)
	}
	argv = append(argv, "-P", ":workspace", "-C", ws, "--", self, CheckArg, "--codex", "--in", ws, "--outside", home)
	env = withEnv(withEnv(withEnv(env, "HOME", home), "CODEX_HOME", filepath.Join(home, ".codex")), "TMPDIR", tmp)
	return runProof(ctx, env, ws, argv)
}

// runProof runs argv in dir within proofLimit and returns what it printed,
// line by line, and why it failed.
func runProof(ctx context.Context, env []string, dir string, argv []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, proofLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out := &ring{max: tailBytes}
	cmd.Env, cmd.Dir, cmd.Stdout, cmd.Stderr = env, dir, out, out
	err := cmd.Run()
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out.bytes())), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, oneLine(l, 300))
		}
	}
	return lines, err
}

// RunConfineCheck is agentd confine-check: inside the wall, it tries each
// thing an attempt must and must not be able to do, prints "ok" or "FAIL" for
// each, and exits 0 only when all went as they must.
func RunConfineCheck(args []string) int {
	fs := flag.NewFlagSet(CheckArg, flag.ContinueOnError)
	in := fs.String("in", "", "a directory inside the wall")
	outside := fs.String("outside", "", "a directory outside it")
	file := fs.String("file", "", "a file outside it")
	pid := fs.Int("pid", 0, "a process outside it")
	codex := fs.Bool("codex", false, "inside codex's sandbox: the workspace is in, the rest of the wall outside, no network")
	if err := fs.Parse(args); err != nil || *in == "" || *outside == "" || !*codex && (*file == "" || *pid == 0) {
		fmt.Fprintln(os.Stderr, "usage: agentd confine-check --in <dir> --outside <dir> [--file <file> --pid <pid> | --codex]")
		return 2
	}
	failed := false
	try := func(what string, want bool, err error) {
		switch {
		case want && err == nil, !want && err != nil:
			fmt.Printf("ok: %s\n", what)
		case want:
			failed = true
			fmt.Printf("FAIL: %s: %v\n", what, err)
		default:
			failed = true
			fmt.Printf("FAIL: %s was allowed\n", what)
		}
	}
	try("write inside", true, os.WriteFile(filepath.Join(*in, "new"), []byte("x"), 0o600))
	try("make a file outside", false, os.WriteFile(filepath.Join(*outside, "new"), []byte("x"), 0o600))
	if *codex {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
		if err == nil {
			_ = syscall.Close(fd)
		}
		try("open a network socket", false, err)
	} else {
		try("append to a file outside", false, appendTo(*file))
		try("truncate a file outside", false, os.Truncate(*file, 0))
		try("rename out of the wall", false, os.Rename(filepath.Join(*in, "new"), filepath.Join(*outside, "moved")))
		link := filepath.Join(*in, "hardlink")
		err := os.Link(*file, link)
		if err == nil {
			err = appendTo(link)
		}
		try("write through a hard link to a file outside", false, err)
		sym := filepath.Join(*in, "symlink")
		if err = os.Symlink(*file, sym); err == nil {
			err = appendTo(sym)
		}
		try("write through a symbolic link to a file outside", false, err)
		try("write through /proc/<pid>/root", false, appendTo(filepath.Join("/proc", strconv.Itoa(*pid), "root", *file)))
		_, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(*pid), "environ"))
		try("read /proc/<pid>/environ of a process outside", false, err)
		tmp := filepath.Join("/tmp", "agentd-confine-"+strconv.Itoa(os.Getpid()))
		err = os.WriteFile(tmp, []byte("x"), 0o600)
		if err == nil {
			_ = os.Remove(tmp)
		}
		try("make a file in /tmp", false, err)
		f, err := os.OpenFile(os.DevNull, os.O_WRONLY|os.O_TRUNC, 0)
		if err == nil {
			_ = f.Close()
		}
		try("write /dev/null", true, err)
	}
	if failed {
		return 1
	}
	return 0
}

func appendTo(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte("x"))
	return errors.Join(err, f.Close())
}

// RunConfineProbe is agentd confine-probe --root <dir>: the Owner's probe on
// the agent host, run as an agent user under its unit's confinement. It
// proves the wall as agentd does when it starts, codex included, prints each
// check, and exits 0 only when the wall holds for every executor.
func RunConfineProbe(args []string) int {
	fs := flag.NewFlagSet(ProbeArg, flag.ContinueOnError)
	root := fs.String("root", "", "agentd's directory (AGENTD_ROOT)")
	if err := fs.Parse(args); err != nil || *root == "" {
		fmt.Fprintln(os.Stderr, "usage: agentd confine-probe --root <dir>")
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	w, lines := ProveWall(context.Background(), self, *root, AllowedEnv(os.Environ()), true)
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	fmt.Print(b.String())
	for _, ex := range []string{"exec:deterministic", "exec:cursor", "exec:claude", "exec:codex"} {
		if !w.holds(ex) {
			fmt.Printf("agentd confine-probe: the wall does not hold for %s\n", ex)
			return 1
		}
	}
	fmt.Println("agentd confine-probe: the wall holds for every executor")
	return 0
}
