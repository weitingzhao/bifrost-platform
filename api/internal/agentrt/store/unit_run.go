package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// The four ways a unit task ends (0019_unit_run.sql). Only the first three
// judged the content; an infra run says nothing about it.
const (
	UnitRunPassed = "passed"
	UnitRunTest   = "test"
	UnitRunBuild  = "build"
	UnitRunInfra  = "infra"
)

// UnitRun is one row of agentrt.unit_run: one TaskRun of one unit task, or of
// one part of it. The pointers are the columns an infra run leaves empty.
type UnitRun struct {
	Plan           string
	Repo           string
	Unit           string
	Task           string
	InputKey       string
	CommitSHA      string
	ShardIndex     int
	ShardTotal     int
	Attempt        int
	Class          string
	Reason         string
	Message        string
	ExitCode       *int
	FailedTests    []string
	FailedTestsCut bool
	TestsExpected  *int
	TestsRan       *int
	TestsSkipped   *int
	TestsListSHA   *string
	QueuedSeconds  *int
	FetchSeconds   *int
	RunSeconds     *int
	Node           *string
	RunName        string
	RunUID         string
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

// RecordUnitRun appends the run; a TaskRun already recorded writes nothing
// and reports false.
func RecordUnitRun(ctx context.Context, db DB, r UnitRun) (bool, error) {
	wrote := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		failed := r.FailedTests
		if failed == nil {
			failed = []string{}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO agentrt.unit_run (plan, repo, unit, task, input_key, commit_sha, shard_index, shard_total, attempt,
		    class, reason, message, exit_code, failed_tests, failed_tests_cut, tests_expected, tests_ran, tests_skipped, tests_list_sha,
		    queued_seconds, fetch_seconds, run_seconds, node, run_name, run_uid, started_at, completed_at)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
		  ON CONFLICT (run_uid) DO NOTHING`,
			r.Plan, r.Repo, r.Unit, r.Task, r.InputKey, r.CommitSHA, r.ShardIndex, r.ShardTotal, r.Attempt,
			r.Class, r.Reason, r.Message, r.ExitCode, failed, r.FailedTestsCut, r.TestsExpected, r.TestsRan, r.TestsSkipped, r.TestsListSHA,
			r.QueuedSeconds, r.FetchSeconds, r.RunSeconds, r.Node, r.RunName, r.RunUID, r.StartedAt, r.CompletedAt)
		wrote = err == nil && tag.RowsAffected() == 1
		return err
	})
	return wrote, err
}

// UnitRunsByKey returns the runs of one unit task on one content, oldest
// first, at most limit.
func UnitRunsByKey(ctx context.Context, db DB, repo, unit, task, key string, limit int) ([]UnitRun, error) {
	var out []UnitRun
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT plan, repo, unit, task, input_key, commit_sha, shard_index, shard_total, attempt, class, reason, message,
		    exit_code, failed_tests, failed_tests_cut, tests_expected, tests_ran, tests_skipped, tests_list_sha,
		    queued_seconds, fetch_seconds, run_seconds, node, run_name, run_uid, started_at, completed_at
		  FROM agentrt.unit_run WHERE repo = $1 AND unit = $2 AND task = $3 AND input_key = $4 ORDER BY unit_run_id LIMIT $5`,
			repo, unit, task, key, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r UnitRun
			if err = rows.Scan(&r.Plan, &r.Repo, &r.Unit, &r.Task, &r.InputKey, &r.CommitSHA, &r.ShardIndex, &r.ShardTotal, &r.Attempt, &r.Class, &r.Reason, &r.Message,
				&r.ExitCode, &r.FailedTests, &r.FailedTestsCut, &r.TestsExpected, &r.TestsRan, &r.TestsSkipped, &r.TestsListSHA,
				&r.QueuedSeconds, &r.FetchSeconds, &r.RunSeconds, &r.Node, &r.RunName, &r.RunUID, &r.StartedAt, &r.CompletedAt); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
