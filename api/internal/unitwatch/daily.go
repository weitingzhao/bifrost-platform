package unitwatch

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
)

// The daily unit run (2026-11-18, the coordinator, after the one-off run that
// caught an undeclared input): once a day, in the quiet hour, the units of
// main's head that have no verdict run. It is information and blocks nothing.
// A task that fails on main is a known regression, introduced by the commit
// that gave the task its present inputs, so the existing wake reaches whoever
// made it. The ledger's regressions with dailyPrefix are what a week's
// review counts.

const (
	dailyHour     = 5 // Chicago: no run started in hours 5 and 6 on 2026-11-18
	dailyDeadline = 3 * time.Hour
	dailyPrefix   = "daily unit run: "
)

// DailyTask is one unit task of a commit's plan and its input key.
type DailyTask struct{ Unit, Task, Key string }

func (t DailyTask) name() string { return t.Unit + " " + t.Task }

// Daily is the pass's state; one goroutine runs it. What it started is kept in
// memory: a restart before the report loses that day's report, not the runs.
type Daily struct {
	DB          eventlog.DB
	Head        func(ctx context.Context, repo string) (string, error)
	FirstParent func(ctx context.Context, repo, commit string) (string, error)
	// Start has the commit's units without a verdict run.
	Start func(ctx context.Context, repo, commit string) error
	// Tasks is the commit's plan.
	Tasks func(ctx context.Context, repo, commit string) ([]DailyTask, error)
	// Verdict is the ledger's word on a task's inputs: evidence.EvidenceGreen,
	// EvidenceRed or EvidenceNone.
	Verdict func(ctx context.Context, repo string, t DailyTask) (string, error)
	Now     func() time.Time

	day     string
	commit  string
	started time.Time
}

// Pass starts the day's run in its hour and reports it once it has settled.
// An error leaves the state as it was: the next pass tries again.
func (d *Daily) Pass(ctx context.Context) error {
	now := d.Now()
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return err
	}
	local := now.In(chicago)
	if day := local.Format("2006-01-02"); day != d.day && local.Hour() == dailyHour {
		head, err := d.Head(ctx, watchedRepo)
		if err != nil {
			return fmt.Errorf("main of %s: %w", watchedRepo, err)
		}
		if err := d.Start(ctx, watchedRepo, head); err != nil {
			return fmt.Errorf("starting the units of %s: %w", head, err)
		}
		d.day, d.commit, d.started = day, head, now
		slog.Info("units: daily run started", "commit", head)
	}
	if d.commit == "" {
		return nil
	}
	tasks, err := d.Tasks(ctx, watchedRepo, d.commit)
	if err != nil {
		return err
	}
	var failed []DailyTask
	pending := 0
	for _, t := range tasks {
		verdict, err := d.Verdict(ctx, watchedRepo, t)
		if err != nil {
			return err
		}
		switch verdict {
		case evidence.EvidenceRed:
			failed = append(failed, t)
		case evidence.EvidenceNone:
			pending++
		}
	}
	if pending > 0 && now.Sub(d.started) < dailyDeadline {
		return nil
	}
	if err := d.report(ctx, failed); err != nil {
		return err
	}
	slog.Info("units: daily run settled", "commit", d.commit, "tasks", len(tasks), "failed", len(failed), "without_verdict", pending)
	d.commit = ""
	return nil
}

// report records one regression per introducing commit and closes the daily
// ones whose commit no longer has a failing task.
func (d *Daily) report(ctx context.Context, failed []DailyTask) error {
	byCommit := map[string][]string{}
	plans := map[string]map[string]string{}
	for _, t := range failed {
		introduced, err := d.introducedBy(ctx, t, plans)
		if err != nil {
			return err
		}
		byCommit[introduced] = append(byCommit[introduced], t.name())
	}
	open, err := regressions.List(ctx, d.DB, watchedRepo, true)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, g := range open {
		if !strings.HasPrefix(g.Impact, dailyPrefix) {
			continue
		}
		known[g.Introduced] = true
		if _, still := byCommit[g.Introduced]; !still {
			if _, err := regressions.Fixed(ctx, d.DB, writer, watchedRepo, g.Introduced, d.commit); err != nil {
				return err
			}
		}
	}
	for introduced, names := range byCommit {
		if known[introduced] {
			continue
		}
		if _, err := regressions.Known(ctx, d.DB, writer, watchedRepo, introduced, dailyImpact(names)); err != nil {
			return err
		}
	}
	return nil
}

// introducedBy walks main back from the run's commit to the oldest commit of
// the unbroken run in which the task has these same inputs.
func (d *Daily) introducedBy(ctx context.Context, t DailyTask, plans map[string]map[string]string) (string, error) {
	commit := d.commit
	for read := 1; read < walkLimit; read++ {
		parent, err := d.FirstParent(ctx, watchedRepo, commit)
		if err != nil {
			return "", fmt.Errorf("the parent of %s: %w", commit, err)
		}
		if parent == "" {
			return commit, nil
		}
		keys, ok := plans[parent]
		if !ok {
			tasks, err := d.Tasks(ctx, watchedRepo, parent)
			if err != nil {
				// A commit whose plan cannot be built has no inputs to compare.
				slog.Warn("units: daily run: a plan could not be read", "commit", parent, "error", err)
				return commit, nil
			}
			keys = map[string]string{}
			for _, p := range tasks {
				keys[p.name()] = p.Key
			}
			plans[parent] = keys
		}
		if keys[t.name()] != t.Key {
			return commit, nil
		}
		commit = parent
	}
	return commit, nil
}

func dailyImpact(names []string) string {
	sort.Strings(names)
	line := fmt.Sprintf("%s%d failing on main: %s", dailyPrefix, len(names), strings.Join(names, ", "))
	if r := []rune(line); len(r) > regressions.MaxImpact {
		line = string(r[:regressions.MaxImpact])
	}
	return line
}
