package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type gitHistory struct {
	directory string
	firstOnce sync.Once
	first     map[string]time.Time
	firstErr  error
}

func newGitHistory(directory string) (*gitHistory, error) {
	h := &gitHistory{directory: directory}
	out, err := h.git("rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, fmt.Errorf("infra history unavailable: %w", err)
	}
	if strings.TrimSpace(out) != "false" {
		return nil, fmt.Errorf("infra history is shallow; first-seen dates cannot be established")
	}
	return h, nil
}
func (h *gitHistory) git(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", h.directory}, args...)...)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(out), err
}

var historyID = regexp.MustCompile(`^TD-[1-9][0-9]*$`)

func (h *gitHistory) FirstSeen(td string) (time.Time, error) {
	if !historyID.MatchString(td) {
		return time.Time{}, fmt.Errorf("invalid debt id")
	}
	h.firstOnce.Do(h.loadFirstSeen)
	if h.firstErr != nil {
		return time.Time{}, h.firstErr
	}
	if t, ok := h.first[td]; ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("heading not found in infra history")
}

func (h *gitHistory) loadFirstSeen() {
	h.first = map[string]time.Time{}
	// One traversal for the entire import, across every branch. Added exact
	// headings (not substring pickaxe matches) distinguish TD-9 from TD-91.
	out, err := h.git("log", "--all", "--format=TIME %cI", "--no-ext-diff", "--no-renames", "--unified=0", "-m", "-p", "-G^### TD-[1-9][0-9]*$", "--", "agent-config/TECH_DEBT.md")
	if err != nil {
		h.firstErr = err
		return
	}
	var at time.Time
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "TIME ") {
			at, err = time.Parse(time.RFC3339, strings.TrimPrefix(line, "TIME "))
			if err != nil {
				h.firstErr = err
				return
			}
			continue
		}
		if !strings.HasPrefix(line, "+### ") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(line, "+### "), "\r")
		if !historyID.MatchString(id) {
			continue
		}
		old := h.first[id]
		if old.IsZero() || at.Before(old) {
			h.first[id] = at
		}
	}
}
