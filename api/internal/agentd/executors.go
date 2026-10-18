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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// The executors of round 2 (design v5 §5.2 「执行者」): deterministic (the
// spec's verify commands, run by agentd itself), codex, cursor and claude, with the
// flags their --help gives on the host (10-13; claude 10-15). Each runs with an
// environment rebuilt from an allow list: no admin kubeconfig, no operator or
// admin token, no business key reaches a task. Codex and cursor are offered
// only where the host user has no MCP servers (mcpguard.go); claude ignores
// the host's MCP configuration and has only read-only tools.

// allowedEnv are the variables an executor may see.
var allowedEnv = map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "LANG": true,
	"LC_ALL": true, "LC_CTYPE": true, "TMPDIR": true, "TERM": true, "GOPATH": true, "GOCACHE": true, "GOMODCACHE": true,
	"CODEX_HOME": true}

// AllowedEnv keeps only the allowed variables of environ.
func AllowedEnv(environ []string) []string {
	out := []string{}
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if allowedEnv[k] {
			out = append(out, kv)
		}
	}
	return out
}

// Prompt is what an agent executor is asked: the spec's text, and how the
// result is handed over.
func Prompt(a *Attempt) string {
	var b strings.Builder
	b.WriteString(a.Spec.Text)
	if a.Spec.Check != "" && a.Subject != nil {
		fmt.Fprintf(&b, "\n\n---\nYou are checking task %s, attempt %s (round %d), at commit %s of %s, checked out here. Its spec:\n\n%s\n",
			a.Spec.SubjectTask, a.Spec.Attempt, a.Spec.Round, a.Spec.Commit, a.Spec.Repo, a.Subject.Text)
		if len(a.VerifyReport) > 0 {
			fmt.Fprintf(&b, "\nIts deterministic check passed; its verify.json:\n\n%s\n", a.VerifyReport)
		}
	}
	b.WriteString("\n\n---\nWork only in this directory. Your final message is your report, in markdown: what you did, what you ran and how it ended, and what is left. Do not write paths of this machine into the report.\n")
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
	return map[string]Executor{
		"exec:deterministic": deterministicExecutor(self, env),
		"exec:codex":         codexExecutor(env),
		"exec:cursor":        cursorExecutor(cursorEnv),
		"exec:claude":        claudeExecutor(claudeEnv),
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
		return Spawn{Argv: []string{self, "verify", "--spec", path, "--out", filepath.Join(a.Out, "verify.json")}, Env: env, Dir: a.Dir}, nil
	}
}

// codexExecutor runs codex in its workspace-write sandbox with what it may
// write pinned on the command line, whatever the host user's configuration
// says (TD-371): no writable root besides the workspace and a temporary
// directory of the attempt's own, not /tmp nor the shared $TMPDIR, and no
// notify command (codex runs it unsandboxed after each turn). Measured
// 10-18 on macOS (codex 0.162): with these pins a write to /tmp and to the
// home directory is refused, one to the workspace taken; on 06 (Linux) the
// probe in agent-06-owner.sh proves it before codex shares the host.
func codexExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		tmp := filepath.Join(a.Out, "tmp")
		if err := os.MkdirAll(tmp, 0o700); err != nil {
			return Spawn{}, err
		}
		argv := []string{"codex", "exec", "--json", "--skip-git-repo-check", "-C", a.Dir, "-s", "workspace-write",
			"-c", "sandbox_workspace_write.writable_roots=[" + strconv.Quote(tmp) + "]",
			"-c", "sandbox_workspace_write.exclude_slash_tmp=true",
			"-c", "sandbox_workspace_write.exclude_tmpdir_env_var=true",
			"-c", "notify=[]",
			"-o", filepath.Join(a.Out, "report.md")}
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "-m", m)
		}
		return Spawn{Argv: append(argv, Prompt(a)), Env: withEnv(env, "TMPDIR", tmp), Dir: a.Dir}, nil
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

func cursorExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
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
		return Spawn{Argv: append(argv, Prompt(a)), Env: env, Dir: a.Dir,
			After: func() error {
				return streamResultReport("cursor", filepath.Join(a.Out, "log.txt"), filepath.Join(a.Out, "report.md"))
			}}, nil
	}
}

func claudeExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		argv := []string{"claude", "-p", "--output-format", "stream-json",
			"--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			// No settings file is read, the host user's nor the checkout's, and
			// no hook runs: a hook is a command the read-only tools do not
			// govern, and a branch under review could carry one in its
			// .claude/settings.json (measured 10-18, claude 2.1.302: such a
			// hook runs without these, and with either alone it does not;
			// TD-371). The OAuth token comes from the environment.
			"--setting-sources", "", "--settings", `{"disableAllHooks":true}`,
			"--allowedTools", "Read,Grep,Glob",
			"--disallowedTools", "Bash,Edit,Write,NotebookEdit,WebFetch,WebSearch",
			// allowedTools grants permission; tools limits the available built-ins.
			"--tools", "Read,Grep,Glob", "--verbose"}
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "--model", m)
		}
		// The tool lists take every word that follows, and a prompt that begins
		// with "-" is refused as an unknown option; "--" ends both (measured
		// 10-15, claude 2.1.295; review 215 B1, B2).
		return Spawn{Argv: append(argv, "--", Prompt(a)), Env: env, Dir: a.Dir,
			After: func() error {
				return streamResultReport("claude", filepath.Join(a.Out, "log.txt"), filepath.Join(a.Out, "report.md"))
			}}, nil
	}
}

// streamResultReport writes the result of a vendor's stream-json output (its last
// result event) as the report. The output is read a line at a time and a
// line over maxBody is skipped, never held (review 164 B1).
func streamResultReport(vendor, logPath, reportPath string) error {
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
	return os.WriteFile(reportPath, []byte(result), 0o600)
}

// oneLine is s on one line, cut to at most max bytes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

// eachLine calls fn with each line of r up to max bytes long; longer lines
// are skipped without being kept.
func eachLine(r io.Reader, max int, fn func([]byte)) error {
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
	if err := fs.Parse(args); err != nil || *specPath == "" || *outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: agentd verify --spec <file> --out <file>")
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
	v := Verify(context.Background(), ".", os.Environ(), spec.Verify, limit)
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
// "C:\u003c", review 237 A). When a local path is still left, the outputs are
// withheld rather than refused for it.
func handIn(v Verification, paths *pathRewrite) ([]byte, error) {
	v = withoutLocalPaths(v, paths)
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	if masked, ok := localpath.MaskJSON(out); ok {
		return masked, nil
	}
	v.Reason = withheld
	for i := range v.Commands {
		v.Commands[i].Argv = []string{withheld}
		v.Commands[i].Tail = withheld
	}
	return json.MarshalIndent(v, "", "  ")
}

const withheld = "(withheld: it still named a path of this machine after masking)"

// pathRewrite names the directories a check's output names most by what
// they are to the check: the checkout "." (the check runs in it), the task's
// directory with the siblings beside the checkout "..", and the home directory
// "$HOME", each also by its resolved form.
type pathRewrite struct {
	re    *regexp.Regexp
	alias map[string]string
}

func newPathRewrite() *pathRewrite {
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
	if wd, err := os.Getwd(); err == nil {
		add(wd, ".")
		add(filepath.Dir(wd), "..")
	}
	add(os.Getenv("HOME"), "$HOME")
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
