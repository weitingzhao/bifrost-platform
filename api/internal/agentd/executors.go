package agentd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// The executors of round 2 (design v5 §5.2 「执行者」): deterministic (the
// spec's verify commands, run by agentd itself), codex, cursor and claude, with the
// flags their --help gives on the host (10-13; claude 10-15). Each runs with an
// environment rebuilt from an allow list: no admin kubeconfig, no operator or
// admin token, no business key reaches a task. Codex and cursor are offered
// only where the host user has no MCP servers (mcpguard.go); claude ignores
// the host's MCP configuration and has only read-only tools, but for an
// implementer's attempt inside agentd's wall.

// allowedEnv are the variables an executor may see.
var allowedEnv = map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "LANG": true,
	"LC_ALL": true, "LC_CTYPE": true, "TMPDIR": true, "TERM": true, "GOPATH": true, "GOCACHE": true, "GOMODCACHE": true,
	"CODEX_HOME": true}

// AllowedEnv keeps only the allowed variables of environ, and of PATH only
// its absolute directories: an empty or relative entry is looked up from
// whatever directory a command runs in, a checkout among them, so the same
// PATH could find another program there (codex's node, review 305 A).
func AllowedEnv(environ []string) []string {
	out := []string{}
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		if !allowedEnv[k] {
			continue
		}
		if k == "PATH" {
			kv = "PATH=" + absolutePath(v)
		}
		out = append(out, kv)
	}
	return out
}

// absolutePath is the absolute directories of a PATH, in order.
func absolutePath(path string) string {
	var dirs []string
	for _, d := range filepath.SplitList(path) {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	return strings.Join(dirs, string(filepath.ListSeparator))
}

// Prompt is what an agent executor is asked: the spec's text, for an
// implementation what the checks of its task's earlier rounds failed on
// (TD-405), and how the result is handed over.
func Prompt(a *Attempt) string {
	var b strings.Builder
	b.WriteString(a.Spec.Text)
	writePriorChecks(&b, a.Failed)
	if a.Spec.Check != "" && a.Subject != nil {
		fmt.Fprintf(&b, "\n\n---\nYou are checking task %s, attempt %s (round %d), at commit %s of %s, checked out here. Its spec:\n\n%s\n",
			a.Spec.SubjectTask, a.Spec.Attempt, a.Spec.Round, a.Spec.Commit, a.Spec.Repo, a.Subject.Text)
		if len(a.VerifyReport) > 0 {
			fmt.Fprintf(&b, "\nIts deterministic check passed; its verify.json:\n\n%s\n", a.VerifyReport)
		}
	}
	b.WriteString("\n\n---\nWork only in this directory. Your final message is your report, in markdown: what you did, what you ran and how it ended, and what is left. Do not write paths of this machine into the report.\n")
	// R2-2 (STG walks 2 and 3, 2026-10-19): told to keep caches out of the
	// checkout, codex made new empty ones and its offline build failed. What
	// the caches hold depends on the task's setup (review 405 B1).
	// Only codex's commands have no network (its sandbox); cursor's and
	// claude's have the host's (review 422 B1).
	noNetwork := ""
	if a.Claim != nil && a.Claim.Tool.Executor == "exec:codex" {
		noNetwork = " (your commands have no network to fetch it again)"
	}
	b.WriteString("Go's cache locations (GOCACHE, GOMODCACHE) are set for this attempt, outside this directory; keep them as they are, so that whatever the task's setup fetched is used" + noNetwork + ", unless the task asks for a fresh cache.\n")
	if a.Spec.Check == "review" {
		fmt.Fprintf(&b, "End the report with exactly one line: %q if the work is correct and complete as its spec asks, or %q if anything must change. A report without that line counts as no verdict.\n",
			VerdictPass, VerdictFail)
	}
	return b.String()
}

// Secrets are credentials agentd gives one executor only.
type Secrets struct {
	// CursorAPIKey reaches cursor alone, as CURSOR_API_KEY: cursor keeps its
	// login in the macOS keychain, which a user that never logged in to the
	// desktop does not have, and a daemon has no session to unlock (10-15).
	CursorAPIKey string
	// ClaudeOAuthToken reaches claude alone as CLAUDE_CODE_OAUTH_TOKEN,
	// avoiding the daemon user's locked or absent macOS keychain as well.
	ClaudeOAuthToken string
}

// cursorMemoryStore keeps cursor from the keychain when it has a key: even
// given one, cursor-agent (2026.10.01) saves the login the key buys in the
// keychain by default and exits when it cannot (measured 10-15 as the
// daemon's user: "couldn't save your login to the macOS keychain (security
// exit code 154)"). Kept in memory, each run logs in with the key again.
const cursorMemoryStore = "AGENT_CLI_CREDENTIAL_STORE=memory"

// Executors are the executors this host runs, by actor id.
func Executors(self string, environ []string, s Secrets) map[string]Executor {
	env := AllowedEnv(environ)
	cursorEnv := env
	if s.CursorAPIKey != "" {
		cursorEnv = append(append([]string{}, env...), "CURSOR_API_KEY="+s.CursorAPIKey, cursorMemoryStore)
	}
	claudeEnv := env
	if s.ClaudeOAuthToken != "" {
		claudeEnv = append(append([]string{}, env...), "CLAUDE_CODE_OAUTH_TOKEN="+s.ClaudeOAuthToken)
	}
	ex := map[string]Executor{
		"exec:deterministic": deterministicExecutor(self, env),
		"exec:codex":         codexExecutor(env, CodexHome(environ), CodexBin(environ)),
		"exec:cursor":        cursorExecutor(cursorEnv),
		"exec:claude":        claudeExecutor(claudeEnv),
	}
	for name, e := range ex {
		ex[name] = keyless(name, e)
	}
	return ex
}

// keyless is an executor whose spawn is refused when its environment holds
// a name on the deny list (T13): whatever its builder did, no business key
// reaches a task.
func keyless(name string, e Executor) Executor {
	return func(a *Attempt) (Spawn, error) {
		sp, err := e(a)
		if err != nil {
			return Spawn{}, err
		}
		if err = noBusinessKeys(name, sp.Env); err != nil {
			return Spawn{}, err
		}
		return sp, nil
	}
}

func deterministicExecutor(self string, env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		spec := a.Spec
		if a.Subject != nil {
			spec = *a.Subject
		}
		body, err := json.Marshal(spec)
		if err != nil {
			return Spawn{}, err
		}
		path := filepath.Join(a.Out, "verify-spec.json")
		if err = os.WriteFile(path, body, 0o600); err != nil {
			return Spawn{}, err
		}
		own, err := attemptEnv(env, a.Out)
		if err != nil {
			return Spawn{}, err
		}
		return Spawn{Argv: verifyArgv(self, a), Env: own, Dir: a.Dir}, nil
	}
}

