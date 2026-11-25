// Package fullci is the daily full CI's decisions (verification): when the
// day's full runs start, and how a red one is noted. The controller runs the
// loop (agentrt/controller, startFullCI) and lends each pass what it holds:
// the ledger, its clock, its log and the commit STG runs (Host).
package fullci

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

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

// The daily full CI (R5:5.7-9): with incremental Go tests per landing
// (R5:5.7-1), the whole suite runs on a schedule and a red run is fixed
// forward. Once a day at At, in the Owner's zone (Host.Zone: the controller
// binds the FullCIZone the server gave it, planpace.BriefZone, the zone of
// the briefs), a full run of main's head starts, and one of the commit STG
// runs now when that differs, so a PROD promotion of STG's commit
// has a full green before releasing starts at 08:00. STG's commit also runs
// in full whenever it changes, at any hour (2026-11-24), so a release in the
// day finds its full pass ready. A commit runs at most once a day (the
// run's name is its commit and the day). A red run is noted once, with the
// pipeline tasks that failed and the commits landed since the last green
// full run whose own CI run left a module out: the full run is the net under
// those, so its red says which landings reached main without that part
// having run. The newest finished run is kept for the platform's gauge
// (Latest), which the alert rules read.

// FullCIEvery is how often the loop looks at the clock and the runs.
const FullCIEvery = time.Minute

// FullCIAt is the default time of the day's runs; fullCIWindow how far back
// the runs are read (reds to handle, the last green); FullCILandingsMax how
// many landings a red run's reconciliation looks at; fullCITestsMax how many
// failed Go tests its note's text names.
const (
	// 07:30 since 2026-11-21 (CI line): PROD is released from 08:00 in the
	// same zone on a full pass of the commit STG runs (headrelease mode on);
	// at 03:00 a landing between 03:00 and 08:00 left STG's commit without
	// one. The run takes about 3 minutes. Revert: "03:00".
	FullCIAt          = "07:30"
	fullCIWindow      = 14 * 24 * time.Hour
	FullCILandingsMax = 30
	fullCITestsMax    = 10
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
// commits after from up to to, newest first, at most max. FailedTasks names
// the pipeline tasks of a run that failed; RunModules reads, for a landed
// commit, its own CI run and which modules that run ran ("*" for all of
// them, otherwise their names each between spaces; "" for no such run). Any
// of these nil is not read, and the note says so. The commit STG runs now
// is Host.STG's (the controller binds its Sources.Deployed).
type FullCI struct {
	At       string // HH:MM; "" is FullCIAt
	Runs     func(ctx context.Context, since time.Time) ([]landingci.FullRun, error)
	Ensure   func(ctx context.Context, revision string, day time.Time) (string, error)
	Main     func(ctx context.Context) (string, error)
	TestLog  func(ctx context.Context, run string) (string, error)
	Landings func(ctx context.Context, from, to string, max int) ([]FullCILanding, error)

	FailedTasks func(ctx context.Context, run string) ([]string, error)
	RunModules  func(ctx context.Context, revision string) (run, modules string, err error)

	mu      sync.Mutex
	day     string   // the day whose runs are started, or being started
	pending []string // the commits of day still to start
	stg     string   // the STG commit last queued on its change (below)
	// The runs this process started for STG's change, by name: they are not
	// the day's runs (startFullRuns).
	changeRuns map[string]bool
	changeOf   map[string]bool // commits queued for STG's change, not yet started

	// The newest finished run of the last pass. It has its own lock: mu is
	// held for a whole pass, and a scrape of /metrics must not wait for one.
	latestMu     sync.Mutex
	latestName   string
	latestStatus string
}

// Latest is the newest finished (Succeeded or Failed) full run the last
// pass listed, by creation time and then name; ok is false when there is no
// loop or no such run.
func (f *FullCI) Latest() (name, status string, ok bool) {
	if f == nil {
		return "", "", false
	}
	f.latestMu.Lock()
	defer f.latestMu.Unlock()
	return f.latestName, f.latestStatus, f.latestName != ""
}

func (f *FullCI) setLatest(runs []landingci.FullRun) {
	var latest *landingci.FullRun
	for i, r := range runs {
		if r.Status != "Succeeded" && r.Status != "Failed" {
			continue
		}
		if latest == nil || r.CreatedAt.After(latest.CreatedAt) || (r.CreatedAt.Equal(latest.CreatedAt) && r.Name > latest.Name) {
			latest = &runs[i]
		}
	}
	f.latestMu.Lock()
	defer f.latestMu.Unlock()
	if latest == nil {
		f.latestName, f.latestStatus = "", ""
		return
	}
	f.latestName, f.latestStatus = latest.Name, latest.Status
}

// FullCISettings reads the loop's switches: AGENTRT_FULL_CI=off stops it
// (default on), AGENTRT_FULL_CI_AT is its time (HH:MM, default 07:30) and
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

// Host is what the controller lends a pass. DB is the ledger the notes go
// to; Now its clock (time.Now when nil); Logf and Slow its log (nothing is
// logged when nil; Slow logs a step that took the controller's step time).
// STG reads the commit STG runs now (ok false when it cannot be read); nil
// when the controller has no deployment reader, and then only main's head
// runs. Zone is the zone of the day's time: unbound (""), no run is started
// and the pass says so; red runs are still noted.
type Host struct {
	DB   eventlog.DB
	Now  func() time.Time
	Logf func(format string, args ...any)
	Slow func(start time.Time, format string, args ...any)
	STG  func(ctx context.Context) (commit string, ok bool)
	Zone string
}

func (h Host) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}

