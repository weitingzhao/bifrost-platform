// Package unitwatch says on the ledger when main's unit declaration stops
// reading, and when it reads again (2026-11-18, the coordinator: information,
// no gate). a25c27aa declared inputs for a package with no tests; unitplan
// refused the whole file, 34 commits of main ran everything, CI stayed green
// and nobody knew until fde7d1df.
//
// A pass reads main's head. A declaration that does not read is a known
// regression on main, introduced by the first commit of the unbroken run of
// such commits, so the wake reaches whoever made it. One that reads closes
// what this package recorded. A pass decides from main and the ledger alone,
// so running it again changes nothing. What could not be read (Gitea away, a
// tree or a file not served) says nothing about the declaration and records
// nothing.
package unitwatch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

const (
	// watchedRepo is the repository watched. Other repositories' pushes arrive on
	// the same topic and a pass cannot see which one woke it, so it always
	// reads this one's main.
	watchedRepo = "bifrost-platform"
	// walkLimit is how many commits of main a pass reads, the head included,
	// looking for where the declaration stopped reading.
	walkLimit = 100

	// impactPrefix marks this package's regressions among all of the repo's:
	// a pass closes those and no other.
	impactPrefix = unitplan.ManifestPath + " does not read: "

	passTimeout = 10 * time.Minute
)

// The system writes these, not a person or a seat.
var writer = actor.Writer{Actor: "controller"}

// Watch is one repository's main and the ledger its regressions go in.
type Watch struct {
	DB eventlog.DB
	// Head is the commit main is at.
	Head func(ctx context.Context, repo string) (string, error)
	// Tree is a commit's tree, of this repository or of a pinned one.
	Tree func(ctx context.Context, repo, commit string) (*unitplan.Tree, error)
	// FirstParent is a commit's first parent, "" for a commit with none.
	FirstParent func(ctx context.Context, repo, commit string) (string, error)
}

// problem is why commit's declaration does not read, "" when it reads. An
// error means it could not be told. The tree is asked as well as the build's
// answer: go/build turns a file it could not open into an error about the
// package, or drops the cause, so the words of the answer cannot separate a
// bad declaration from a Gitea that did not serve a file.
func (w Watch) problem(ctx context.Context, commit string) (string, error) {
	t, err := w.Tree(ctx, watchedRepo, commit)
	if err != nil {
		return "", fmt.Errorf("the tree of %s: %w", commit, err)
	}
	var pinErr error
	_, err = unitplan.Build(watchedRepo, commit, t, unitplan.Source{Pinned: func(repo, at string) (*unitplan.Tree, error) {
		pin, err := w.Tree(ctx, repo, at)
		if err != nil && pinErr == nil {
			pinErr = fmt.Errorf("the pin %s@%s: %w", repo, at, err)
		}
		return pin, err
	}})
	if pinErr != nil {
		return "", pinErr
	}
	if failed := t.ReadFailure(); failed != nil {
		return "", fmt.Errorf("a file of %s: %w", commit, failed)
	}
	if err != nil {
		return strings.Join(strings.Fields(err.Error()), " "), nil
	}
	return "", nil
}

// Pass brings the ledger in line with main's head. An error means nothing
// could be concluded; the next pass tries again.
func (w Watch) Pass(ctx context.Context) error {
	head, err := w.Head(ctx, watchedRepo)
	if err != nil {
		return fmt.Errorf("main of %s: %w", watchedRepo, err)
	}
	why, err := w.problem(ctx, head)
	if err != nil {
		return err
	}
	open, err := regressions.List(ctx, w.DB, watchedRepo, true)
	if err != nil {
		return err
	}
	var ours []regressions.Regression
	for _, g := range open {
		if strings.HasPrefix(g.Impact, impactPrefix) {
			ours = append(ours, g)
		}
	}
	if why == "" {
		for _, g := range ours {
			if _, err := regressions.Fixed(ctx, w.DB, writer, watchedRepo, g.Introduced, head); err != nil {
				return err
			}
		}
		return nil
	}
	if len(ours) > 0 {
		return nil // already known, and its introducer already told
	}
	introduced, why, whole, err := w.firstUnread(ctx, head, why)
	if err != nil {
		return err
	}
	_, err = regressions.Known(ctx, w.DB, writer, watchedRepo, introduced, impact(why, whole))
	return err
}

// firstUnread walks main back along first parents from head, which does not
// read, to the oldest commit of the unbroken run that does not. whole is false
// when the walk ran out at walkLimit: the run may begin earlier.
func (w Watch) firstUnread(ctx context.Context, head, headWhy string) (commit, why string, whole bool, err error) {
	commit, why = head, headWhy
	for read := 1; read < walkLimit; read++ {
		parent, err := w.FirstParent(ctx, watchedRepo, commit)
		if err != nil {
			return "", "", false, fmt.Errorf("the parent of %s: %w", commit, err)
		}
		if parent == "" {
			return commit, why, true, nil
		}
		parentWhy, err := w.problem(ctx, parent)
		if err != nil {
			return "", "", false, err
		}
		if parentWhy == "" {
			return commit, why, true, nil
		}
		commit, why = parent, parentWhy
	}
	return commit, why, false, nil
}

// impact is the regression's line, cut to what the ledger takes.
func impact(why string, whole bool) string {
	suffix := ""
	if !whole {
		suffix = fmt.Sprintf(" (all %d commits read fail; it may begin earlier)", walkLimit)
	}
	room := regressions.MaxImpact - len([]rune(impactPrefix)) - len([]rune(suffix))
	if r := []rune(why); len(r) > room {
		why = string(r[:room])
	}
	return impactPrefix + why + suffix
}

// Run runs pass once per kick, kicks that arrive meanwhile folding into one:
// a pass can read many trees, far longer than an event may be held. A pass
// that could not conclude runs again after retry without waiting for another
// push, since main can stay quiet for hours.
func Run(ctx context.Context, kicks <-chan struct{}, retry time.Duration, pass func(context.Context) error) {
	var again <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-kicks:
		case <-again:
		}
		again = nil
		pctx, cancel := context.WithTimeout(ctx, passTimeout)
		err := pass(pctx)
		cancel()
		if err != nil && ctx.Err() == nil {
			slog.Warn("unit declaration watch: no conclusion; trying again", "after", retry, "error", err)
			again = time.After(retry)
		}
	}
}
