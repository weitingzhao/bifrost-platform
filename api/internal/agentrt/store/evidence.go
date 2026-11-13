package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// The evidence ledger (migration 0017, CI design v4 §3.3): one row says one
// task ran on one unit with the inputs its key names, and passed or failed.
// In this wave a repository is one unit and the task is its CI pipeline. The
// platform writes a row when it sees a CI run complete; nothing decides by
// the ledger yet.

// EvidenceUnitRepo is the unit of a whole repository.
const EvidenceUnitRepo = "repo"

// Evidence results and scopes. Only a passed row of scope full is evidence
// for the whole unit; an incremental or console run tested part of it.
const (
	EvidencePassed      = "passed"
	EvidenceFailed      = "failed"
	EvidenceInvalidated = "invalidated"
	EvidenceScopeFull   = "full"
)

// RepoAt is a repository at a commit, with that commit's git tree.
type RepoAt struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit,omitempty"`
	Tree   string `json:"tree,omitempty"`
}

// EvidenceRow is one row of the ledger.
type EvidenceRow struct {
	ID         int64           `json:"evidence_id"`
	Repo       string          `json:"repo"`
	Unit       string          `json:"unit"`
	Task       string          `json:"task"`
	InputKey   string          `json:"input_key"`
	Result     string          `json:"result"`
	Scope      string          `json:"scope"`
	Inputs     json.RawMessage `json:"inputs"`
	Statement  json.RawMessage `json:"statement"`
	CommitSHA  string          `json:"commit_sha"`
	RunName    string          `json:"run_name"`
	RunUID     string          `json:"run_uid"`
	RecordedAt time.Time       `json:"recorded_at"`
	// audit: a failed row of the daily full CI. RecordEvidence writes an
	// invalidated row beside it when the ledger is green for its key.
	audit bool
}

var gitObject = regexp.MustCompile(`^[0-9a-f]{40}$`)

// EvidenceInputs is the document a key is the sha256 of, and that key: the
// task, the repository and its git tree, and the tree of every repository
// used beside it, by name. Commits are not in it: the same content under
// another commit has the same key.
func EvidenceInputs(task, repo, tree string, beside []RepoAt) (map[string]any, string, error) {
	if task == "" || repo == "" || !gitObject.MatchString(tree) {
		return nil, "", fmt.Errorf("%w: evidence inputs need a task, a repository and its tree", ErrInvalid)
	}
	with := map[string]any{}
	for _, b := range beside {
		if b.Repo == "" || b.Repo == repo || !gitObject.MatchString(b.Tree) {
			return nil, "", fmt.Errorf("%w: evidence inputs: %q beside %s has no tree", ErrInvalid, b.Repo, repo)
		}
		if _, twice := with[b.Repo]; twice {
			return nil, "", fmt.Errorf("%w: evidence inputs name %s twice", ErrInvalid, b.Repo)
		}
		with[b.Repo] = b.Tree
	}
	inputs := map[string]any{"task": task, "repo": repo, "tree": tree, "beside": with}
	key, err := canon.Sum(inputs)
	return inputs, key, err
}

