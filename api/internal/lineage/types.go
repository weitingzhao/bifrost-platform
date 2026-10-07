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
	// Reached lists, per lane/env, the first recorded release that contained
	// this commit (a release built a commit at or after it on the default branch).
	Reached []Reach `json:"reached,omitempty"`
}

// Reach is the first release in one lane/env that contained a commit.
type Reach struct {
	Lane    string    `json:"lane"`
	Env     string    `json:"env"`
	Run     string    `json:"run"`
	At      time.Time `json:"at"`
	Deploys bool      `json:"deploys"`
}

// Release is one recorded delivery run, as lineage needs it.
type Release struct {
	Run     string
	Lane    string
	Env     string
	Deploys bool
	At      time.Time
	// Repos: repo -> commit it was built from.
	Repos map[string]string
}

// ReleaseHead is the latest recorded release of a lane/env.
type ReleaseHead struct {
	Lane    string            `json:"lane"`
	Env     string            `json:"env"`
	Run     string            `json:"run"`
	At      time.Time         `json:"at"`
	Deploys bool              `json:"deploys"`
	Repos   map[string]string `json:"repos"`
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
	Session string `json:"session"`
	// Title is the thread's human name: set by hand (TitleSource "manual") or
	// the session title synced from its transcripts ("transcript").
	Title       string    `json:"title,omitempty"`
	TitleSource string    `json:"title_source,omitempty"`
	Transcripts []string  `json:"transcripts"`
	Link        string    `json:"link,omitempty"`
	FirstAt     time.Time `json:"first_at"`
	LastAt      time.Time `json:"last_at"`
	CommitCount int       `json:"commit_count"`
	Landed      int       `json:"landed"`
	// Reached counts commits per "lane/env" they reached.
	Reached map[string]int `json:"reached"`
	Repos   []RepoCommits  `json:"repos"`
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
	// MirrorUpdated is when Gitea last fetched this repo from GitHub (mirrors only).
	MirrorUpdated *time.Time `json:"mirror_updated,omitempty"`
}

// Response is GET /api/v1/lineage.
type Response struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Since        time.Time          `json:"since"`
	Days         int                `json:"days"`
	Reachability probe.Reachability `json:"reachability"`
	Threads      []Thread           `json:"threads"`
	Coverage     []RepoCoverage     `json:"coverage"`
	// Releases is the latest recorded release per lane/env (empty when no
	// release records exist yet); ReleasesError says why it is empty.
	Releases      []ReleaseHead `json:"releases"`
	ReleasesError string        `json:"releases_error,omitempty"`
	// Graph is every repo's commit graph; only with ?graph=true (it is large).
	Graph []RepoGraph `json:"graph,omitempty"`
	// MirrorSync is the mirror fetch this build asked for, if any.
	MirrorSync *MirrorSync `json:"mirror_sync,omitempty"`
	Errors     []string    `json:"errors"`
}
