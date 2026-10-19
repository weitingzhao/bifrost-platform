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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proving the write wall on the host (TD-371): before agentd counts any
// executor as confined, it runs, inside the wall, a check that tries what an
// attempt must not be able to do, and the same kind of check in codex's own
// sandbox (codex runs outside the wall: confine.go).
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
// tries them on must be unchanged after. Then, when codex (the program, an
// absolute path) is given, its own sandbox, configured as codexExecutor
// configures it and outside the wall, must run a command that writes its
// workspace and its temporary directory, may write nowhere else, and has no
// network; the wall then records that program and the PATH it ran with,
// which a confined codex attempt must use. A nil Wall is no wall.
func ProveWall(ctx context.Context, self, root string, env []string, codex string) (*Wall, []string) {
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
	nonce, err := NewChangeID()
	if err != nil {
		return nil, append(lines, err.Error())
	}
	argv := append(wallArgv(self, []string{in}), self, CheckArg, "--in", in, "--outside", outside, "--file", file,
		"--pid", strconv.Itoa(os.Getpid()), "--nonce", nonce)
	out, err := runProof(ctx, withEnv(env, "TMPDIR", in), dir, argv)
	lines = append(lines, out...)
	body, rerr := os.ReadFile(file)
	entries, derr := os.ReadDir(outside)
	_, inErr := os.Stat(filepath.Join(in, "new"))
	switch {
	case err != nil:
		return nil, append(lines, "the checks inside the wall: "+err.Error())
	case !slices.Contains(out, checkDone+nonce):
		return nil, append(lines, "the checks inside the wall did not all run")
	case inErr != nil:
		return nil, append(lines, "nothing was written inside the wall")
	case rerr != nil || string(body) != "before\n":
		return nil, append(lines, "the file outside the wall was changed")
	case derr != nil || len(entries) != 1:
		return nil, append(lines, "something was made outside the wall")
	}
	w := &Wall{ABI: abi, Executors: map[string]bool{"exec:deterministic": true, "exec:cursor": true, "exec:claude": true}}
	if codex != "" {
		cx, cerr := codexHeld(ctx, codex, self, root, dir, env)
		lines = append(lines, cx...)
		if cerr != nil {
			lines = append(lines, "codex's own sandbox: "+cerr.Error()+"; codex runs alone")
		} else {
			w.Executors["exec:codex"] = true
			if p := envValues(env, "PATH"); len(p) > 0 {
				w.Codex, w.Path = codex, p[len(p)-1]
			}
		}
	}
	return w, lines
}

// proveCodex runs codex's own sandbox (the program codex) outside agentd's
// wall, configured as codexExecutor configures it (codexProofArgv); the check
// in it must write its workspace and its temporary directory, and nothing
// else: not the codex home beside them, not a file outside them, not /tmp;
// and it must be in a network namespace not the host's, where a connection
// to a documentation address over IPv4 (192.0.2.1:443) and over IPv6
// (2001:db8::1) is refused or unreachable, while this process can start the
// IPv4 one (the control: on a host with no route the check would prove
// nothing). That is what is proven of the network, not every route or
// protocol (review 305 B).
func proveCodex(ctx context.Context, codex, self, dir string, env []string) ([]string, error) {
	ws, home, tmp, outside := filepath.Join(dir, "cx-ws"), filepath.Join(dir, "cx-home"), filepath.Join(dir, "cx-tmp"), filepath.Join(dir, "cx-outside")
	for _, d := range []string{ws, home, tmp, outside, filepath.Join(home, ".codex")} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, err
		}
	}
	file := filepath.Join(outside, "file")
	if err := os.WriteFile(file, []byte("before\n"), 0o600); err != nil {
		return nil, err
	}
	if err := routeOut(); !errors.Is(err, syscall.EINPROGRESS) && err != nil {
		return nil, fmt.Errorf("this host has no route out to test the sandbox against: %v", err)
	}
	netns, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("this host's network namespace: %w", err)
	}
	nonce, err := NewChangeID()
	if err != nil {
		return nil, err
	}
	argv := codexProofArgv(codex, self, ws, home, tmp, file, netns, nonce)
	env = withEnv(withEnv(withEnv(env, "HOME", home), "CODEX_HOME", filepath.Join(home, ".codex")), "TMPDIR", tmp)
	lines, err := runProof(ctx, env, ws, argv)
	_, inErr := os.Stat(filepath.Join(ws, "new"))
	_, tmpErr := os.Stat(filepath.Join(tmp, "new"))
	_, outErr := os.Lstat(filepath.Join(home, "new"))
	body, rerr := os.ReadFile(file)
	switch {
	case err != nil:
	case !slices.Contains(lines, checkDone+nonce):
		err = errors.New("the check in codex's sandbox did not all run")
	case inErr != nil || tmpErr != nil:
		err = errors.New("codex's workspace or temporary directory was not written")
	case outErr == nil || rerr != nil || string(body) != "before\n":
		err = errors.New("codex's sandbox wrote outside its workspace")
	}
	return lines, err
}