// RunEvidence is the evidence a completed CI run is, and whether it is any.
// It is none when the run is not a CI run of a repository, was reused from
// another run (that run is the evidence), ended for a reason other than its
// tasks passing or failing (cancelled, timed out, could not start), or does
// not say what it tested: its own commit and tree, and a tree for every
// repository it reports beside it. The caller fills in trees the run did not
// report before it asks.
func RunEvidence(p PipelineRun) (EvidenceRow, bool, error) {
	if p.Kind != "ci" || p.Repo == "" || p.Run == "" || p.UID == "" || len(p.Inputs) == 0 {
		return EvidenceRow{}, false, nil
	}
	if p.TestedBy != "" && p.TestedBy != p.Run {
		return EvidenceRow{}, false, nil
	}
	result := ""
	switch {
	case p.Result == "Succeeded":
		result = EvidencePassed
	case p.Result == "Failed" && p.Reason == "Failed":
		result = EvidenceFailed
	default:
		return EvidenceRow{}, false, nil
	}
	var own *RepoAt
	var beside []RepoAt
	for i := range p.Inputs {
		in := p.Inputs[i]
		if !gitObject.MatchString(in.Tree) || !gitObject.MatchString(in.Commit) {
			return EvidenceRow{}, false, nil
		}
		if in.Repo == p.Repo {
			own = &p.Inputs[i]
			continue
		}
		beside = append(beside, in)
	}
	if own == nil {
		return EvidenceRow{}, false, nil
	}
	scope := p.Scope
	if scope == "" {
		scope = EvidenceScopeFull
	}
	inputs, key, err := EvidenceInputs(p.Pipeline, p.Repo, own.Tree, beside)
	if err != nil {
		return EvidenceRow{}, false, err
	}
	sort.Slice(beside, func(i, j int) bool { return beside[i].Repo < beside[j].Repo })
	subject := []any{}
	for _, in := range append([]RepoAt{*own}, beside...) {
		subject = append(subject, map[string]any{"name": in.Repo, "digest": map[string]any{"gitCommit": in.Commit, "gitTree": in.Tree}})
	}
	// An in-toto Statement v1 with the test-result predicate, so a row can be
	// signed or exported as it is.
	predicate := map[string]any{
		"result":        map[string]string{EvidencePassed: "PASSED", EvidenceFailed: "FAILED"}[result],
		"configuration": []any{map[string]any{"name": p.Pipeline, "uri": "tekton://cicd/pipelines/" + p.Pipeline}},
		"url":           "tekton://cicd/pipelineruns/" + p.Run,
		"scope":         scope,
		"run_uid":       p.UID,
		"completed_at":  p.CompletedAt.UTC().Format(time.RFC3339),
	}
	if result == EvidenceFailed {
		// What it failed on, when the run's tasks said (the statement is not
		// in the key: the same inputs with or without it are the same key).
		if len(p.FailedTests) > 0 {
			predicate["failedTests"] = evidenceNames(p.FailedTests, maxFailedTests)
		}
		if len(p.FailedTasks) > 0 {
			predicate["failed_tasks"] = evidenceNames(p.FailedTasks, maxFailedTests)
		}
		if p.FailClass == FailClassTest || p.FailClass == FailClassBuild {
			predicate["fail_class"] = p.FailClass
		}
	}
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       subject,
		"predicateType": "https://in-toto.io/attestation/test-result/v0.1",
		"predicate":     predicate,
	}
	e := EvidenceRow{Repo: p.Repo, Unit: EvidenceUnitRepo, Task: p.Pipeline, InputKey: key, Result: result, Scope: scope,
		CommitSHA: own.Commit, RunName: p.Run, RunUID: p.UID, audit: p.Audit && result == EvidenceFailed}
	if e.Inputs, err = canon.Encode(inputs); err != nil {
		return EvidenceRow{}, false, err
	}
	if e.Statement, err = canon.Encode(statement); err != nil {
		return EvidenceRow{}, false, err
	}
	return e, true, nil
}

// RecordEvidence appends rows and returns how many were new: a row the ledger
// already has (the same run, unit, task, key and result) is left as it is.
func RecordEvidence(ctx context.Context, db DB, rows []EvidenceRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	n := 0
	var errs []error
	// One transaction a row: a row the table refuses does not hold back the others.
	for _, e := range rows {
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			const insert = `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
			  VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11) ON CONFLICT (run_uid, repo, unit, task, input_key, result) DO NOTHING`
			if e.audit {
				// The daily full run failed on inputs the ledger holds a full
				// pass for: the pass is invalidated (until a later full pass).
				var before []EvidenceRow
				rows, err := tx.Query(ctx, `SELECT result, scope FROM agentrt.evidence
				  WHERE repo = $1 AND unit = $2 AND task = $3 AND input_key = $4 ORDER BY evidence_id`, e.Repo, e.Unit, e.Task, e.InputKey)
				if err != nil {
					return err
				}
				for rows.Next() {
					var r EvidenceRow
					if err = rows.Scan(&r.Result, &r.Scope); err != nil {
						rows.Close()
						return err
					}
					before = append(before, r)
				}
				rows.Close()
				if err = rows.Err(); err != nil {
					return err
				}
				if EvidenceVerdict(before) == EvidenceGreen {
					if _, err = tx.Exec(ctx, insert, e.Repo, e.Unit, e.Task, e.InputKey, EvidenceInvalidated, e.Scope, string(e.Inputs), string(e.Statement), e.CommitSHA, e.RunName, e.RunUID); err != nil {
						return err
					}
				}
			}
			tag, err := tx.Exec(ctx, insert,
				e.Repo, e.Unit, e.Task, e.InputKey, e.Result, e.Scope, string(e.Inputs), string(e.Statement), e.CommitSHA, e.RunName, e.RunUID)
			if err == nil && tag.RowsAffected() == 1 {
				n++
			}
			return err
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("evidence of run %s: %w", e.RunName, err))
		}
	}
	return n, errors.Join(errs...)
}

// Evidence verdicts: what the rows of one key say.
const (
	EvidenceGreen = "green" // a passed row of scope full that no later row invalidated
	EvidenceRed   = "red"   // not green, and a failed row
	EvidenceNone  = "none"  // neither: nothing ran on these inputs, or only part of the unit was tested
)