func (h Host) slow(start time.Time, format string, args ...any) {
	if h.Slow != nil {
		h.Slow(start, format, args...)
	}
}

func (h Host) clock() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Pass is one pass of the full CI loop: the day's runs once their
// time has come, then the red runs not handled yet. It returns how many
// runs it started and red runs it noted.
func (f *FullCI) Pass(ctx context.Context, h Host) (int, error) {
	if f == nil || f.Runs == nil || f.Ensure == nil || f.Main == nil {
		return 0, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := h.clock()
	runs, err := f.Runs(ctx, now.Add(-fullCIWindow))
	if err != nil {
		return 0, fmt.Errorf("full CI runs: %w", err)
	}
	f.setLatest(runs)
	started, serr := startFullRuns(ctx, h, f, now, runs)
	noted, rerr := fullCIReds(ctx, h, f, runs)
	return started + noted, errors.Join(serr, rerr)
}

// startFullRuns starts the day's runs once its time has come: main's head
// and STG's commit, read once a day. A run that cannot be started is tried
// again on the next pass; a run of the day already there (a restart) means
// the day's runs were started.
func startFullRuns(ctx context.Context, host Host, f *FullCI, now time.Time, runs []landingci.FullRun) (int, error) {
	if host.Zone == "" {
		// time.LoadLocation("") is UTC: unbound, the day's time would be read
		// in another zone. Nothing starts.
		return 0, errors.New("full CI: no zone bound; no run started")
	}
	loc, err := time.LoadLocation(host.Zone)
	if err != nil {
		return 0, err
	}
	h, m, err := fullCIClock(f.At)
	if err != nil {
		return 0, err
	}
	local := now.In(loc)
	// STG's commit is run in full when it changes, at any hour (CI line
	// 2026-11-24): a PROD release of it is judged on a full pass of that commit
	// (LedgerCIStrict), and with the day's runs alone a release in the day
	// started its own full run and waited for it (0e98b939: 20:07 on STG, its
	// full run started by hand). A commit runs at most once a day, as Ensure
	// names the run by commit and day. Revert: delete this block.
	if host.STG != nil {
		if stg, ok := host.STG(ctx); ok && stg != "" && stg != f.stg {
			f.stg = stg
			ran := false // today's run of it already exists (a restart)
			for _, r := range runs {
				ran = ran || (r.Revision == stg && strings.HasSuffix(r.Name, "-"+local.Format("20060102")))
			}
			if !ran && !slices.Contains(f.pending, stg) {
				f.pending = append(f.pending, stg)
				if f.changeOf == nil {
					f.changeOf = map[string]bool{}
				}
				f.changeOf[stg] = true
			}
		}
	}
	if day := local.Format("20060102"); f.day != day && !local.Before(time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)) {
		// The day's runs: main's head (STG's commit is queued above). A run of
		// today means they were started (a restarted controller starts none),
		// except a run this process started for STG's change.
		started := false
		for _, r := range runs {
			started = started || (strings.HasSuffix(r.Name, "-"+day) && !f.changeRuns[r.Name])
		}
		f.day = day
		if !started {
			main, err := f.Main(ctx)
			if err != nil || main == "" {
				f.day = ""
				return 0, fmt.Errorf("full CI: main's head: %v", err)
			}
			if !slices.Contains(f.pending, main) {
				f.pending = append([]string{main}, f.pending...)
			}
			if host.STG == nil {
				host.logf("agentrt-full-ci: the commit STG runs cannot be read; only main's head runs today")
			}
		}
	}
	var left, names []string
	var errs []error
	for _, commit := range f.pending {
		start := time.Now()
		name, err := f.Ensure(ctx, commit, local)
		host.slow(start, "agentrt-full-ci: start %s", commit)
		if err != nil {
			left = append(left, commit)
			errs = append(errs, fmt.Errorf("full CI of %s: %w", commit, err))
			continue
		}
		if f.changeOf[commit] {
			delete(f.changeOf, commit)
			if f.changeRuns == nil {
				f.changeRuns = map[string]bool{}
			}
			f.changeRuns[name] = true
		}
		names = append(names, name)
	}
	f.pending = left
	if len(names) > 0 {
		text := "Full CI started: " + strings.Join(names, ", ") + "."
		if err := evidence.NoteFullCI(ctx, host.DB, text, evidence.FullCI{Started: names}); err != nil {
			errs = append(errs, err)
		}
	}
	return len(names), errors.Join(errs...)
}