// verifyArgv is the deterministic executor's command. Walled, each verify
// command, which runs the checked code, runs inside a narrower wall (the
// checkout, the siblings, the temporary directory), so that it cannot write
// the attempt's output, verify.json among it (TD-371, review 300).
func verifyArgv(self string, a *Attempt) []string {
	argv := []string{self, "verify", "--spec", filepath.Join(a.Out, "verify-spec.json"), "--out", filepath.Join(a.Out, "verify.json")}
	if a.Walled {
		for _, w := range a.inner {
			argv = append(argv, "--write", w)
		}
	}
	return argv
}

// codexPins are the configuration codex always runs with, whatever the host
// user's says (TD-371), but for its writable roots (codexRoots): no notify
// command, no lifecycle hooks (each could run a command outside the sandbox,
// review 283); never asking for approval; the built-in provider, not one
// whose auth command a configuration names; neither /tmp nor $TMPDIR
// writable.
var codexPins = []string{"features.hooks=false", `approval_policy="never"`, `model_provider="openai"`,
	"sandbox_workspace_write.exclude_slash_tmp=true", "sandbox_workspace_write.exclude_tmpdir_env_var=true", "notify=[]"}

// codexRoots: the workspace and the attempt's own temporary directory are
// all codex's commands may write. The path goes into TOML as a Go-quoted
// string.
func codexRoots(tmp string) string {
	return "sandbox_workspace_write.writable_roots=[" + strconv.Quote(tmp) + "]"
}

