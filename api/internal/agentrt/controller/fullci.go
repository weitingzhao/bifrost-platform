package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The daily full CI (R5:5.7-9): with incremental Go tests per landing
// (R5:5.7-1), the whole suite runs on a schedule and a red run is fixed
// forward. Once a day at At, in the Owner's zone (store.BriefZone, the zone
// of the briefs), a full run of main's head starts, and one of the commit
// STG runs now when that differs, so a PROD promotion of STG's commit
// usually has a full green by morning. A commit runs at most once a day (the
// run's name is its commit and the day). A red run that no open goal of an
// earlier red with the same failed tests covers proposes a goal to fix it.

// FullCIEvery is how often the loop looks at the clock and the runs.
const FullCIEvery = time.Minute

// FullCIAt is the default time of the day's runs; fullCIWindow how far back
// the runs are read (reds to handle, the last green); FullCILandingsMax how
// many landings a goal lists.
const (
	FullCIAt          = "03:00"
	fullCIWindow      = 14 * 24 * time.Hour
	FullCILandingsMax = 30
	fullCITestsMax    = 50
)

// FullCILanding is a first-parent commit of main and its subject.
type FullCILanding struct {
	Commit, Subject string
}

// FullCI is what the loop reads and starts; nil on the Controller runs no
// loop (AGENTRT_FULL_CI=off, or not wired). Runs lists the full runs created
// since, oldest first; Ensure starts the run of a commit on a day (the day
// as the zone has it) and returns its name; Main reads main's head. TestLog
// reads the end of a run's test-api log, Landings main's first-parent
// commits after from up to to, newest first, at most max; either nil, the
// goal says it could not read them. The commit STG runs now is
// Sources.Deployed's.
type FullCI struct {
	At       string // HH:MM; "" is FullCIAt
	Runs     func(ctx context.Context, since time.Time) ([]landingci.FullRun, error)
	Ensure   func(ctx context.Context, revision string, day time.Time) (string, error)
	Main     func(ctx context.Context) (string, error)
	TestLog  func(ctx context.Context, run string) (string, error)
	Landings func(ctx context.Context, from, to string, max int) ([]FullCILanding, error)

	mu      sync.Mutex
	day     string   // the day whose runs are started, or being started
	pending []string // the commits of day still to start
}

// FullCISettings reads the loop's switches: AGENTRT_FULL_CI=off stops it
// (default on), AGENTRT_FULL_CI_AT is its time (HH:MM, default 03:00) and
// AGENTRT_FULL_CI_PARAM=on has a run ask for api-scope-request=full, which
// stays off until the pipeline declares that param. A time that does not
// parse is an error, and the loop does not run.
func FullCISettings(getenv func(string) string) (on bool, at string, param bool, err error) {
	on = strings.TrimSpace(getenv("AGENTRT_FULL_CI")) != "off"
	param = strings.TrimSpace(getenv("AGENTRT_FULL_CI_PARAM")) == "on"
	at = strings.TrimSpace(getenv("AGENTRT_FULL_CI_AT"))
	if at == "" {
		at = FullCIAt
	}
	if _, _, err = fullCIClock(at); err != nil {
		on = false
	}
	return on, at, param, err
}

var fullCIAtShape = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

