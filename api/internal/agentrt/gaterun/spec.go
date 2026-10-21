package gaterun

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// A gate's spec is an agentd task spec (agentd/spec.go) with text, verify
// and timeout_s only: no setup, no siblings, no check fields. Each verify
// command runs this package's runner on one package and names its tests:
//
//	["go","-C","api","run","./cmd/agentrt-gate","-pkg","./internal/agentrt/plan/","-test","TestGrammarIsClosed", ...]
//
// No shell, no env, no other go flag: what runs is the runner, and the
// runner runs the tests (the review td397-1 A8).

// RunnerArgv is what every gate command starts with.
var RunnerArgv = []string{"go", "-C", "api", "run", "./cmd/agentrt-gate"}

var (
	pkgRe  = regexp.MustCompile(`^\./internal/[a-z0-9_]+(/[a-z0-9_]+)*/$`)
	testRe = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
	idRe   = regexp.MustCompile(`^T[0-9]{2}[a-z]?$`) // plan.testIDRe
)

// ErrSpec is a gate spec that is not one.
var ErrSpec = errors.New("gaterun: not a gate spec")

// Command is one verify command of a gate: a package and its tests.
type Command struct {
	Pkg   string
	Tests []string
}

// Argv is the command as the spec holds it.
func (c Command) Argv() []string {
	argv := append(slices.Clone(RunnerArgv), "-pkg", c.Pkg)
	for _, t := range c.Tests {
		argv = append(argv, "-test", t)
	}
	return argv
}

// Gate is one gate: its test id (a plan rule's gates_passed test), what it
// shows, and its commands.
type Gate struct {
	ID       string
	Text     string
	Commands []Command
	TimeoutS int
}

// DefaultTimeoutS bounds a gate's commands in all.
const DefaultTimeoutS = 1200

// Spec is the gate's spec as canonical JSON, and its sha256 (what a rule
// pins).
func (g Gate) Spec() ([]byte, string, error) {
	if !idRe.MatchString(g.ID) || g.Text == "" || len(g.Commands) == 0 {
		return nil, "", fmt.Errorf("%w: gate %q needs a test id, text and commands", ErrSpec, g.ID)
	}
	verify := []any{}
	for _, c := range g.Commands {
		if err := c.check(); err != nil {
			return nil, "", err
		}
		args := []any{}
		for _, a := range c.Argv() {
			args = append(args, a)
		}
		verify = append(verify, args)
	}
	timeout := g.TimeoutS
	if timeout == 0 {
		timeout = DefaultTimeoutS
	}
	body, err := canon.Encode(map[string]any{"text": g.Text, "verify": verify, "timeout_s": timeout})
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:]), nil
}

func (c Command) check() error {
	if !pkgRe.MatchString(c.Pkg) || len(c.Tests) == 0 {
		return fmt.Errorf("%w: a command names one package under ./internal/ and at least one test", ErrSpec)
	}
	seen := map[string]bool{}
	for _, t := range c.Tests {
		if !testRe.MatchString(t) || seen[t] {
			return fmt.Errorf("%w: test %q is not a test name, or is named twice", ErrSpec, t)
		}
		seen[t] = true
	}
	return nil
}

// ParseSpec reads a stored gate spec and returns its commands, refusing
// anything but the form above: unknown fields, setup, siblings, check
// fields, a command that is not the runner on one package with named tests,
// or a timeout out of bounds.
func ParseSpec(body []byte) ([]Command, error) {
	var s struct {
		Text     *string    `json:"text"`
		Verify   [][]string `json:"verify"`
		TimeoutS *int       `json:"timeout_s"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpec, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: more than one JSON value", ErrSpec)
	}
	if s.Text == nil || *s.Text == "" || len(s.Verify) == 0 || s.TimeoutS == nil || *s.TimeoutS < 60 || *s.TimeoutS > 3600 {
		return nil, fmt.Errorf("%w: it needs text, verify commands and a timeout_s of 60 to 3600", ErrSpec)
	}
	var out []Command
	for i, argv := range s.Verify {
		c, err := parseArgv(argv)
		if err != nil {
			return nil, fmt.Errorf("command %d: %w", i, err)
		}
		out = append(out, c)
	}
	return out, nil
}

func parseArgv(argv []string) (Command, error) {
	if len(argv) < len(RunnerArgv)+4 || !slices.Equal(argv[:len(RunnerArgv)], RunnerArgv) {
		return Command{}, fmt.Errorf("%w: a command runs %v", ErrSpec, RunnerArgv)
	}
	rest := argv[len(RunnerArgv):]
	if rest[0] != "-pkg" {
		return Command{}, fmt.Errorf("%w: the runner's first flag is -pkg", ErrSpec)
	}
	c := Command{Pkg: rest[1]}
	for i := 2; i < len(rest); i += 2 {
		if rest[i] != "-test" || i+1 >= len(rest) {
			return Command{}, fmt.Errorf("%w: after -pkg come -test <name> pairs only", ErrSpec)
		}
		c.Tests = append(c.Tests, rest[i+1])
	}
	return c, c.check()
}
