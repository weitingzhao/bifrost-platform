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
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       subject,
		"predicateType": "https://in-toto.io/attestation/test-result/v0.1",
		"predicate": map[string]any{
			"result":        map[string]string{EvidencePassed: "PASSED", EvidenceFailed: "FAILED"}[result],
			"configuration": []any{map[string]any{"name": p.Pipeline, "uri": "tekton://cicd/pipelines/" + p.Pipeline}},
			"url":           "tekton://cicd/pipelineruns/" + p.Run,
			"scope":         scope,
			"run_uid":       p.UID,
			"completed_at":  p.CompletedAt.UTC().Format(time.RFC3339),
		},
	}
	e := EvidenceRow{Repo: p.Repo, Unit: EvidenceUnitRepo, Task: p.Pipeline, InputKey: key, Result: result, Scope: scope,
		CommitSHA: own.Commit, RunName: p.Run, RunUID: p.UID}
	if e.Inputs, err = canon.Encode(inputs); err != nil {
		return EvidenceRow{}, false, err
	}
	if e.Statement, err = canon.Encode(statement); err != nil {
		return EvidenceRow{}, false, err
	}
	return e, true, nil
}

// RecordEvidence appends rows and returns how many were new: a run has at
// most one row per result, so a row the ledger already has is left as it is.
func RecordEvidence(ctx context.Context, db DB, rows []EvidenceRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	n := 0
	var errs []error
	// One transaction a row: a row the table refuses does not hold back the others.
	for _, e := range rows {
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `INSERT INTO agentrt.evidence (repo, unit, task, input_key, result, scope, inputs, statement, commit_sha, run_name, run_uid)
			  VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11) ON CONFLICT (run_uid, result) DO NOTHING`,
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
