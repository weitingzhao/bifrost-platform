package agentd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
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
//
// codex is the exception (Owner 2026-10-19, option A of the cut's §10):
// codex 0.163 sandboxes its commands only with bubblewrap, which cannot
// mount inside a Landlock domain, so codex runs outside agentd's wall, its
// commands in its own sandbox with the pinned writable roots and no network.
// It shares the host when ProveWall proved that sandbox on this host; what
// the codex process itself writes rests on codex's own checks (a residual,
// the cut's §7). Its setup still runs each command in the narrower wall.

// ConfineArg is the first argument that makes agentd run argv inside the
// write wall.
const ConfineArg = "confine"

// Wall is what ProveWall found on this host: the kernel's Landlock ABI and
// the executors that are held to their own directories: inside the wall, or
// for codex in its own sandbox (selfSandboxed). A nil Wall holds nothing.
type Wall struct {
	ABI       int
	Executors map[string]bool
	// Codex is the codex program whose sandbox was proven, an absolute path,
	// and Path the PATH it was proven with: a confined codex attempt runs
	// exactly these (checkPins, review 304 A2).
	Codex, Path string
}

// CodexBin is the codex program PATH in environ finds, an absolute path, or
// "" when there is none.
func CodexBin(environ []string) string {
	v := envValues(environ, "PATH")
	if len(v) == 0 {
		return ""
	}
	for _, dir := range filepath.SplitList(v[len(v)-1]) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, "codex")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// selfSandboxed are the executors held by a sandbox of their own, proven on
// the host, instead of by agentd's wall.
var selfSandboxed = map[string]bool{"exec:codex": true}

// holds: the executor's attempts are held to their own directories, and so
// may share the host.
func (w *Wall) holds(executor string) bool { return w != nil && w.Executors[executor] }

// wraps: the executor's attempts run inside agentd's wall.
func (w *Wall) wraps(executor string) bool { return w.holds(executor) && !selfSandboxed[executor] }

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

// An attempt's own directories, under its output. The home is the
// executor's (its configuration: CODEX_HOME, CLAUDE_CONFIG_DIR, cursor's
// ~/.cursor), which the setup and the checked code may not write; the
// temporary directory holds what they share with it: TMPDIR, the Go caches
// (codex's commands may write there too) and the setup's own home.
func attemptHome(out string) string { return filepath.Join(out, "home") }
func attemptTmp(out string) string  { return filepath.Join(out, "tmp") }
func setupHome(out string) string   { return filepath.Join(attemptTmp(out), "home") }
func codexHomeOf(out string) string { return filepath.Join(attemptHome(out), ".codex") }
func claudeDirOf(out string) string { return filepath.Join(attemptHome(out), ".claude") }

// claudeTmpOf is claude's temporary root (CLAUDE_CODE_TMPDIR), in the
// executor's home: not in the temporary directory, which the setup and the
// checked code may write too, so that nothing they leave is where claude
// keeps its own files.
func claudeTmpOf(out string) string { return filepath.Join(attemptHome(out), "tmp") }

// homePins are the variables every executor's environment has set to the
// attempt's own directories.
func homePins(out string) [][2]string {
	tmp := attemptTmp(out)
	return [][2]string{{"HOME", attemptHome(out)}, {"TMPDIR", tmp}, {"GOPATH", filepath.Join(tmp, "go")},
		{"GOMODCACHE", filepath.Join(tmp, "go", "pkg", "mod")}, {"GOCACHE", filepath.Join(tmp, "go-build")}}
}

// cursorDataDir is cursor's data directory for the attempt whose output is
// out (CURSOR_DATA_DIR): a short directory of the attempt's own under root,
// named by a digest of out. cursor puts each workspace's sockets under
// <data dir>/projects when that path is at most cursorPathMax characters,
// and otherwise falls back, through $TMPDIR/.cursor, to /tmp/.cursor, which
// the wall refuses and which every attempt would share (cursor-agent
// 2026.10.16, measured on STG 2026-10-19: the attempt's home under the tasks
// tree is 90-odd characters). With no root (a test) it is in out.
func cursorDataDir(root, out string) string {
	if root == "" {
		return filepath.Join(out, "cursor")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(out)))
	return filepath.Join(root, "s", hex.EncodeToString(sum[:])[:24])
}

// cursorPathMax is the longest base cursor-agent takes for its sockets'
// directories (its projects directory).
const cursorPathMax = 83

// executorWrites are the directories the wall lets an attempt's executor
// write: its checkout, its output, its siblings, and for cursor its data
// directory.
func executorWrites(executor string, a *Attempt, sibs []string) []string {
	w := append([]string{a.Dir, a.Out}, sibs...)
	if executor == "exec:cursor" {
		w = append(w, cursorDataDir(a.root, a.Out))
	}
	return w
}

