package agentd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The executors of round 2 (design v5 §5.2 「执行者」): deterministic (the
// spec's verify commands, run by agentd itself), codex and cursor, with the
// flags their --help gives on the host (10-13). Each runs with an
// environment rebuilt from an allow list: no admin kubeconfig, no operator or
// admin token, no business key reaches a task. The agent executors get no
// MCP server from the host's own configuration (codex: mcp_servers emptied;
// cursor: MCP servers are not approved in print mode without --approve-mcps).

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
	return b.String()
}

// Executors are the executors this host runs, by actor id.
func Executors(self string, environ []string) map[string]Executor {
	env := AllowedEnv(environ)
	return map[string]Executor{
		"exec:deterministic": deterministicExecutor(self, env),
		"exec:codex":         codexExecutor(env),
		"exec:cursor":        cursorExecutor(env),
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

func codexExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		argv := []string{"codex", "exec", "--json", "--skip-git-repo-check", "-C", a.Dir, "-s", "workspace-write",
			"-c", "mcp_servers={}", "-o", filepath.Join(a.Out, "report.md")}
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "-m", m)
		}
		return Spawn{Argv: append(argv, Prompt(a)), Env: env, Dir: a.Dir}, nil
	}
}

func cursorExecutor(env []string) Executor {
	return func(a *Attempt) (Spawn, error) {
		argv := []string{"cursor-agent", "-p", "--output-format", "stream-json", "--trust", "--workspace", a.Dir}
		if m := a.Claim.Tool.Model; m != "" {
			argv = append(argv, "--model", m)
		}
		return Spawn{Argv: append(argv, Prompt(a)), Env: env, Dir: a.Dir,
			After: func() error { return cursorReport(filepath.Join(a.Out, "log.txt"), filepath.Join(a.Out, "report.md")) }}, nil
	}
}

// cursorReport writes the result of cursor's stream-json output (its last
// result event) as the report.
func cursorReport(logPath, reportPath string) error {
	b, err := os.ReadFile(logPath)
	if err != nil {
		return err
	}
	var result string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), maxBody)
	for sc.Scan() {
		var ev struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.Type == "result" && ev.Result != "" {
			result = ev.Result
		}
	}
	if result == "" {
		return errors.New("agentd: cursor left no result")
	}
	return os.WriteFile(reportPath, []byte(result), 0o600)
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
	out, err := json.MarshalIndent(v, "", "  ")
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