// codexExecutor runs codex in its workspace-write sandbox with codexPins,
// with saved exec rules ignored, and with a codex home of the attempt's own
// that holds only the host's login (codexlogin.go): a project's
// .codex/config.toml applies only to a project the home trusts, and this one
// trusts none (measured 10-19, codex 0.162: a checkout's MCP server is not
// loaded with a new home, and is with the project trusted), and nothing an
// earlier run left (hooks, rules, a provider, MCP servers) is read. It runs
// outside agentd's wall, its commands in its own bubblewrap sandbox (codex
// 0.163 sandboxes with nothing else on Linux, and bubblewrap cannot mount
// inside a Landlock domain; the cut's §10, option A).
// Measured 10-18 on macOS (codex 0.162) with these pins: writes to /tmp, the
// home directory and the shared $TMPDIR refused, to the attempt's own
// temporary directory and the workspace taken.
func codexExecutor(env []string, master, bin string) Executor {
	if bin == "" {
		bin = "codex" // none on PATH: it fails to start, and is never confined
	}
	return func(a *Attempt) (Spawn, error) {
		tmp := attemptTmp(a.Out)
		// the path goes into TOML as a Go-quoted string: no control character
		// may be in it (review 283 C)
		if strings.ContainsFunc(tmp, unicode.IsControl) {
			return Spawn{}, fmt.Errorf("agentd: the attempt's directory %q holds a control character", tmp)
		}
		own, err := attemptEnv(env, a.Out)
		if err != nil {
			return Spawn{}, err
		}
		home := codexHomeOf(a.Out)
		if err = ownDir(home); err != nil {
			return Spawn{}, err
		}
		if err = copyCodexLogin(master, home); err != nil {
			return Spawn{}, fmt.Errorf("agentd: codex's login: %w", err)
		}
		argv := []string{bin, "exec", "--json", "--skip-git-repo-check", "-C", a.Dir, "-s", "workspace-write", "--ignore-rules"}
		for _, p := range codexPins {
			argv = append(argv, "-c", p)
		}
		argv = append(argv, "-c", codexRoots(tmp))
		argv = append(argv, "-o", filepath.Join(a.Out, "report.md"))
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "-m", m)
		}
		return Spawn{Argv: append(argv, Prompt(a)), Env: withEnv(own, "CODEX_HOME", home), Dir: a.Dir}, nil
	}
}

// withEnv is env with key set to value, replacing what it had.
func withEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

// cursorRefused are files of a checkout that make cursor run commands of the
// checkout's own: project hooks and MCP servers (TD-371). cursor does not
// start in a checkout that has one; a home of the attempt's own keeps the
// user's (~/.cursor) out.
var cursorRefused = []string{".cursor/hooks.json", ".cursor/mcp.json"}

func cursorExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		for _, rel := range cursorRefused {
			if _, err := os.Lstat(filepath.Join(a.Dir, rel)); !errors.Is(err, fs.ErrNotExist) {
				return Spawn{}, fmt.Errorf("agentd: the checkout has %s (or it cannot be looked at: %v); cursor would run its commands, so it does not start (TD-371)", rel, err)
			}
		}
		own, err := attemptEnv(env, a.Out)
		if err != nil {
			return Spawn{}, err
		}
		// A data directory of the attempt's own, short enough for cursor's
		// sockets: otherwise cursor falls back to /tmp/.cursor (confine.go).
		data := cursorDataDir(a.root, a.Out)
		if a.root != "" {
			if n := len(filepath.Join(data, "projects")); n > cursorPathMax {
				return Spawn{}, fmt.Errorf("agentd: cursor's data directory %s is %d characters with its projects, over %d; cursor would put its sockets in /tmp", data, n, cursorPathMax)
			}
			if err = ownDir(filepath.Dir(data)); err != nil {
				return Spawn{}, err
			}
		}
		if err = ownDir(data); err != nil {
			return Spawn{}, err
		}
		own = withEnv(own, "CURSOR_DATA_DIR", data)
		argv := []string{"cursor-agent", "-p", "--output-format", "stream-json", "--trust", "--workspace", a.Dir}
		if a.Claim.EffectClass != "branch" {
			// An attempt that pushes no branch (a review) changes nothing:
			// cursor's ask mode writes no file and runs only read-only
			// commands. Without it a reviewer on agent-06 whose commands were
			// refused wrote .cursor/rules/allow-shell.mdc and cli.json into the
			// checkout to allow itself the shell (2026-10-17, PROD M-3).
			argv = append(argv, "--mode", "ask")
		}
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "--model", m)
		}
		return Spawn{Argv: append(argv, Prompt(a)), Env: own, Dir: a.Dir,
			After: func() error {
				return streamResultReport("cursor", a.root, a.Out)
			}}, nil
	}
}

