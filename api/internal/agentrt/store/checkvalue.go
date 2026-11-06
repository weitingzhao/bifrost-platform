package store

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The check value projection (GET /check-value; the Owner 2026-11-05: every
// check and retry shows its value or goes): for each check of the chain and
// each kind of retry, per ISO week in UTC, how often it ran, what it caught,
// what of that no later check would have caught (unique), what it flagged
// that turned out fine (noise), and the machine's and the Owner's minutes it
// cost. As the progress projection (progress.go) it is computed on every read
// from the runtime's own records, in one read-only snapshot, never stored;
// a number the records cannot give is null with its reason in the cell's
// unknown, never 0. The reads (checkValueRead) are apart from the fold
// (checkValueFold), which does no I/O.
//
// A row that ran in each of the last checkValueMarkWeeks full weeks and
// caught nothing unique in any of them is marked for deletion; the
// controller's pass (controller/checkvalue.go) proposes the goal of deleting
// it and records a check_value note on the controller's stream, whose
// mission the mark carries.

// The weeks a read may ask for: the full weeks before the current one.
const (
	CheckValueWeeksDefault = 8
	CheckValueWeeksMax     = 12
)

// checkValueMarkWeeks is how many full weeks a row runs with nothing unique
// before it is marked; checkValueOwnerMinutes the Owner's minutes an answer
// on a card is estimated at.
const (
	checkValueMarkWeeks    = 4
	checkValueOwnerMinutes = 2
)

// checkValueRecordedFrom is the release from which attempt_computed carries
// self_check and question_seen the option the card recommended (R5:5.7, the
// check value table): what came before says neither.
const checkValueRecordedFrom = "recorded from R5:5.7"

// CheckValueCell is one row's numbers in one week. A nil number is unknown,
// its reason in Unknown under the field's name; owner_minutes_basis says how
// an Owner's minutes were estimated.
type CheckValueCell struct {
	Week         string            `json:"week"`
	Ran          *int              `json:"ran"`
	Caught       *int              `json:"caught"`
	Unique       *int              `json:"unique"`
	Noise        *int              `json:"noise"`
	Minutes      *float64          `json:"minutes"`
	OwnerMinutes *float64          `json:"owner_minutes"`
	Unknown      map[string]string `json:"unknown"`
}

// CheckValueMark is a row marked for deletion since the first week of its
// run of weeks with nothing unique, and the mission of the goal the
// controller proposed for it ("" before it has).
type CheckValueMark struct {
	Kind    string `json:"kind"`
	Since   string `json:"since"`
	Mission string `json:"mission,omitempty"`
}

// CheckValueRow is a check (kind check) or a kind of retry (kind retry), in
// the order of the check chain.
type CheckValueRow struct {
	Key   string           `json:"key"`
	Kind  string           `json:"kind"`
	Label string           `json:"label"`
	Order int              `json:"order"`
	Cells []CheckValueCell `json:"cells"`
	Mark  *CheckValueMark  `json:"mark"`
}

// CheckValueDoc is the projection: its moment, its weeks oldest first (the
// last one is the current, partial week) and its rows.
type CheckValueDoc struct {
	AsOf  string          `json:"as_of"`
	Weeks []string        `json:"weeks"`
	Rows  []CheckValueRow `json:"rows"`
}

// checkValueRowDefs are the rows: the check chain in its order, then the
// retries.
var checkValueRowDefs = []struct{ key, kind, label string }{
	{"self_check", "check", "Executor self-check"},
	{"deterministic", "check", "Deterministic verification"},
	{"review", "check", "Review"},
	{"landing_ci", "check", "Landing CI"},
	{"stg_release", "check", "STG release"},
	{"b5_coverage", "check", "B5 coverage"},
	{"owner_cards", "check", "Owner cards"},
	{"new_round", "retry", "New round"},
	{"landing_retry", "retry", "Landing and STG retry"},
	{"budget_grant", "retry", "Budget grant"},
}

// checkValueLaterChecks is what a later check runs, for whether a catch is
// unique: the commands of the spec's verify (Repos[repo].Verify, the
// deterministic check) and, where the landing runs a CI, what that CI runs.
// Source of the landing CI's: the description of the bifrost-ci-platform
// pipeline (bifrost-trade-infra; rules.LandingRepositories names it): go vet,
// go test and go build of the API module, and the Console's type-check, lint,
// test and build. bifrost-trade-infra's landing CI states no commands here,
// so nothing counts as run by it.
var checkValueLaterChecks = map[string]struct {
	source     string
	goModule   string   // the module the CI's go commands run in
	goCommands []string // go subcommands
	consoleDir string   // the npm prefix of the Console
	npmScripts []string // npm scripts it runs there
}{
	"bifrost-platform": {source: "the bifrost-ci-platform pipeline's description (bifrost-trade-infra)", goModule: "api",
		goCommands: []string{"vet", "test", "build"}, consoleDir: "console", npmScripts: []string{"type-check", "lint", "test", "build"}},
}

// checkValueArgv is a command without a leading env and its assignments.
func checkValueArgv(argv []string) []string {
	if len(argv) > 0 && argv[0] == "env" {
		argv = argv[1:]
		for len(argv) > 0 && strings.Contains(argv[0], "=") && !strings.HasPrefix(argv[0], "-") {
			argv = argv[1:]
		}
	}
	return argv
}

// checkValueLandingCIRuns says whether repo's landing CI runs argv.
func checkValueLandingCIRuns(repo string, argv []string) bool {
	ci, ok := checkValueLaterChecks[repo]
	if !ok {
		return false
	}
	argv = checkValueArgv(argv)
	if len(argv) == 0 {
		return false
	}
	switch argv[0] {
	case "go":
		dir, sub := "", ""
		for i := 1; i < len(argv) && sub == ""; i++ {
			switch {
			case argv[i] == "-C" && i+1 < len(argv):
				dir = argv[i+1]
				i++
			case !strings.HasPrefix(argv[i], "-"):
				sub = argv[i]
			}
		}
		return dir == ci.goModule && slices.Contains(ci.goCommands, sub)
	case "npm":
		dir, script := "", ""
		for i := 1; i < len(argv) && script == ""; i++ {
			switch {
			case argv[i] == "--prefix" && i+1 < len(argv):
				dir = argv[i+1]
				i++
			case argv[i] == "run" && i+1 < len(argv):
				script = argv[i+1]
			case argv[i] == "test":
				script = "test"
			}
		}
		return dir == ci.consoleDir && slices.Contains(ci.npmScripts, script)
	}
	return false
}

// checkValueVerifyRuns says whether repo's deterministic check runs argv.
func checkValueVerifyRuns(repo string, argv []string) bool {
	argv = checkValueArgv(argv)
	return slices.ContainsFunc(Repos[repo].Verify, func(c []string) bool { return slices.Equal(checkValueArgv(c), argv) })
}

// checkValueSelfCheckCovered says whether every command a self-check of
// repo runs is one a later check runs: the self-check runs the hand-in
// commands and the plan's own go test checks in the test directories
// (HandInChecks), which the deterministic check or the landing CI run
// again. ok is false for a repository the runtime does not know.
func checkValueSelfCheckCovered(repo string) (covered, ok bool) {
	r, ok := Repos[repo]
	if !ok {
		return false, false
	}
	for _, c := range r.HandIn {
		if !checkValueVerifyRuns(repo, c) && !checkValueLandingCIRuns(repo, c) {
			return false, true
		}
	}
	for _, dir := range r.TestDirs {
		if !checkValueLandingCIRuns(repo, []string{"go", "-C", dir, "test", "./..."}) {
			return false, true
		}
	}
	return true, true
}

// checkValueFailedCommands are the commands agentd's verify.json says
// failed (exited non-zero or timed out); ok is false when the evidence does
// not read as one or names none.
func checkValueFailedCommands(evidence []byte) ([][]string, bool) {
	var v struct {
		Commands []struct {
			Argv     []string `json:"argv"`
			ExitCode int      `json:"exit_code"`
			TimedOut bool     `json:"timed_out"`
		} `json:"commands"`
	}
	if len(evidence) == 0 || json.Unmarshal(evidence, &v) != nil {
		return nil, false
	}
	var out [][]string
	for _, c := range v.Commands {
		if (c.ExitCode != 0 || c.TimedOut) && len(c.Argv) > 0 {
			out = append(out, c.Argv)
		}
	}
	return out, len(out) > 0
}

// The rows the fold reads.

type checkValueComputed struct {
	At        time.Time
	Task      string
	SelfCheck []byte // attempt_computed's self_check, nil for none
}

type checkValueVerification struct {
	Task, Method, Verdict, Commit string
	AttemptNo                     int
	At                            time.Time
	VerifierStarted               *time.Time
	VerifierEnded                 *time.Time
	Evidence                      []byte // a failed deterministic check's report
}

type checkValueAttempt struct {
	No             int
	Commit         string
	Started, Ended *time.Time
}

type checkValueTask struct {
	State, Repo string
}

// checkValueLanding is a repository's landing stream, as LandingState folds
// it, or why it could not be read.
type checkValueLanding struct {
	Repo    string
	Entries []landing.Entry
	Grants  map[string][]landing.Interval
	Err     string
}

type checkValueAnswer struct {
	At                      time.Time
	Option, Recommended, By string
	HasRecommended          bool
}

type checkValueRework struct {
	Task string
	At   time.Time
}

type checkValueGrant struct {
	Task  string
	At    time.Time
	Owner bool
}

// checkValueNoteRow is a check_value note, with its mission's state.
type checkValueNoteRow struct {
	CheckValueNote
	MissionState string
}

type checkValueRows struct {
	// The first records that carry self_check and recommended; nil: none yet.
	selfCheckSince, recommendedSince *time.Time
	computed                         []checkValueComputed
	verifications                    []checkValueVerification
	attempts                         map[string][]checkValueAttempt // by task, by attempt_no
	tasks                            map[string]checkValueTask
	landing                          []checkValueLanding
	stg                              *StgState
	stgErr                           string
	answers                          []checkValueAnswer
	reworks                          []checkValueRework
	grants                           []checkValueGrant
	notes                            []checkValueNoteRow
}

// CheckValue computes the projection over the last weeks full weeks and the
// current one, at the database's clock.
func CheckValue(ctx context.Context, db DB, weeks int) (CheckValueDoc, error) {
	return CheckValueAt(ctx, db, weeks, time.Time{})
}

// CheckValueAt is CheckValue at at (zero: the database's clock).
func CheckValueAt(ctx context.Context, db DB, weeks int, at time.Time) (CheckValueDoc, error) {
	if weeks < 1 || weeks > CheckValueWeeksMax {
		return CheckValueDoc{}, invalid("weeks is 1 to %d, not %d", CheckValueWeeksMax, weeks)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CheckValueDoc{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT COALESCE($1, clock_timestamp())`, nullTime(at)).Scan(&now); err != nil {
		return CheckValueDoc{}, err
	}
	all := checkValueWeeksOf(now, CheckValueWeeksMax)
	rows, err := checkValueRead(ctx, tx, all[0].start)
	if err != nil {
		return CheckValueDoc{}, err
	}
	return checkValueFold(rows, now, weeks), nil
}

// checkValueRead reads the records from since on (and what they need of
// what came later, up to now), in tx.
func checkValueRead(ctx context.Context, tx pgx.Tx, since time.Time) (checkValueRows, error) {
	r := checkValueRows{attempts: map[string][]checkValueAttempt{}, tasks: map[string]checkValueTask{}}
	if err := tx.QueryRow(ctx, `SELECT (SELECT min(at) FROM agentrt.event WHERE event_type = 'attempt_computed' AND body_json ? 'self_check'),
	    (SELECT min(at) FROM agentrt.event WHERE event_type = 'question_seen' AND body_json ? 'recommended')`).
		Scan(&r.selfCheckSince, &r.recommendedSince); err != nil {
		return r, err
	}
	taskSet := map[string]bool{}
	// The implementations' endings.
	rows, err := tx.Query(ctx, `SELECT e.at, e.task_id, e.body_json->'self_check'
	  FROM agentrt.event e JOIN agentrt.task t ON t.task_id = e.task_id
	  WHERE e.event_type = 'attempt_computed' AND t.role = 'implement' AND e.at >= $1 ORDER BY e.event_id`, since)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var c checkValueComputed
		if err = rows.Scan(&c.At, &c.Task, &c.SelfCheck); err != nil {
			rows.Close()
			return r, err
		}
		taskSet[c.Task] = true
		r.computed = append(r.computed, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	// The deterministic checks and the reviews, but a failure the STG test
	// switch injected (TestNeedFailFirstVerify). A landing_ci verification's
	// pass is an acceptance on submission, before any CI ran: the landing
	// CI's runs are read from its stream below.
	rows, err = tx.Query(ctx, `SELECT v.task_id, v.method, v.verdict, COALESCE(a.pushed_commit, v.subject_commit, ''), a.attempt_no, v.created_at,
	    va.started_at, va.ended_at, b.body
	  FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id
	  LEFT JOIN agentrt.attempt va ON va.attempt_id = v.verifier_attempt_id
	  LEFT JOIN agentrt.artifact_blob b ON b.artifact_id = v.evidence_artifact_id AND v.method = 'deterministic' AND v.verdict = 'fail'
	  WHERE v.method IN ('deterministic', 'review') AND v.check_ref IS DISTINCT FROM $2 AND v.created_at >= $1
	  ORDER BY v.created_at, v.verification_id`, since, TestNeedFailFirstVerify)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var v checkValueVerification
		if err = rows.Scan(&v.Task, &v.Method, &v.Verdict, &v.Commit, &v.AttemptNo, &v.At, &v.VerifierStarted, &v.VerifierEnded, &v.Evidence); err != nil {
			rows.Close()
			return r, err
		}
		taskSet[v.Task] = true
		r.verifications = append(r.verifications, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	// The Owner's and the rules' answers: each question's first answer.
	rows, err = tx.Query(ctx, `SELECT DISTINCT ON (e.body_json->>'question') e.at, COALESCE(e.body_json->>'option', ''),
	    COALESCE(e.body_json->>'recommended', ''), e.body_json ? 'recommended', COALESCE(e.body_json->>'answered_by', '')
	  FROM agentrt.event e WHERE e.event_type = 'question_seen' AND e.body_json->>'status' = $2 AND e.at >= $1
	  ORDER BY e.body_json->>'question', e.event_id`, since, QuestionAnswered)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var a checkValueAnswer
		if err = rows.Scan(&a.At, &a.Option, &a.Recommended, &a.HasRecommended, &a.By); err != nil {
			rows.Close()
			return r, err
		}
		r.answers = append(r.answers, a)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	// New rounds, and the budget grants that add attempts to a task's
	// mission (missionbudget.go: the Owner's on a card, the controller's own
	// or for the rule that answered for him).
	rows, err = tx.Query(ctx, `SELECT e.event_type, e.task_id, e.at, e.actor_id = 'owner' FROM agentrt.event e
	  WHERE e.task_id IS NOT NULL AND e.at >= $1 AND ((e.event_type = 'task_state_changed' AND e.body_json->>'to' = 'rework')
	    OR (e.event_type = 'mission_amended' AND e.body_json ? 'max_attempts' AND e.actor_id IN ('owner', 'controller')))
	  ORDER BY e.event_id`, since)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var typ, task string
		var at time.Time
		var owner bool
		if err = rows.Scan(&typ, &task, &at, &owner); err != nil {
			rows.Close()
			return r, err
		}
		taskSet[task] = true
		if typ == "task_state_changed" {
			r.reworks = append(r.reworks, checkValueRework{Task: task, At: at})
		} else {
			r.grants = append(r.grants, checkValueGrant{Task: task, At: at, Owner: owner})
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	// The landing streams, as LandingState folds them; one that does not
	// read is unknown, not empty.
	repos := make([]string, 0, len(rules.LandingRepositories))
	for repo := range rules.LandingRepositories {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	for _, repo := range repos {
		l := checkValueLanding{Repo: repo}
		if s, err := scanLandingStream(ctx, tx, "landing:"+repo); err != nil {
			if ctx.Err() != nil {
				return r, err
			}
			l.Err = err.Error()
		} else {
			l.Entries, l.Grants = s.entries, grantPeriods(s.order)
			for _, e := range s.entries {
				if t := e.Record.String("task"); t != "" {
					taskSet[t] = true
				}
			}
		}
		r.landing = append(r.landing, l)
	}
	if s, err := StgReleaseState(ctx, tx); err != nil {
		if ctx.Err() != nil {
			return r, err
		}
		r.stgErr = err.Error()
	} else {
		r.stg = &s
		for _, b := range s.Batches {
			for m := range b.Members {
				taskSet[m.Root] = true
			}
		}
	}
	tasks := make([]string, 0, len(taskSet))
	for t := range taskSet {
		tasks = append(tasks, t)
	}
	rows, err = tx.Query(ctx, `SELECT task_id, state, COALESCE(repo, '') FROM agentrt.task WHERE task_id = ANY($1)`, tasks)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var id string
		var t checkValueTask
		if err = rows.Scan(&id, &t.State, &t.Repo); err != nil {
			rows.Close()
			return r, err
		}
		r.tasks[id] = t
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	rows, err = tx.Query(ctx, `SELECT task_id, attempt_no, COALESCE(pushed_commit, ''), started_at, ended_at
	  FROM agentrt.attempt WHERE task_id = ANY($1) ORDER BY task_id, attempt_no`, tasks)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var task string
		var a checkValueAttempt
		if err = rows.Scan(&task, &a.No, &a.Commit, &a.Started, &a.Ended); err != nil {
			rows.Close()
			return r, err
		}
		r.attempts[task] = append(r.attempts[task], a)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return r, err
	}
	r.notes, err = checkValueNotes(ctx, tx)
	return r, err
}

// checkValueNotes are the controller's check_value notes, oldest first,
// each with its mission's state.
func checkValueNotes(ctx context.Context, tx pgx.Tx) ([]checkValueNoteRow, error) {
	rows, err := tx.Query(ctx, `SELECT COALESCE(e.body_json->'check_value'->>'key', ''), COALESCE(e.body_json->'check_value'->>'week', ''),
	    COALESCE(e.body_json->'check_value'->>'mission', ''), COALESCE(m.state, '')
	  FROM agentrt.event e LEFT JOIN agentrt.mission m ON m.mission_id = e.body_json->'check_value'->>'mission'
	  WHERE e.event_stream_id = $1 AND e.event_type = 'note' AND e.actor_id = 'controller' AND e.body_json ? 'check_value'
	  ORDER BY e.seq`, ControllerStream)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checkValueNoteRow
	for rows.Next() {
		var n checkValueNoteRow
		if err = rows.Scan(&n.Key, &n.Week, &n.Mission, &n.MissionState); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// checkValueWeek is an ISO week in UTC: [start, end).
type checkValueWeek struct {
	label      string
	start, end time.Time
}