func fullCIClock(at string) (int, int, error) {
	if at == "" {
		at = FullCIAt
	}
	m := fullCIAtShape.FindStringSubmatch(at)
	if m == nil {
		return 0, 0, fmt.Errorf("AGENTRT_FULL_CI_AT %q is not HH:MM", at)
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	return h, min, nil
}

// startFullCI runs the loop when there is one, timed as the others are.
func (c *Controller) startFullCI(ctx context.Context, wg *sync.WaitGroup) {
	if c.FullCI == nil {
		return
	}
	c.every(ctx, wg, "agentrt-full-ci", FullCIEvery, func() error {
		passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		_, err := c.FullCIPass(passCtx)
		return err
	})
}

// FullCIPass is one pass of the full CI loop: the day's runs once their
// time has come, then the red runs not handled yet. It returns how many
// runs it started and goals it proposed.
func (c *Controller) FullCIPass(ctx context.Context) (int, error) {
	f := c.FullCI
	if f == nil || f.Runs == nil || f.Ensure == nil || f.Main == nil {
		return 0, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := c.clock()
	runs, err := f.Runs(ctx, now.Add(-fullCIWindow))
	if err != nil {
		return 0, fmt.Errorf("full CI runs: %w", err)
	}
	started, serr := c.startFullRuns(ctx, f, now, runs)
	proposed, rerr := c.fullCIReds(ctx, f, runs)
	return started + proposed, errors.Join(serr, rerr)
}

// startFullRuns starts the day's runs once its time has come: main's head
// and STG's commit, read once a day. A run that cannot be started is tried
// again on the next pass; a run of the day already there (a restart) means
// the day's runs were started.
func (c *Controller) startFullRuns(ctx context.Context, f *FullCI, now time.Time, runs []landingci.FullRun) (int, error) {
	loc, err := time.LoadLocation(store.BriefZone)
	if err != nil {
		return 0, err
	}
	h, m, err := fullCIClock(f.At)
	if err != nil {
		return 0, err
	}
	local := now.In(loc)
	if local.Before(time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)) {
		return 0, nil
	}
	day := local.Format("20060102")
	if f.day != day {
		for _, r := range runs {
			if strings.HasSuffix(r.Name, "-"+day) {
				f.day, f.pending = day, nil
				return 0, nil
			}
		}
		main, err := f.Main(ctx)
		if err != nil || main == "" {
			return 0, fmt.Errorf("full CI: main's head: %v", err)
		}
		f.day, f.pending = day, []string{main}
		if c.Sources.Deployed != nil {
			if stg, _, ok := c.boundedDeployed(ctx, "stg"); ok && stg != "" && stg != main {
				f.pending = append(f.pending, stg)
			} else if !ok {
				c.logf("agentrt-full-ci: the commit STG runs cannot be read; only main's head runs today")
			}
		}
	}
	var left, names []string
	var errs []error
	for _, commit := range f.pending {
		start := time.Now()
		name, err := f.Ensure(ctx, commit, local)
		c.slow(slowStepLog, start, "agentrt-full-ci: start %s", commit)
		if err != nil {
			left = append(left, commit)
			errs = append(errs, fmt.Errorf("full CI of %s: %w", commit, err))
			continue
		}
		names = append(names, name)
	}
	f.pending = left
	if len(names) > 0 {
		text := "Full CI started: " + strings.Join(names, ", ") + "."
		if err := store.NoteFullCI(ctx, c.DB, text, store.FullCI{Started: names}); err != nil {
			errs = append(errs, err)
		}
	}
	return len(names), errors.Join(errs...)
}

// fullCIReds handles each red run not handled yet, oldest first: one an
// open goal of an earlier red with the same failed tests covers is noted
// with that goal; any other proposes a goal (key fullci:<run>, so a retry
// gets the same goal) and is noted with it.
func (c *Controller) fullCIReds(ctx context.Context, f *FullCI, runs []landingci.FullRun) (int, error) {
	proposed := 0
	var errs []error
	for i, r := range runs {
		if r.Status != "Failed" {
			continue
		}
		start := time.Now()
		did, err := c.fullCIRed(ctx, f, r, runs[:i])
		c.slow(slowStepLog, start, "agentrt-full-ci: red %s", r.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("full CI red %s: %w", r.Name, err))
		}
		if did {
			proposed++
		}
	}
	return proposed, errors.Join(errs...)
}