// claudePins are the flags claude always runs with: no MCP server; no
// user, project or local settings file read, the host user's nor the
// checkout's, and their hooks and plugins' off: a hook is a command the
// tools agentd grants do not govern, and a branch under review could carry
// one in its .claude/settings.json (measured 10-18, claude 2.1.302: such a
// hook runs without these, and with either alone it does not; TD-371). A
// managed policy (/etc/claude-code, its managed-settings.d too) still
// applies, hooks and all: only root writes it, and check-agent-06.sh requires
// there be none, or one that turns every hook off (reviews 282, 283).
var claudePins = [][]string{{"--strict-mcp-config"}, {"--mcp-config", `{"mcpServers":{}}`},
	{"--setting-sources", ""}, {"--settings", `{"disableAllHooks":true}`}}

// The built-ins claude may use (--tools limits the available ones,
// --allowedTools only grants permission), and every other one refused: a
// reader's are read-only; an implementer's (an attempt that pushes a branch,
// W-31 2.4 「Claude 写核心代码」) also edit, write and run commands, which
// agentd's wall holds to the attempt's own directories (claudeExecutor).
const (
	claudeReadTools    = "Read,Grep,Glob"
	claudeWriteTools   = claudeReadTools + ",Edit,Write,Bash"
	claudeReadRefused  = "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch"
	claudeWriteRefused = "NotebookEdit,WebFetch,WebSearch"
	claudeNoWall       = "agentd: claude edits and runs commands only inside agentd's write wall, and this host has none for it (TD-371): it implements nothing here"
)

// claudeTools are the tool flags of a claude attempt: an implementer's when
// it writes, a reader's otherwise.
func claudeTools(writes bool) []string {
	tools, refused := claudeReadTools, claudeReadRefused
	if writes {
		tools, refused = claudeWriteTools, claudeWriteRefused
	}
	return []string{"--tools", tools, "--allowedTools", tools, "--disallowedTools", refused, "--verbose"}
}

// claudeWriter says whether a claude attempt is an implementer's: it pushes
// a branch, as cursor's agent mode does (an attempt that pushes none may
// change nothing, checkChanges).
func claudeWriter(a *Attempt) bool { return a.Claim.EffectClass == "branch" }

// claudeExecutor runs claude with claudePins and its tools, its
// configuration directory and its temporary root the attempt's own
// (CLAUDE_CONFIG_DIR, CLAUDE_CODE_TMPDIR: claude 2.1.302 keeps its temporary
// files under /tmp otherwise, which the wall refuses and every attempt
// would share) and its updater off: an update writes beside the program,
// outside the wall. An implementer's attempt starts only inside agentd's
// wall (a.Walled): with no wall its edits and commands could write anywhere
// the agent user may. The OAuth token comes from the environment.
func claudeExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		writes := claudeWriter(a)
		if writes && !a.Walled {
			return Spawn{}, errors.New(claudeNoWall)
		}
		own, err := attemptEnv(env, a.Out)
		if err != nil {
			return Spawn{}, err
		}
		dir, tmp := claudeDirOf(a.Out), claudeTmpOf(a.Out)
		for _, d := range []string{dir, tmp} {
			if err = ownDir(d); err != nil {
				return Spawn{}, err
			}
		}
		own = withEnv(withEnv(withEnv(own, "CLAUDE_CONFIG_DIR", dir), "CLAUDE_CODE_TMPDIR", tmp), "DISABLE_AUTOUPDATER", "1")
		argv := []string{"claude", "-p", "--output-format", "stream-json"}
		for _, p := range claudePins {
			argv = append(argv, p...)
		}
		argv = append(argv, claudeTools(writes)...)
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "--model", m)
		}
		// The tool lists take every word that follows, and a prompt that begins
		// with "-" is refused as an unknown option; "--" ends both (measured
		// 10-15, claude 2.1.295; review 215 B1, B2).
		return Spawn{Argv: append(argv, "--", Prompt(a)), Env: own, Dir: a.Dir,
			After: func() error {
				return streamResultReport("claude", a.root, a.Out)
			}}, nil
	}
}

