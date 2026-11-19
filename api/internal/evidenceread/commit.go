package evidenceread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
)

// GET /api/v1/evidence/commit?repo=…&commit=… answers "is this commit green"
// from the evidence ledger (CI design v4 §3.3, landing wave 1 block D), and
// with what. Read-only; nothing decides by it yet.
//
// For each task the ledger has for the repository it computes the key the
// task would have for this commit now: the commit's git tree, and the tree
// of the main head of every repository the task last used beside it. The
// rows of that key give the verdict (evidence.EvidenceVerdict): green, red, or
// none. none is not red: nothing ran on these inputs, or only part of the
// repository was tested; the caller starts a CI run, it does not refuse.
// The commit's own rows are returned as well, whatever their key, so a
// commit that was green against an older main of a repository beside it
// shows as that.

// CommitInput is one repository at one commit and its tree.
type CommitInput struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

// CommitTask is the verdict for one task of the repository.
type CommitTask struct {
	Task     string                 `json:"task"`
	Verdict  string                 `json:"verdict"`
	InputKey string                 `json:"input_key"`
	Inputs   json.RawMessage        `json:"inputs"`
	Beside   []CommitInput          `json:"beside"`
	Rows     []evidence.EvidenceRow `json:"rows"`
}

// CommitAnswer is the answer of GET /api/v1/evidence/commit.
type CommitAnswer struct {
	Repo    string       `json:"repo"`
	Commit  string       `json:"commit"`
	Tree    string       `json:"tree"`
	Unit    string       `json:"unit"`
	Verdict string       `json:"verdict"`
	Reason  string       `json:"reason"`
	Tasks   []CommitTask `json:"tasks"`
	// CommitRows is what was recorded for this commit, newest first, with
	// the inputs each run had then.
	CommitRows []evidence.EvidenceRow `json:"commit_rows"`
}

// ForCommit reads the ledger for one commit. tree reads a commit's git
// tree and main a repository's main head; both are Gitea's.
func ForCommit(ctx context.Context, db eventlog.DB, tree func(context.Context, string, string) (string, error),
	main func(context.Context, string) (string, error), repo, commit string) (CommitAnswer, error) {
	out := CommitAnswer{Repo: repo, Commit: commit, Unit: evidence.EvidenceUnitRepo, Verdict: evidence.EvidenceNone, Tasks: []CommitTask{}}
	var err error
	if out.Tree, err = tree(ctx, repo, commit); err != nil {
		return out, err
	}
	if out.CommitRows, err = evidence.EvidenceByCommit(ctx, db, repo, commit, 20); err != nil {
		return out, err
	}
	tasks, err := evidence.EvidenceTasks(ctx, db, repo, out.Unit)
	if err != nil {
		return out, err
	}
	if len(tasks) == 0 {
		out.Reason = "the ledger has no evidence for " + repo + " yet"
		return out, nil
	}
	heads := map[string]CommitInput{}
	verdicts := map[string]int{}
	for _, last := range tasks {
		var doc struct {
			Beside map[string]string `json:"beside"`
		}
		if err = json.Unmarshal(last.Inputs, &doc); err != nil {
			return out, fmt.Errorf("evidence %d: inputs: %w", last.ID, err)
		}
		t := CommitTask{Task: last.Task, Beside: []CommitInput{}}
		var beside []evidence.RepoAt
		for name := range doc.Beside {
			h, ok := heads[name]
			if !ok {
				h.Repo = name
				if h.Commit, err = main(ctx, name); err != nil {
					return out, err
				}
				if h.Tree, err = tree(ctx, name, h.Commit); err != nil {
					return out, err
				}
				heads[name] = h
			}
			t.Beside = append(t.Beside, h)
			beside = append(beside, evidence.RepoAt{Repo: name, Tree: h.Tree})
		}
		sort.Slice(t.Beside, func(i, j int) bool { return t.Beside[i].Repo < t.Beside[j].Repo })
		inputs, key, err := evidence.EvidenceInputs(last.Task, repo, out.Tree, beside)
		if err != nil {
			return out, err
		}
		t.InputKey = key
		if t.Inputs, err = json.Marshal(inputs); err != nil {
			return out, err
		}
		if t.Rows, err = evidence.EvidenceByKey(ctx, db, repo, out.Unit, last.Task, key); err != nil {
			return out, err
		}
		t.Verdict = evidence.EvidenceVerdict(t.Rows)
		verdicts[t.Verdict]++
		out.Tasks = append(out.Tasks, t)
	}
	switch {
	case verdicts[evidence.EvidenceRed] > 0:
		out.Verdict, out.Reason = evidence.EvidenceRed, "a task failed on these inputs and none passed in full since"
	case verdicts[evidence.EvidenceGreen] == len(out.Tasks):
		out.Verdict, out.Reason = evidence.EvidenceGreen, "every task passed in full on these inputs"
	case len(out.CommitRows) > 0:
		out.Reason = "no full pass on these inputs; the commit's own rows were recorded with other inputs or a narrower scope (commit_rows)"
	default:
		out.Reason = "nothing ran on these inputs"
	}
	return out, nil
}

// CommitSources is what one request of the route reads.
type CommitSources struct {
	DB eventlog.DB
	// Tree reads a commit's git tree and Main a repository's main head; both
	// are Gitea's.
	Tree func(context.Context, string, string) (string, error)
	Main func(context.Context, string) (string, error)
}

// CommitRoute is GET /api/v1/evidence/commit: the checks of a request's
// names are the caller's, and Sources says, per request, what to read (ok
// false: the evidence ledger is not available on this platform).
type CommitRoute struct {
	RepoOK, CommitOK func(string) bool
	Sources          func() (CommitSources, bool)
}

func (c CommitRoute) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (c CommitRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !c.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !c.CommitOK(commit) {
		c.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	src, ok := c.Sources()
	if !ok {
		c.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out, err := ForCommit(ctx, src.DB, src.Tree, src.Main, repo, commit)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, eventlog.ErrInvalid) {
			status = http.StatusBadRequest
		}
		c.writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	c.writeJSON(w, http.StatusOK, out)
}