// innerWrites are what the code an attempt runs besides its executor (the
// setup, the deterministic check's commands) may write when it is walled:
// the checkout, the siblings and the temporary directory; not the executor's
// home, nor the attempt's output (review 300 A3).
func innerWrites(a *Attempt, sibs []string) []string {
	return append(append([]string{a.Dir}, sibs...), attemptTmp(a.Out))
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

// walledVerify is Verify with each command inside a wall over writes (none:
// as it is); the trace keeps each command as the spec wrote it.
func walledVerify(ctx context.Context, env []string, cmds [][]string, writes []string, limit time.Duration) (Verification, error) {
	run := cmds
	if len(writes) > 0 {
		self, err := os.Executable()
		if err != nil {
			return Verification{}, err
		}
		run = make([][]string, len(cmds))
		for k, c := range cmds {
			run[k] = append(wallArgv(self, writes), c...)
		}
	}
	v := Verify(ctx, ".", env, run, limit)
	for k := range v.Commands {
		v.Commands[k].Argv = cmds[k]
	}
	return v, nil
}

// envKeys are the variables an executor's environment may hold: the allowed
// ones and the executor's own (its credential, its configuration).
var envKeys = map[string][]string{
	"exec:codex":         nil,
	"exec:deterministic": nil,
	"exec:claude":        {"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_TMPDIR", "DISABLE_AUTOUPDATER", "CLAUDE_CODE_OAUTH_TOKEN"},
	"exec:cursor":        {"CURSOR_API_KEY", "AGENT_CLI_CREDENTIAL_STORE", "CURSOR_DATA_DIR"},
}

// checkPins says what the spawn of a confined attempt lacks: the wall over
// exactly the attempt's workspace, output and siblings, outermost (but for
// an executor in a sandbox of its own, which has none); the
// setup, when there is one, with its own home, its narrower wall and, for
// cursor, its refusals; the executor's command exactly as its builder makes
// it (no flag added, none missing, none repeated); and an environment of
// the allowed variables and the executor's own only, each once, with
// homePins and the executor's configuration directory the attempt's own. An
// executor this does not know has no pins.
func checkPins(w *Wall, executor, self string, a *Attempt, sibs []string, sp Spawn) error {
	keys, known := envKeys[executor]
	if !known {
		return fmt.Errorf("agentd: %s has no pins to hold it to its workspace", executor)
	}
	var lacks []string
	if sp.Dir != a.Dir {
		lacks = append(lacks, "its workspace as its directory")
	}
	argv := sp.Argv
	if !selfSandboxed[executor] {
		wall := wallArgv(self, executorWrites(executor, a, sibs))
		if !hasPrefix(argv, wall) {
			lacks = append(lacks, "the wall over its own directories")
		} else {
			argv = argv[len(wall):]
		}
	}
	if len(argv) > 1 && argv[0] == self && argv[1] == SetupArg {
		o := setupOpts{Home: setupHome(a.Out), Writes: innerWrites(a, sibs)}
		if executor == "exec:cursor" {
			o.Refuse = cursorRefused
		}
		if setup := setupArgv(self, a.Out, o); hasPrefix(argv, setup) {
			argv = argv[len(setup):]
		} else {
			lacks = append(lacks, "the setup's own home and wall")
		}
	}
	codex := ""
	if w != nil {
		codex = w.Codex
	}
	if why := executorArgv(executor, self, codex, a, sibs, argv); why != "" {
		lacks = append(lacks, why)
	}
	want := map[string]string{}
	for _, kv := range homePins(a.Out) {
		want[kv[0]] = kv[1]
	}
	switch executor {
	case "exec:codex":
		// The program and the PATH its sandbox was proven with: a PATH of the
		// attempt's could find another codex, or another node for it.
		want["CODEX_HOME"] = codexHomeOf(a.Out)
		if w == nil || w.Path == "" {
			lacks = append(lacks, "a PATH its sandbox was proven with")
		} else {
			want["PATH"] = w.Path
		}
	case "exec:claude":
		want["CLAUDE_CONFIG_DIR"], want["CLAUDE_CODE_TMPDIR"], want["DISABLE_AUTOUPDATER"] = claudeDirOf(a.Out), claudeTmpOf(a.Out), "1"
	case "exec:cursor":
		want["CURSOR_DATA_DIR"] = cursorDataDir(a.root, a.Out)
		if len(envValues(sp.Env, "AGENT_CLI_CREDENTIAL_STORE")) > 0 {
			want["AGENT_CLI_CREDENTIAL_STORE"] = "memory"
		}
	}
	seen := map[string]bool{}
	for _, kv := range sp.Env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case seen[k]:
			lacks = append(lacks, k+" once")
		case !allowedEnv[k] && !slices.Contains(keys, k):
			lacks = append(lacks, "no "+k)
		case want[k] != "" && v != want[k], k == "CODEX_HOME" && executor != "exec:codex":
			lacks = append(lacks, k+" its own")
		}
		seen[k] = true
	}
	for k := range want {
		if !seen[k] {
			lacks = append(lacks, k)
		}
	}
	if len(lacks) > 0 {
		slices.Sort(lacks)
		return fmt.Errorf("agentd: %s is confined, but its command lacks: %s", executor, strings.Join(slices.Compact(lacks), "; "))
	}
	return nil
}