// streamResultReport writes the result of a vendor's stream-json output (its last
// result event) as the report. The output is read a line at a time and a
// line over maxBody is skipped, never held (review 164 B1).
func streamResultReport(vendor, root, out string) error {
	logPath := filepath.Join(out, "log.txt")
	f, err := openRegular(logPath, maxMeasured) // never waits on what the executor left in its place (review 274 A3)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var result string
	var failed bool
	err = eachLine(f, maxBody, func(line []byte) {
		var ev struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Type == "result" && ev.Result != "" {
			result, failed = ev.Result, ev.IsError
		}
	})
	if err != nil {
		return fmt.Errorf("agentd: %s's output: %w", vendor, err)
	}
	if result == "" {
		return fmt.Errorf("agentd: %s left no result", vendor)
	}
	// A run that ends in error puts the error in result (claude 2.1.295 with no
	// login: "Failed to authenticate: …", is_error true, measured 10-15). That
	// is a failed attempt, not a report.
	if failed {
		return fmt.Errorf("agentd: %s ended in error: %s", vendor, oneLine(result, 200))
	}
	r, err := ownDirAt(root, out)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return writeOwn(r, "report.md", []byte(result))
}

// oneLine is s on one line, cut to at most max bytes without splitting a
// character.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		cut := max
		for cut > 0 && cut > max-(utf8.UTFMax-1) && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if !utf8.RuneStart(s[cut]) {
			cut = max // not UTF-8 here: cut by bytes as before
		}
		s = s[:cut]
	}
	return s
}

// eachLine calls fn with each line of r up to max bytes long; longer lines
// are skipped without being kept.
func eachLine(r io.Reader, max int, fn func([]byte)) error {
	return eachLineOr(r, max, fn, nil)
}

