package s5check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// behaviorScript is the trusted suite runner inside the protected policy root.
const behaviorScript = "testdata/behavior.cjs"

// The fixed original assertions plus added boundaries, per timezone run
// (testdata/README.md). A run that executes fewer is not a completed suite.
const (
	behaviorTests      = 30
	behaviorAssertions = 122
)

var behaviorZones = []string{"UTC", "America/Chicago"}

// NodeBehaviorRunner executes the protected suite in a separate Node process.
// Candidate source is executed only there (inside the suite's VM, inside the
// CI sandbox that runner.py imposes on this whole process tree); this process
// never loads it. The runner writes the exact enrolled candidate blobs from the
// immutable result tree into a fresh private directory — no checkout, no
// symlinks, no candidate node_modules, scripts or result files — and records
// completion from the trusted harness output and exit status.
type NodeBehaviorRunner struct {
	NodeBinary string
	PolicyRoot string
	ModulePath string
	Manifest   json.RawMessage
	Timeout    time.Duration
}

type behaviorRun struct {
	Timezone    string  `json:"timezone"`
	Tests       int     `json:"tests"`
	Assertions  int     `json:"assertions"`
	ParseMillis float64 `json:"parseMillis"`
}

func (n NodeBehaviorRunner) Run(ctx context.Context, result TreeSnapshot, key CandidateKey) (BehaviorResult, error) {
	out := BehaviorResult{}
	profiles, err := policyFiles(n.Manifest)
	if err != nil {
		return out, err
	}
	_, _, tests, err := contractDigests(n.Manifest)
	if err != nil {
		return out, err
	}
	if !filepath.IsAbs(n.PolicyRoot) || !filepath.IsAbs(n.ModulePath) {
		return out, errors.New("behavior runner requires absolute protected paths")
	}
	script := filepath.Join(n.PolicyRoot, filepath.FromSlash(behaviorScript))
	work, err := os.MkdirTemp("", "s5-behavior-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(work)
	for p := range profiles {
		entry, ok := result.Files[p]
		if !ok || entry.Mode != "100644" || entry.Binary {
			return out, fmt.Errorf("enrolled source %s is not a regular text blob in R", p)
		}
		target := filepath.Join(work, filepath.FromSlash(p))
		if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return out, err
		}
		if err = os.WriteFile(target, []byte(entry.Content), 0o400); err != nil {
			return out, err
		}
	}
	node := n.NodeBinary
	if node == "" {
		node = "node"
	}
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, "--root", work)
	cmd.Dir = work
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null", "LC_ALL=C", "NODE_PATH=" + n.ModulePath}
	stdout := &boundedBuffer{limit: 64 << 10}
	stderr := &boundedBuffer{limit: 8192}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil {
		return out, fmt.Errorf("trusted behavior suite: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	var report struct {
		Schema int           `json:"schema"`
		Runs   []behaviorRun `json:"runs"`
	}
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&report); err != nil {
		return out, fmt.Errorf("invalid trusted behavior report: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return out, errors.New("trusted behavior emitted multiple reports")
	}
	if report.Schema != 1 || len(report.Runs) != len(behaviorZones) {
		return out, errors.New("trusted behavior report is incomplete")
	}
	for i, run := range report.Runs {
		if run.Timezone != behaviorZones[i] || run.Tests != behaviorTests || run.Assertions != behaviorAssertions {
			return out, fmt.Errorf("trusted behavior run %d did not complete the fixed suite", i)
		}
	}
	return BehaviorResult{Passed: true, ResultTree: result.Tree, TestsDigest: tests, Detail: "trusted suite completed in UTC and America/Chicago"}, nil
}