// EvidenceVerdict reads the rows of one key, oldest first.
func EvidenceVerdict(rows []EvidenceRow) string {
	green, red := false, false
	for _, r := range rows {
		switch {
		case r.Result == EvidencePassed && r.Scope == EvidenceScopeFull:
			green = true
		case r.Result == EvidenceInvalidated:
			green = false
		case r.Result == EvidenceFailed:
			red = true
		}
	}
	switch {
	case green:
		return EvidenceGreen
	case red:
		return EvidenceRed
	}
	return EvidenceNone
}

const evidenceColumns = `evidence_id, repo, unit, task, input_key, result, scope, inputs::text, statement::text, commit_sha, run_name, run_uid, recorded_at`

func evidenceQuery(ctx context.Context, db DB, sql string, args ...any) ([]EvidenceRow, error) {
	out := []EvidenceRow{}
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		out = out[:0]
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e EvidenceRow
			var inputs, statement string
			if err = rows.Scan(&e.ID, &e.Repo, &e.Unit, &e.Task, &e.InputKey, &e.Result, &e.Scope, &inputs, &statement, &e.CommitSHA, &e.RunName, &e.RunUID, &e.RecordedAt); err != nil {
				return err
			}
			e.Inputs, e.Statement = json.RawMessage(inputs), json.RawMessage(statement)
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

// EvidenceTasks is the newest row of each task the ledger has for a unit of
// a repository: which tasks give evidence for it, and what each used beside
// the repository the last time it ran.
func EvidenceTasks(ctx context.Context, db DB, repo, unit string) ([]EvidenceRow, error) {
	if unit == EvidenceUnitRepo {
		// The repository's CI pipelines, not the tasks of the unit a plan
		// names "repo" (see ciRunRows).
		return evidenceQuery(ctx, db, `SELECT DISTINCT ON (task) `+evidenceColumns+` FROM agentrt.evidence
		  WHERE repo = $1 AND `+ciRunRows+` ORDER BY task, evidence_id DESC`, repo)
	}
	return evidenceQuery(ctx, db, `SELECT DISTINCT ON (task) `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND unit = $2 ORDER BY task, evidence_id DESC`, repo, unit)
}

// ciRunRows is the rows a CI pipeline's run wrote (RunEvidence): the whole
// repository as the unit, and a key document of the first wave, which has no
// version. A unit task's row (UnitEvidenceRow) is neither, even when its unit
// is the one a plan names "repo": its key document says "v". Whether a
// commit's CI passed is asked of these rows alone (review 2026-11-13, A1:
// asked of every row, one unit task's pass answered for the whole commit).
const ciRunRows = `unit = '` + EvidenceUnitRepo + `' AND NOT (inputs ? 'v')`

// EvidenceCIByCommit is the newest rows the CI pipelines' runs recorded for a
// commit, newest first: what says whether the commit's CI passed.
func EvidenceCIByCommit(ctx context.Context, db DB, repo, commit string, limit int) ([]EvidenceRow, error) {
	return evidenceQuery(ctx, db, `SELECT `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND commit_sha = $2 AND `+ciRunRows+` ORDER BY evidence_id DESC LIMIT $3`, repo, commit, limit)
}

// EvidenceByKey is the rows of one key, oldest first.
func EvidenceByKey(ctx context.Context, db DB, repo, unit, task, key string) ([]EvidenceRow, error) {
	return evidenceQuery(ctx, db, `SELECT `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND unit = $2 AND task = $3 AND input_key = $4 ORDER BY evidence_id`, repo, unit, task, key)
}

// EvidenceByCommit is the newest rows recorded for a commit of a repository,
// whatever their key, newest first.
func EvidenceByCommit(ctx context.Context, db DB, repo, commit string, limit int) ([]EvidenceRow, error) {
	return evidenceQuery(ctx, db, `SELECT `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND commit_sha = $2 ORDER BY evidence_id DESC LIMIT $3`, repo, commit, limit)
}

// EvidenceByTree is the newest rows the CI pipelines' runs recorded for a git
// tree of a repository, under whatever commit and whatever was beside it,
// newest first.
func EvidenceByTree(ctx context.Context, db DB, repo, tree string, limit int) ([]EvidenceRow, error) {
	return evidenceQuery(ctx, db, `SELECT `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND `+ciRunRows+` AND inputs->>'tree' = $2 ORDER BY evidence_id DESC LIMIT $3`, repo, tree, limit)
}

// The classes of a failed run (PipelineRun.FailClass).
const (
	FailClassTest  = "test"
	FailClassBuild = "build"
	// maxFailedTests bounds the names one row keeps.
	maxFailedTests = 200
)

func evidenceNames(in []string, limit int) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		if len(out) == limit {
			break
		}
		out = append(out, s)
	}
	return out
}