func hasPrefix(argv, prefix []string) bool {
	return len(argv) >= len(prefix) && slices.Equal(argv[:len(prefix)], prefix)
}

// executorArgv says how argv is not the executor's command as its builder
// makes it ("" when it is).
func executorArgv(executor, self, codex string, a *Attempt, sibs []string, argv []string) string {
	model := a.Claim.Tool.Model
	switch executor {
	case "exec:deterministic":
		w := *a
		w.inner = innerWrites(a, sibs)
		if !slices.Equal(argv, verifyArgv(self, &w)) {
			return "the deterministic check as agentd runs it, inside its own wall"
		}
	case "exec:claude":
		// An implementer's tools only inside the wall (claudeExecutor).
		if claudeWriter(a) && !a.Walled {
			return "the wall around claude's edits and commands"
		}
		want := []string{"claude", "-p", "--output-format", "stream-json"}
		for _, p := range claudePins {
			want = append(want, p...)
		}
		want = append(want, claudeTools(claudeWriter(a))...)
		if model != "" {
			want = append(want, "--model", model)
		}
		if !slices.Equal(argv, append(want, "--", Prompt(a))) {
			return "claude's command as agentd makes it"
		}
	case "exec:cursor":
		want := []string{"cursor-agent", "-p", "--output-format", "stream-json", "--trust", "--workspace", a.Dir}
		if a.Claim.EffectClass != "branch" {
			want = append(want, "--mode", "ask")
		}
		if model != "" {
			want = append(want, "--model", model)
		}
		if !slices.Equal(argv, append(want, Prompt(a))) {
			return "cursor's command as agentd makes it"
		}
	case "exec:codex":
		return codexArgv(a, codex, argv)
	}
	return ""
}

// codexArgv: argv is codex exec in workspace-write with saved rules
// ignored, every -c of codexPins and its writable roots once and
// no other, its report where agentd reads it, the claim's model, and the
// prompt last.
func codexArgv(a *Attempt, codex string, argv []string) string {
	if codex == "" {
		return "a codex whose sandbox was proven"
	}
	head := []string{codex, "exec", "--json", "--skip-git-repo-check", "-C", a.Dir, "-s", "workspace-write", "--ignore-rules"}
	if !hasPrefix(argv, head) || len(argv) < len(head)+1 || argv[len(argv)-1] != Prompt(a) {
		return "codex exec in workspace-write, rules ignored, the prompt last"
	}
	want := map[string]bool{codexRoots(attemptTmp(a.Out)): false}
	for _, p := range codexPins {
		want[p] = false
	}
	report, model := false, a.Claim.Tool.Model == ""
	rest := argv[len(head) : len(argv)-1]
	for i := 0; i < len(rest); i += 2 {
		if i+1 >= len(rest) {
			return "codex's flags in pairs"
		}
		flag, v := rest[i], rest[i+1]
		switch done, ok := want[v]; {
		case flag == "-c" && ok && !done:
			want[v] = true
		case flag == "-o" && !report && v == filepath.Join(a.Out, "report.md"):
			report = true
		case flag == "-m" && !model && v == a.Claim.Tool.Model:
			model = true
		default:
			return "codex's flags as agentd sets them (" + flag + " " + v + ")"
		}
	}
	for v, done := range want {
		if !done {
			return "-c " + v
		}
	}
	if !report || !model {
		return "codex's report and model"
	}
	return ""
}