// fullCIReds handles each red run not handled yet, oldest first: each is
// noted once (evidence.FullCIRedNoted), with what failed and the landings that
// left a module out.
func fullCIReds(ctx context.Context, h Host, f *FullCI, runs []landingci.FullRun) (int, error) {
	noted := 0
	var errs []error
	for i, r := range runs {
		if r.Status != "Failed" {
			continue
		}
		start := time.Now()
		did, err := fullCIRed(ctx, h, f, r, runs[:i])
		h.slow(start, "agentrt-full-ci: red %s", r.Name)
		if err != nil {
			errs = append(errs, fmt.Errorf("full CI red %s: %w", r.Name, err))
		}
		if did {
			noted++
		}
	}
	return noted, errors.Join(errs...)
}

func fullCIRed(ctx context.Context, h Host, f *FullCI, r landingci.FullRun, before []landingci.FullRun) (bool, error) {
	if noted, err := evidence.FullCIRedNoted(ctx, h.DB, r.Name); err != nil || noted {
		return false, err
	}
	failed := r.FailedTests
	if !r.HasFailedTests {
		log := ""
		if f.TestLog != nil {
			if l, err := f.TestLog(ctx, r.Name); err == nil {
				log = l
			} else {
				h.logf("agentrt-full-ci: %s's test log: %v", r.Name, err)
			}
		}
		failed = failedInLog(log)
	}
	failed = slices.Compact(slices.Sorted(slices.Values(failed)))
	note := evidence.FullCI{Run: r.Name, Commit: r.Revision, FailedKey: strings.Join(failed, " ")}
	if f.FailedTasks != nil {
		tasks, err := f.FailedTasks(ctx, r.Name)
		if err != nil {
			h.logf("agentrt-full-ci: %s's failed tasks: %v", r.Name, err)
		}
		note.FailedTasks = tasks
	}
	var green *landingci.FullRun
	for i := len(before) - 1; i >= 0; i-- {
		if before[i].Status == "Succeeded" {
			green = &before[i]
			break
		}
	}
	note.Suspects, note.Unread, note.Unreconciled = fullCIReconcile(ctx, h, f, r, green)
	tasks := "no failed task could be read"
	if len(note.FailedTasks) > 0 {
		tasks = "failed tasks: " + strings.Join(note.FailedTasks, ", ")
	}
	h.logf("agentrt-full-ci: ALERT full CI run %s of %s is red; %s", r.Name, r.Revision, tasks)
	return true, evidence.NoteFullCI(ctx, h.DB, fullCIRedText(note, failed, green), note)
}