// codexHeld proves codex's own sandbox (proveCodex), once codex and its
// PATH are outside agentd's directory (pathOutside).
func codexHeld(ctx context.Context, codex, self, root, dir string, env []string) ([]string, error) {
	if err := pathOutside(root, codex, env); err != nil {
		return nil, err
	}
	return proveCodex(ctx, codex, self, dir, env)
}

// pathOutside says why codex, or a directory of the PATH in env it runs
// with, may be in agentd's directory root, where attempts write: there a
// setup could put a program the proof did not run (a node for codex's
// wrapper, review 306 A). Links are followed as far as a path exists.
func pathOutside(root, codex string, env []string) error {
	dirs := []string{filepath.Dir(codex)}
	if p := envValues(env, "PATH"); len(p) > 0 {
		dirs = append(dirs, filepath.SplitList(p[len(p)-1])...)
	}
	roots := []string{filepath.Clean(root), resolvedAncestor(root)}
	for _, d := range dirs {
		for _, c := range []string{filepath.Clean(d), resolvedAncestor(d)} {
			for _, r := range roots {
				if rel, err := filepath.Rel(r, c); err == nil && (rel == "." || filepath.IsLocal(rel)) {
					return fmt.Errorf("%s, of codex's PATH, is in agentd's directory, where attempts write", d)
				}
			}
		}
	}
	return nil
}

