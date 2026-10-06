// Package lineage answers "which agent thread produced which commits" from the
// commit trailers stamped by the workspace git hooks:
//
//	Claude-Session:    desktop thread id (local_…)
//	Claude-Transcript: CLI session id
//	Change-Id:         stable across rebase / cherry-pick / amend / version bump
//
// It reads git history from the Gitea mirror (read-only) and knows nothing
// about what the repos contain.
package lineage

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Commit is one commit with its lineage trailers.
type Commit struct {
	Repo       string    `json:"repo"`
	SHA        string    `json:"sha"`
	Subject    string    `json:"subject"`
	At         time.Time `json:"at"`
	Session    string    `json:"session,omitempty"`
	Transcript string    `json:"transcript,omitempty"`
	ChangeID   string    `json:"change_id,omitempty"`
	// Ref is "main" or the branch the commit was found on.
	Ref string `json:"ref"`
	// Landed: the commit is on main, or a main commit carries its Change-Id.
	Landed bool `json:"landed"`
	// LandedSHA is the main commit that carries the change when it differs from SHA.
	LandedSHA string `json:"landed_sha,omitempty"`
	// LandedBy says how landing was decided: "sha" (on main), "change_id" (a main
	// commit carries its Change-Id, e.g. squashed) or "subject" (no Change-Id; a main
	// commit has the same subject — the pre-trailer heuristic, weaker).
	LandedBy string `json:"landed_by,omitempty"`
}

// RepoCommits groups a thread's commits in one repo, newest first.
type RepoCommits struct {
	Repo    string   `json:"repo"`
	Commits []Commit `json:"commits"`
}

// Thread is everything one agent thread committed inside the window.
// Session is empty for commits that carry a Change-Id but no session trailer
// (Cursor, or a commit made outside an agent).
type Thread struct {
	Session     string        `json:"session"`
	Transcripts []string      `json:"transcripts"`
	Link        string        `json:"link,omitempty"`
	FirstAt     time.Time     `json:"first_at"`
	LastAt      time.Time     `json:"last_at"`
	CommitCount int           `json:"commit_count"`
	Landed      int           `json:"landed"`
	Repos       []RepoCommits `json:"repos"`
}

// RepoCoverage says how much of a repo's main history in the window carries lineage.
type RepoCoverage struct {
	Repo string `json:"repo"`
	// Commits on main in the window.
	MainCommits int `json:"main_commits"`
	// WithSession carries a Claude-Session trailer.
	WithSession int `json:"with_session"`
	// WithChangeID carries a Change-Id trailer.
	WithChangeID int `json:"with_change_id"`
	// AgentNoLineage has a Co-Authored-By: Claude line but no session trailer
	// (made before the hooks, or where they did not run).
	AgentNoLineage int `json:"agent_no_lineage"`
	// Branches scanned besides main.
	Branches int `json:"branches"`
}

// Response is GET /api/v1/lineage.
type Response struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Since        time.Time          `json:"since"`
	Days         int                `json:"days"`
	Reachability probe.Reachability `json:"reachability"`
	Threads      []Thread           `json:"threads"`
	Coverage     []RepoCoverage     `json:"coverage"`
	Errors       []string           `json:"errors"`
}