// eachLineOr is eachLine that calls over for each line it skips, one longer
// than max (nil: none is told).
func eachLineOr(r io.Reader, max int, fn func([]byte), over func()) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	skip := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !skip {
			if len(line)+len(chunk) > max {
				skip, line = true, line[:0]
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if !skip && len(line) > 0 {
			fn(bytes.TrimRight(line, "\r\n"))
		}
		if skip && over != nil {
			over()
		}
		line, skip = line[:0], false
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// RunVerify is the deterministic executor (agentd verify --spec <file> --out
// <file>): it runs the spec's verify commands in the current directory and
// writes verify.json; it exits 0 when they all pass.
func RunVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the spec whose verify commands to run")
	outPath := fs.String("out", "", "where to write verify.json")
	var writes stringsFlag
	fs.Var(&writes, "write", "a directory the commands may write (repeated); none: no wall of their own")
	if err := fs.Parse(args); err != nil || *specPath == "" || *outPath == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: agentd verify --spec <file> --out <file> [--write <dir>...]")
		return 2
	}
	body, err := os.ReadFile(*specPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	spec, err := ParseSpec(body)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	limit := time.Duration(spec.TimeoutS) * time.Second
	v, err := walledVerify(context.Background(), os.Environ(), spec.Verify, writes, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	out, err := handIn(v, newPathRewrite())
	if err == nil {
		err = os.WriteFile(*outPath, out, 0o600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !v.Pass {
		return 1
	}
	return 0
}

// handIn is verify.json as RunVerify writes it: v without local paths
// (withoutLocalPaths), marshalled, and the JSON itself masked
// (localpath.MaskJSON), since the platform scans the marshalled bytes and the
// encoder's own escapes can complete H8's drive-letter form ("C:<" is written
// "C:\u003c", review 237 A). When a local path is still left, or the output
// names another task's workspace (localpath.WorkspaceRef, T11: the platform
// refuses that too; its own directories are "." and ".." by now, review 342
// A3), the outputs are withheld rather than refused.
func handIn(v Verification, paths *pathRewrite) ([]byte, error) {
	v = withoutLocalPaths(v, paths)
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	if masked, ok := localpath.MaskJSON(out); ok {
		if _, names := localpath.WorkspaceRef(masked); !names {
			return masked, nil
		}
	}
	v.Reason = withheld
	for i := range v.Commands {
		v.Commands[i].Argv = []string{withheld}
		v.Commands[i].Tail = withheld
	}
	return json.MarshalIndent(v, "", "  ")
}

const withheld = "(withheld: it still named a path of this machine, or another task's workspace, after masking)"

// pathRewrite names the directories a check's output names most by what
// they are to the check: the checkout "." (the check runs in it), the task's
// directory with the siblings beside the checkout "..", and the home directory
// "$HOME", each also by its resolved form.
type pathRewrite struct {
	re    *regexp.Regexp
	alias map[string]string
}

func newPathRewrite() *pathRewrite {
	wd, _ := os.Getwd()
	return newPathRewriteAt(wd, map[string]string{"$HOME": os.Getenv("HOME")})
}

// newPathRewriteAt is the rewrite for what ran in the checkout wd, each
// directory in named also by its name ("$HOME"), from anywhere: agentd
// handing in an attempt's report (handText).
func newPathRewriteAt(wd string, named map[string]string) *pathRewrite {
	return newPathRewriteFor(wd, true, named)
}

// newPathRewriteFor is newPathRewriteAt with the checkout's parent ("..")
// named only when parent says so: an action target names the checkout and
// the named roots, never the task's directory, so another attempt or a
// sibling stays masked whole there (review 405 A1).
func newPathRewriteFor(wd string, parent bool, named map[string]string) *pathRewrite {
	p := &pathRewrite{alias: map[string]string{}}
	var dirs []string
	add := func(d, alias string) {
		if d == "" || !filepath.IsAbs(d) {
			return
		}
		for _, d := range []string{filepath.Clean(d), resolved(d)} {
			if _, seen := p.alias[d]; d != "" && d != "/" && !seen {
				p.alias[d] = alias
				dirs = append(dirs, d)
			}
		}
	}
	if wd != "" {
		add(wd, ".")
		if parent {
			add(filepath.Dir(wd), "..")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		add(named[name], name)
	}
	if len(dirs) == 0 {
		return p
	}
	// Longest first, so that at one place the checkout wins over its parent.
	sort.SliceStable(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	quoted := make([]string, len(dirs))
	for i, d := range dirs {
		quoted[i] = regexp.QuoteMeta(d)
	}
	// A directory counts only as a whole path: at the start of the text or
	// after a character that cannot be part of a path (so "/vendor/x/a1" and
	// "https://h/x/a1" are not the checkout "/x/a1"), and followed by a
	// separator, the end, or a character that ends a path in a tool's output
	// (so "/x/a12", "/x/a1+b" and "/x/a1é" are not it either; review 237 B).
	p.re = regexp.MustCompile("(^|[\\s\"'`(\\[{<>=:,;|])(" + strings.Join(quoted, "|") + ")(/|$|[\\s\"'`)\\]}<>:,;|])")
	return p
}

func resolved(d string) string {
	r, err := filepath.EvalSymlinks(d)
	if err != nil {
		return ""
	}
	return filepath.Clean(r)
}

// rewrite replaces each whole directory path in s by its alias. An alias
// starts with "." or "$", never at a place a path can start, so a pass never
// matches what an earlier one wrote; it passes again only for a path whose
// leading character was taken as the previous path's end ("/x/a1:/x/a1/b").
func (p *pathRewrite) rewrite(s string) string {
	if p.re == nil {
		return s
	}
	for range len(s) + 1 {
		idx := p.re.FindAllStringSubmatchIndex(s, -1)
		if idx == nil {
			break
		}
		var b strings.Builder
		last := 0
		for _, m := range idx {
			b.WriteString(s[last:m[4]])
			b.WriteString(p.alias[s[m[4]:m[5]]])
			last = m[5]
		}
		b.WriteString(s[last:])
		s = b.String()
	}
	return s
}

// sgr is an ANSI colour or style sequence. Tools that colour their output
// put one right before and after a path ("\x1b[4m/x/a1/a.ts\x1b[24m"), where
// it would hide the path's boundaries from pathRewrite; nobody reads
// verify.json in a terminal, so they are dropped (review 238 B).
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// withoutLocalPaths rewrites what a check hands in so that it names no path
// of this machine: the directories paths knows by their aliases, then
// whatever the platform's rule H8 still matches masked (localpath.Mask),
// colour sequences dropped first. The
// platform refuses a verify artifact that holds a local path, and tools print
// absolute ones (2026-10-17, STG walk M-9: ESLint's warnings made every
// Console check's verify.json refused).
func withoutLocalPaths(v Verification, paths *pathRewrite) Verification {
	clean := func(s string) string { return localpath.Mask(paths.rewrite(sgr.ReplaceAllString(s, ""))) }
	v.Reason = clean(v.Reason)
	for i := range v.Commands {
		tr := &v.Commands[i]
		tr.Tail = clean(tr.Tail)
		argv := make([]string, len(tr.Argv))
		for j, a := range tr.Argv {
			argv[j] = clean(a)
		}
		tr.Argv = argv
	}
	return v
}