// resolvedAncestor is p with its longest existing ancestor's links resolved
// and the rest, which does not exist yet, as it is.
func resolvedAncestor(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; cur = filepath.Dir(cur) {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		if cur == filepath.Dir(cur) {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

// codexProofArgv is codex's sandbox with codexExecutor's pins, running the
// check: no wall of agentd's around it.
func codexProofArgv(codex, self, ws, home, tmp, file, netns, nonce string) []string {
	argv := []string{codex, "sandbox"}
	for _, p := range append(append([]string{}, codexPins...), codexRoots(tmp)) {
		argv = append(argv, "-c", p)
	}
	return append(argv, "-P", ":workspace", "-C", ws, "--", self, CheckArg, "--codex", "--in", ws, "--tmp", tmp,
		"--outside", home, "--file", file, "--netns", netns, "--nonce", nonce)
}

// routeOut starts a TCP connection to an address of the documentation range
// (192.0.2.1:443), without waiting: EINPROGRESS (or a connection) when there
// is a route out, a refusal or "network is unreachable" when there is none.
func routeOut() error {
	return connectNow(syscall.AF_INET, &syscall.SockaddrInet4{Port: 443, Addr: [4]byte{192, 0, 2, 1}})
}

// routeOut6 is routeOut over IPv6 (2001:db8::1, port 443).
func routeOut6() error {
	return connectNow(syscall.AF_INET6, &syscall.SockaddrInet6{Port: 443, Addr: [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}})
}

func connectNow(family int, sa syscall.Sockaddr) error {
	fd, err := syscall.Socket(family, syscall.SOCK_STREAM, 0)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Close(fd) }()
	syscall.CloseOnExec(fd)
	if err = syscall.SetNonblock(fd, true); err != nil {
		return err
	}
	return syscall.Connect(fd, sa)
}

// noRoute: err says there is no way out (no route, no such address family,
// or the socket refused).
func noRoute(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) || denied(err)
}

// checkDone starts the last line of a check that ran every test.
const checkDone = "confine-check done "

// denied: err is a refusal (Landlock's EACCES or EXDEV, a read-only mount's
// EROFS, seccomp's EPERM), not a failure for another reason (a path that is
// not there).
func denied(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EXDEV) ||
		errors.Is(err, syscall.EROFS)
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
	codex := fs.Bool("codex", false, "inside codex's sandbox: the workspace and --tmp are in, --outside is not, no route out")
	tmp := fs.String("tmp", "", "with --codex: the sandbox's temporary directory, writable")
	netns := fs.String("netns", "", "with --codex: the network namespace of the host, which the sandbox's must not be")
	nonce := fs.String("nonce", "", "printed on the last line once every test ran")
	if err := fs.Parse(args); err != nil || *in == "" || *outside == "" || *file == "" || !*codex && *pid == 0 || *codex && (*tmp == "" || *netns == "") {
		fmt.Fprintln(os.Stderr, "usage: agentd confine-check --in <dir> --outside <dir> --file <file> [--pid <pid> | --codex --tmp <dir> --netns <ns>] [--nonce <word>]")
		return 2
	}
	failed := false
	try := func(what string, want bool, err error) {
		switch {
		case want && err == nil, !want && denied(err):
			fmt.Printf("ok: %s\n", what)
		case want:
			failed = true
			fmt.Printf("FAIL: %s: %v\n", what, err)
		case err != nil:
			failed = true
			fmt.Printf("FAIL: %s failed, but not as a refusal: %v\n", what, err)
		default:
			failed = true
			fmt.Printf("FAIL: %s was allowed\n", what)
		}
	}
	try("write inside", true, os.WriteFile(filepath.Join(*in, "new"), []byte("x"), 0o600))
	try("make a file outside", false, os.WriteFile(filepath.Join(*outside, "new"), []byte("x"), 0o600))
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
	// /tmp is refused, private or not (review 304 A1).
	tmpFile := filepath.Join("/tmp", "agentd-confine-"+strconv.Itoa(os.Getpid()))
	err = os.WriteFile(tmpFile, []byte("x"), 0o600)
	if err == nil {
		_ = os.Remove(tmpFile)
	}
	try("make a file in /tmp", false, err)
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY|os.O_TRUNC, 0)
	if err == nil {
		_ = f.Close()
	}
	try("write /dev/null", true, err)
	if *codex {
		try("write the temporary directory", true, os.WriteFile(filepath.Join(*tmp, "new"), []byte("x"), 0o600))
		// No network: a network namespace not the host's, with no route out
		// over IPv4 or IPv6 (review 304 B1); a connection started is a route.
		if own, nerr := os.Readlink("/proc/self/ns/net"); nerr != nil || own == *netns {
			failed = true
			fmt.Printf("FAIL: the host's network namespace (%s, %v)\n", own, nerr)
		} else {
			fmt.Println("ok: a network namespace of its own")
		}
		for _, r := range []struct {
			name  string
			route func() error
		}{{"IPv4", routeOut}, {"IPv6", routeOut6}} {
			switch rerr := r.route(); {
			case noRoute(rerr):
				fmt.Printf("ok: no %s route out\n", r.name)
			case rerr == nil, errors.Is(rerr, syscall.EINPROGRESS):
				failed = true
				fmt.Printf("FAIL: an %s route out\n", r.name)
			default:
				failed = true
				fmt.Printf("FAIL: an %s route out failed, but not as a refusal: %v\n", r.name, rerr)
			}
		}
	} else {
		try("write through /proc/<pid>/root", false, appendTo(filepath.Join("/proc", strconv.Itoa(*pid), "root", *file)))
		_, err = os.ReadFile(filepath.Join("/proc", strconv.Itoa(*pid), "environ"))
		try("read /proc/<pid>/environ of a process outside", false, err)
	}
	fmt.Println(checkDone + *nonce)
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
// check, and exits 0 only when every executor is held: by the wall, codex by
// its own sandbox.
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
	codex := CodexBin(os.Environ())
	w, lines := ProveWall(context.Background(), self, *root, AllowedEnv(os.Environ()), codex)
	if codex == "" {
		lines = append(lines, "no codex on PATH")
	}
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	fmt.Print(b.String())
	for _, ex := range []string{"exec:deterministic", "exec:cursor", "exec:claude", "exec:codex"} {
		if !w.holds(ex) {
			fmt.Printf("agentd confine-probe: %s is not held to its own directories\n", ex)
			return 1
		}
	}
	fmt.Println("agentd confine-probe: every executor is held to its own directories (codex by its own sandbox)")
	return 0
}