// afterExecutor is what agentd does with an attempt's output once its
// executor and its whole group are gone: codex's login, which codex may have
// refreshed, goes back to the host's (codexlogin.go), and the attempt's
// temporary directory (its build caches among it) and npm and other caches
// go (each attempt fills its own: TD-371). The codex home stays: measure
// reads its session. The output is reached from root, agentd's own
// directory, and only when it is the directory it should be: an earlier run
// may have left a link in its place (review 300 A1). It may run again for
// the same attempt (a restart): what is gone stays gone.
func afterExecutor(executor, codexHome, root, out string, logf func(format string, args ...any)) {
	removeCursorData(root, out, logf)
	r, err := openWithin(root, out)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logf("agentd: %s: %v", out, err)
		return
	}
	defer func() { _ = r.Close() }()
	if executor == "exec:codex" && codexHome != "" {
		if err := keepCodexLogin(codexHome, r); err != nil {
			logf("agentd: codex's login of %s is not kept: %v", out, err)
		}
	}
	// home/.local: what cursor's updater downloads into the attempt's home
	// (review 360); home/tmp: claude's temporary root.
	for _, rel := range []string{"tmp", "home/.npm", "home/.cache", "home/.local", "home/tmp"} {
		if err := removeIn(r, rel); err != nil {
			logf("agentd: %s/%s: %v", out, rel, err)
		}
	}
}

// removeCursorData removes cursor's data directory for the attempt whose
// output is out, through agentd's directory root (links on the way not
// followed), whether or not the output is still there; none is fine. It runs
// once the attempt's group is gone, or when the attempt never started.
func removeCursorData(root, out string, logf func(format string, args ...any)) {
	if root == "" || out == "" {
		return
	}
	d := cursorDataDir(root, out)
	s, err := openWithin(root, filepath.Dir(d))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logf("agentd: %s: %v", filepath.Dir(d), err)
		return
	}
	defer func() { _ = s.Close() }()
	if err = removeIn(s, filepath.Base(d)); err != nil {
		logf("agentd: %s: %v", d, err)
	}
}

// openWithin opens out, a directory inside root, through root, one step at
// a time: each step a directory and not a link, and the directory opened the
// one looked at, so a step swapped meanwhile is not followed (review 301).
// agentd reaches an attempt's checkout and output this way wherever it
// writes there outside the wall (review 302).
func openWithin(root, out string) (*os.Root, error) {
	rel, err := filepath.Rel(root, out)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("agentd: %s is not inside %s", out, root)
	}
	cur, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	for _, step := range strings.Split(rel, string(filepath.Separator)) {
		next, serr := openStep(cur, step)
		_ = cur.Close()
		if serr != nil {
			return nil, serr
		}
		cur = next
	}
	return cur, nil
}

// openStep opens the directory name directly in r, not a link, the one
// looked at.
func openStep(r *os.Root, name string) (*os.Root, error) {
	fi, err := r.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("agentd: %s in %s is not a directory", name, r.Name())
	}
	next, err := r.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	if st, serr := next.Stat("."); serr != nil || !os.SameFile(fi, st) {
		_ = next.Close()
		return nil, fmt.Errorf("agentd: %s in %s changed while it was opened", name, r.Name())
	}
	return next, nil
}

// ownDir opens dir, an attempt's directory, inside root (openWithin); with
// no root (a test), as it is.
func ownDirAt(root, dir string) (*os.Root, error) {
	if root == "" {
		return os.OpenRoot(dir)
	}
	return openWithin(root, dir)
}

// writeOwn puts body in r's name as a new file, replacing whatever the
// executor left under that name (a link, a hard link, a FIFO) without
// writing through it: agentd writes there outside the wall (review 301).
func writeOwn(r *os.Root, name string, body []byte) error {
	return replaceIn(r, name, func(f *os.File) error {
		_, err := f.Write(body)
		return err
	})
}

// appendOwn adds text to r's name, as a new file holding the old one's
// content and text: the old file, which may share its inode with another
// (a hard link), is never written (review 302). An old name that is not a
// regular file directly in r is not read.
func appendOwn(r *os.Root, name, text string) error {
	return replaceIn(r, name, func(f *os.File) error {
		if fi, lerr := r.Lstat(name); lerr == nil {
			if !fi.Mode().IsRegular() {
				return fmt.Errorf("agentd: %s is not a regular file", name)
			}
			old, oerr := r.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if oerr != nil {
				return oerr
			}
			st, serr := old.Stat()
			if serr == nil && !os.SameFile(fi, st) {
				serr = fmt.Errorf("agentd: %s changed while it was opened", name)
			}
			if serr == nil {
				_, serr = io.Copy(f, old)
			}
			_ = old.Close()
			if serr != nil {
				return serr
			}
		}
		_, err := f.WriteString(text)
		return err
	})
}

// replaceIn writes a new file in r with fill and renames it over name.
func replaceIn(r *os.Root, name string, fill func(*os.File) error) error {
	tmp, err := NewChangeID()
	if err != nil {
		return err
	}
	tmp = ".agentd-" + tmp
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = fill(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = r.Rename(tmp, name)
	}
	if err != nil {
		_ = r.Remove(tmp)
	}
	return err
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