// fullCIReconcile looks, for red run r, at the commits landed since the
// last green full run: each whose own CI run ran only some of the modules is
// a suspect, recorded with that run and its run-modules as written; each
// whose CI run or result cannot be read is counted in unread. When the
// landings cannot be looked at, or not all of them, unreconciled says why,
// so that no suspect is not read as none.
func fullCIReconcile(ctx context.Context, h Host, f *FullCI, r landingci.FullRun, green *landingci.FullRun) (suspects []evidence.FullCISuspect, unread int, unreconciled string) {
	switch {
	case green == nil:
		return nil, 0, fmt.Sprintf("no full run in the last %d days was green", int(fullCIWindow.Hours()/24))
	case f.Landings == nil:
		return nil, 0, "the landings since " + green.Name + " cannot be read here"
	case f.RunModules == nil:
		return nil, 0, "the landings' own CI runs cannot be read here"
	}
	landings, err := f.Landings(ctx, green.Revision, r.Revision, FullCILandingsMax+1)
	if err != nil {
		return nil, 0, fmt.Sprintf("the landings since %s could not be read: %v", green.Name, err)
	}
	if len(landings) > FullCILandingsMax {
		landings = landings[:FullCILandingsMax]
		unreconciled = fmt.Sprintf("more than %d commits landed since %s; only the newest %d were looked at", FullCILandingsMax, green.Name, FullCILandingsMax)
	}
	for _, l := range landings {
		run, modules, err := f.RunModules(ctx, l.Commit)
		switch {
		case err != nil:
			h.logf("agentrt-full-ci: the CI run of landing %s: %v", l.Commit, err)
			unread++
		case run == "":
			unread++
		case modules != "" && strings.TrimSpace(modules) != "*":
			suspects = append(suspects, evidence.FullCISuspect{Commit: l.Commit, Subject: l.Subject, Run: run, Modules: modules})
		}
	}
	return suspects, unread, unreconciled
}

// fullCIRedText is a red run's note: the run and its commit, the failed
// tasks, the failed Go tests when known, and the landings since the last
// green full run that left a module out of their own CI run.
func fullCIRedText(n evidence.FullCI, failed []string, green *landingci.FullRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Full CI run %s of %s ended red; ", n.Run, n.Commit)
	if len(n.FailedTasks) > 0 {
		b.WriteString("failed tasks: " + strings.Join(n.FailedTasks, ", "))
	} else {
		b.WriteString("no failed task could be read")
	}
	if len(failed) > 0 {
		shown := failed
		if len(shown) > fullCITestsMax {
			shown = shown[:fullCITestsMax]
		}
		b.WriteString("; failed Go tests: " + strings.Join(shown, ", "))
		if more := len(failed) - len(shown); more > 0 {
			fmt.Fprintf(&b, " and %d more", more)
		}
	}
	b.WriteString(". ")
	if green == nil || (n.Unreconciled != "" && len(n.Suspects) == 0 && n.Unread == 0) {
		return b.String() + "Which landed commits had a module left out of their own CI run was not reconciled: " + n.Unreconciled + "."
	}
	if len(n.Suspects) == 0 {
		fmt.Fprintf(&b, "No commit landed since the last green full run %s with a module left out of its own CI run", green.Name)
	} else {
		fmt.Fprintf(&b, "%d commit(s) landed since the last green full run %s with a module left out of their own CI run: ", len(n.Suspects), green.Name)
		for i, s := range n.Suspects {
			if i > 0 {
				b.WriteString(", ")
			}
			commit := s.Commit
			if len(commit) > 8 {
				commit = commit[:8]
			}
			fmt.Fprintf(&b, "%s (%s)", commit, s.Modules)
		}
	}
	if n.Unread > 0 {
		fmt.Fprintf(&b, "; the CI run of %d more could not be read", n.Unread)
	}
	if n.Unreconciled != "" {
		b.WriteString("; " + n.Unreconciled)
	}
	return b.String() + "."
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