func (c *Controller) fullCIRed(ctx context.Context, f *FullCI, r landingci.FullRun, before []landingci.FullRun) (bool, error) {
	if noted, err := store.FullCIRedNoted(ctx, c.DB, r.Name); err != nil || noted {
		return false, err
	}
	failed, from, log := r.FailedTests, "its failed-tests result", ""
	if !r.HasFailedTests {
		from = "its test-api log"
		if f.TestLog != nil {
			if l, err := f.TestLog(ctx, r.Name); err == nil {
				log = l
			} else {
				c.logf("agentrt-full-ci: %s's test-api log: %v", r.Name, err)
			}
		}
		failed = failedInLog(log)
	}
	failed = slices.Compact(slices.Sorted(slices.Values(failed)))
	key := strings.Join(failed, " ")
	openRun, openMission, err := store.FullCIOpenGoal(ctx, c.DB, key)
	if err != nil {
		return false, err
	}
	note := store.FullCI{Run: r.Name, Commit: r.Revision, FailedKey: key}
	if openMission != "" {
		note.OpenMission, note.OpenRun = openMission, openRun
		text := fmt.Sprintf("Full CI run %s of %s ended red with the failed tests of %s, whose goal %s is still open; no goal is proposed.",
			r.Name, r.Revision, openRun, openMission)
		return false, store.NoteFullCI(ctx, c.DB, text, note)
	}
	var green *landingci.FullRun
	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Status == "Succeeded" {
			green = &before[i]
			break
		}
	}
	var landings []FullCILanding
	var lerr error
	if green != nil && f.Landings != nil {
		landings, lerr = f.Landings(ctx, green.Revision, r.Revision, FullCILandingsMax+1)
	}
	title := "Fix the red full CI run " + r.Name
	text := fullCIGoalText(r, failed, from, green, landings, lerr, f.Landings != nil)
	out, err := store.ProposeGoal(ctx, c.DB, store.Writer{Actor: "controller"}, "fullci:"+r.Name, store.Goal{Title: title, Text: text,
		Repos: []string{"bifrost-platform"}, Main: func(ctx context.Context, _ string) (string, error) { return f.Main(ctx) }})
	if err != nil {
		return false, err
	}
	note.Mission, _ = out["mission_id"].(string)
	return true, store.NoteFullCI(ctx, c.DB, fmt.Sprintf("Full CI run %s of %s ended red; goal %s proposed: %s.", r.Name, r.Revision, note.Mission, title), note)
}

// failedTest is a failed Go test in a test log.
var failedTest = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

func failedInLog(log string) []string {
	var out []string
	for _, m := range failedTest.FindAllStringSubmatch(log, -1) {
		out = append(out, m[1])
	}
	return out
}

// fullCIGoalText is a red run's goal: the run and its commit, its failed
// tests, the last green full run and the landings since, and what to do.
func fullCIGoalText(r landingci.FullRun, failed []string, from string, green *landingci.FullRun,
	landings []FullCILanding, lerr error, canRead bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The full CI run %s of bifrost-platform commit %s (created %s) ended red.\n\n", r.Name, r.Revision, r.CreatedAt.UTC().Format(time.RFC3339))
	switch {
	case len(failed) > 0:
		fmt.Fprintf(&b, "Failed tests (from %s):\n", from)
		for i, t := range failed {
			if i == fullCITestsMax {
				fmt.Fprintf(&b, "- and %d more\n", len(failed)-i)
				break
			}
			fmt.Fprintf(&b, "- %s\n", t)
		}
	case r.HasFailedTests:
		b.WriteString("The run's failed-tests result names no test: read the run's test-api log for what failed.\n")
	default:
		b.WriteString("The run names no failed tests: read the run's test-api log for what failed.\n")
	}
	b.WriteString("\n")
	switch {
	case green == nil:
		fmt.Fprintf(&b, "No full run in the last %d days was green, so there is no range of landings to list.\n", int(fullCIWindow.Hours()/24))
	default:
		fmt.Fprintf(&b, "The last green full run is %s of %s. ", green.Name, green.Revision)
		switch {
		case !canRead:
			b.WriteString("The landings since could not be read here.\n")
		case lerr != nil:
			fmt.Fprintf(&b, "The landings since could not be read: %v.\n", lerr)
		case len(landings) == 0:
			b.WriteString("main has no first-parent commit between it and this run's commit.\n")
		default:
			b.WriteString("The landings between its commit and this one (first-parent commits of main, newest first):\n")
			for i, l := range landings {
				if i == FullCILandingsMax {
					b.WriteString("- and more, not listed\n")
					break
				}
				fmt.Fprintf(&b, "- %s %s\n", l.Commit, l.Subject)
			}
		}
	}
	b.WriteString("\nReproduce the failing tests at main's head. If the cause is not clear from those landings' diffs, bisect by running " +
		"only the failing tests at their commits. Fix forward, with a test.\n")
	return b.String()
}
