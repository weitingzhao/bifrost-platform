package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A third kind of row (Owner 2026-11-14): a review record. Someone read
// files of a repository at one commit and says whether what they read
// passed. The release policy reads these for a release that changes a trust
// anchor (releasepolicy/review.go); nothing else decides by them.
//
// A review row is told from the other two kinds by its unit and task, not by
// its scope: the ledger's scope column allows only full, incremental and
// console (migration 0017), and a review read part of the repository, so it
// is written as incremental. It is therefore never a full pass, and its key
// document has a version, so no reading of a commit's CI (ciRunRows) or of a
// unit task's key finds it.
const (
	ReviewUnit  = "review"
	ReviewTask  = "release-review"
	reviewScope = "incremental"
	// Bounds of one record.
	maxReviewFiles = 500
	maxReviewPath  = 500
	maxReviewNote  = 4000
)

// Review is one review record. RecordedAt is set on a row read back.
type Review struct {
	Repo, Commit string
	// Reviewer is the session id of the thread that read the files.
	Reviewer string
	Files    []string
	Passed   bool
	// NotRead is the reviewer's own words on what they left unread.
	NotRead    string
	RecordedAt time.Time
}

// ReviewRow is the ledger row of a review made at now. Every call is its own
// row: a reviewer who records again has a newer verdict, and the newest one
// is read.
func ReviewRow(rv Review, now time.Time) (EvidenceRow, error) {
	rv.Reviewer, rv.NotRead = strings.TrimSpace(rv.Reviewer), strings.TrimSpace(rv.NotRead)
	if rv.Repo == "" || !gitObject.MatchString(rv.Commit) {
		return EvidenceRow{}, fmt.Errorf("%w: a review names a repository and a 40-character commit", eventlog.ErrInvalid)
	}
	if rv.Reviewer == "" || len(rv.Reviewer) > 128 {
		return EvidenceRow{}, fmt.Errorf("%w: a review needs the session id of who read the files", eventlog.ErrInvalid)
	}
	if len(rv.NotRead) > maxReviewNote {
		return EvidenceRow{}, fmt.Errorf("%w: not_read is over %d characters", eventlog.ErrInvalid, maxReviewNote)
	}
	seen := map[string]bool{}
	files := []any{}
	for _, f := range rv.Files {
		f = strings.TrimSpace(f)
		if f == "" || len(f) > maxReviewPath || strings.HasPrefix(f, "/") || strings.ContainsAny(f, "\n\r\x00") {
			return EvidenceRow{}, fmt.Errorf("%w: a review's files are repository-relative paths", eventlog.ErrInvalid)
		}
		if !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	if len(files) == 0 || len(files) > maxReviewFiles {
		return EvidenceRow{}, fmt.Errorf("%w: a review lists between 1 and %d files", eventlog.ErrInvalid, maxReviewFiles)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].(string) < files[j].(string) })
	result := EvidenceFailed
	if rv.Passed {
		result = EvidencePassed
	}
	at := now.UTC().Format(time.RFC3339Nano)
	inputs := map[string]any{"v": 1, "task": ReviewTask, "repo": rv.Repo, "commit": rv.Commit, "reviewer": rv.Reviewer, "at": at}
	key, err := canon.Sum(inputs)
	if err != nil {
		return EvidenceRow{}, err
	}
	statement := map[string]any{"reviewer": rv.Reviewer, "files": files, "verdict": result, "not_read": rv.NotRead, "reviewed_at": at}
	e := EvidenceRow{Repo: rv.Repo, Unit: ReviewUnit, Task: ReviewTask, InputKey: key, Result: result, Scope: reviewScope,
		CommitSHA: rv.Commit, RunName: "review-" + rv.Commit[:12], RunUID: "review:" + rv.Reviewer + ":" + at}
	if e.Inputs, err = canon.Encode(inputs); err != nil {
		return EvidenceRow{}, err
	}
	if e.Statement, err = canon.Encode(statement); err != nil {
		return EvidenceRow{}, err
	}
	return e, nil
}

// maxReviewsRead bounds one read; a commit with more review rows than this
// is read as unreadable, never as its first rows.
const maxReviewsRead = 200

// Reviews is the review records of one commit of a repository, oldest first.
// Only rows whose key document names a reviewer are read (ReviewRow writes
// no other), so a unit task that happens to carry the same unit and task
// names is not one. A row that does not read as a review is an error: a record that cannot be
// read must not be skipped as if nobody had failed the commit.
func Reviews(ctx context.Context, db eventlog.DB, repo, commit string) ([]Review, error) {
	rows, err := evidenceQuery(ctx, db, `SELECT `+evidenceColumns+` FROM agentrt.evidence
	  WHERE repo = $1 AND commit_sha = $2 AND unit = $3 AND task = $4 AND inputs ? 'reviewer' ORDER BY evidence_id LIMIT $5`,
		repo, commit, ReviewUnit, ReviewTask, maxReviewsRead+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxReviewsRead {
		return nil, fmt.Errorf("more than %d review records for %s", maxReviewsRead, commit)
	}
	out := make([]Review, 0, len(rows))
	for _, row := range rows {
		var st struct {
			Reviewer string   `json:"reviewer"`
			Files    []string `json:"files"`
			Verdict  string   `json:"verdict"`
			NotRead  string   `json:"not_read"`
		}
		if err := json.Unmarshal(row.Statement, &st); err != nil || st.Reviewer == "" || st.Verdict != row.Result ||
			(row.Result != EvidencePassed && row.Result != EvidenceFailed) {
			return nil, fmt.Errorf("review record %d of %s does not read as a review", row.ID, commit)
		}
		out = append(out, Review{Repo: row.Repo, Commit: row.CommitSHA, Reviewer: st.Reviewer, Files: st.Files,
			Passed: row.Result == EvidencePassed, NotRead: st.NotRead, RecordedAt: row.RecordedAt})
	}
	return out, nil
}
