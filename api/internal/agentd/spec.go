package agentd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// The task spec (design v5 §5.2 「执行者」 says only that the deterministic
// executor runs the command array in the spec; the format is set here). A
// spec is either canonical JSON:
//
//	{"text": "<instructions, markdown>", "verify": [["go","test","./..."], ...], "timeout_s": 900}
//
// or plain markdown, which is all text and has no commands. A check's spec,
// which the controller writes, names what it checks: the subject task, the
// attempt, the round, the subject's own spec, and the commit and repository
// the subject pushed; the deterministic checker runs the subject spec's
// verify commands on that commit.
//
// A spec may also name siblings, repositories cloned next to the checkout
// (at <root>/tasks/<task>/<repo>) at the head of a branch, and setup, commands
// run in the checkout before the executor, as its first act in the attempt's
// process group, with its environment and the network a sandboxed executor
// lacks (the Console's dependencies, CUT-agentd-console-checks-2026-10-16). A
// check's attempt prepares both from the subject's spec.
type TaskSpec struct {
	Text     string     `json:"text"`
	Verify   [][]string `json:"verify,omitempty"`
	TimeoutS int        `json:"timeout_s,omitempty"`
	Setup    [][]string `json:"setup,omitempty"`
	Siblings []Sibling  `json:"siblings,omitempty"`

	Check       string `json:"check,omitempty"` // verify or review
	SubjectTask string `json:"subject_task,omitempty"`
	Attempt     string `json:"attempt,omitempty"`
	Round       int    `json:"round,omitempty"`
	SubjectSpec string `json:"task_spec,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Repo        string `json:"repo,omitempty"`
	// A review's spec also names the deterministic check that passed before
	// it and that check's verify.json (store.checkSpec, review 163 A1).
	Verification string `json:"verification,omitempty"`
	VerifyReport string `json:"verify_report,omitempty"`
	// Salvage: the check is of a lost attempt's salvaged result (platform
	// store/adopt.go, T34 case 8); it runs as any other check.
	Salvage bool `json:"salvage,omitempty"`
}

// ParseSpec reads a spec: JSON when it is a JSON object, else markdown text.
// Every verify command is a non-empty argument array.
func ParseSpec(body []byte) (TaskSpec, error) {
	if !utf8.Valid(body) {
		return TaskSpec{}, errors.New("agentd: a task spec is UTF-8 text")
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return TaskSpec{Text: string(body)}, nil
	}
	var s TaskSpec
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return TaskSpec{}, fmt.Errorf("agentd: task spec: %w", err)
	}
	for i, c := range s.Verify {
		if len(c) == 0 || c[0] == "" {
			return TaskSpec{}, fmt.Errorf("agentd: task spec: verify command %d is empty", i)
		}
	}
	for i, c := range s.Setup {
		if len(c) == 0 || c[0] == "" {
			return TaskSpec{}, fmt.Errorf("agentd: task spec: setup command %d is empty", i)
		}
	}
	if s.TimeoutS < 0 {
		return TaskSpec{}, errors.New("agentd: task spec: timeout_s is negative")
	}
	if len(s.Siblings) > maxSiblings {
		return TaskSpec{}, fmt.Errorf("agentd: task spec: at most %d siblings", maxSiblings)
	}
	seen := map[string]bool{}
	for i, sib := range s.Siblings {
		if err := sib.check(); err != nil {
			return TaskSpec{}, fmt.Errorf("agentd: task spec: sibling %d: %w", i, err)
		}
		if seen[sib.Repo] {
			return TaskSpec{}, fmt.Errorf("agentd: task spec: sibling %d: %s twice", i, sib.Repo)
		}
		seen[sib.Repo] = true
	}
	return s, nil
}

// Sibling is a repository a task's checkout needs beside it, at the head of a
// branch: the Console's file:../../bifrost-ui dependency resolves to it.
type Sibling struct {
	Repo string `json:"repo"`
	Ref  string `json:"ref"`
}

const maxSiblings = 4

var (
	// siblingRepo is a directory name next to the attempts, never an
	// attempt's own (a<n>, a<n>.out) and never a path.
	siblingRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	attemptDir  = regexp.MustCompile(`^a[0-9]+$`)
	// siblingRef is a branch name git takes in a refspec, never an option.
	siblingRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

func (s Sibling) check() error {
	switch {
	case !siblingRepo.MatchString(s.Repo) || attemptDir.MatchString(s.Repo):
		return fmt.Errorf("repo %q is not a repository name", s.Repo)
	case !siblingRef.MatchString(s.Ref) || strings.Contains(s.Ref, "..") || strings.Contains(s.Ref, "//") ||
		strings.HasSuffix(s.Ref, "/") || strings.HasSuffix(s.Ref, ".lock"):
		return fmt.Errorf("ref %q is not a branch name", s.Ref)
	}
	return nil
}

// Verification is verify.json: what the deterministic checker ran and how
// each command ended. Pass is true only when every command ran and exited 0;
// no commands is not a pass (rule C5: no conclusion is not a pass).
type Verification struct {
	Pass     bool           `json:"pass"`
	Commands []CommandTrace `json:"commands"`
	Reason   string         `json:"reason,omitempty"`
}

// CommandTrace is one command's run.
type CommandTrace struct {
	Argv     []string `json:"argv"`
	ExitCode int      `json:"exit_code"`
	Seconds  float64  `json:"seconds"`
	TimedOut bool     `json:"timed_out,omitempty"`
	Tail     string   `json:"tail"`
}

const tailBytes = 4096

// Verify runs cmds in dir one after another, each with an argument array and
// the given environment, stopping at the first failure, within limit in all.
func Verify(ctx context.Context, dir string, env []string, cmds [][]string, limit time.Duration) Verification {
	v := Verification{Commands: []CommandTrace{}}
	if len(cmds) == 0 {
		v.Reason = "the spec names no verify command"
		return v
	}
	if limit <= 0 {
		limit = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	for _, argv := range cmds {
		out := &ring{max: tailBytes}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, out, out
		// The command stays in the attempt's process group, so whatever ends
		// the attempt (stop, hard limit, a lost lease, a restart) ends it and
		// its descendants too (review 164 A1). When its own time is up only
		// the command is killed; what it started is cleared with the group
		// once the check exits, and the wait for pipes a descendant still
		// holds is bounded (review 163 B1).
		cmd.WaitDelay = 2 * time.Second
		start := time.Now()
		err := cmd.Run()
		tr := CommandTrace{Argv: argv, ExitCode: -1, Seconds: time.Since(start).Seconds(), Tail: tail(out.bytes())}
		if cmd.ProcessState != nil {
			tr.ExitCode = cmd.ProcessState.ExitCode()
		}
		if ctx.Err() != nil {
			tr.TimedOut = true
		}
		v.Commands = append(v.Commands, tr)
		if err != nil || tr.ExitCode != 0 {
			v.Reason = fmt.Sprintf("%s ended %d", argv[0], tr.ExitCode)
			if tr.TimedOut {
				v.Reason = fmt.Sprintf("%s did not finish within %s", argv[0], limit)
			}
			return v
		}
	}
	v.Pass = true
	return v
}

func tail(b []byte) string {
	if len(b) > tailBytes {
		b = b[len(b)-tailBytes:]
		for len(b) > 0 && !utf8.RuneStart(b[0]) {
			b = b[1:]
		}
	}
	return string(b)
}

// ring keeps the last max bytes written to it.
type ring struct {
	max int
	buf []byte
}

func (r *ring) Write(p []byte) (int, error) {
	n := len(p)
	if n >= r.max {
		r.buf = append(r.buf[:0], p[n-r.max:]...)
		return n, nil
	}
	if keep := r.max - n; len(r.buf) > keep {
		r.buf = append(r.buf[:0], r.buf[len(r.buf)-keep:]...)
	}
	r.buf = append(r.buf, p...)
	return n, nil
}

func (r *ring) bytes() []byte { return r.buf }
