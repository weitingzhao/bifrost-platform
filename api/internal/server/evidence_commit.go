package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// GET /api/v1/evidence/commit?repo=…&commit=… answers "is this commit green"
// from the evidence ledger (CI design v4 §3.3, landing wave 1 block D), and
// with what. Read-only; nothing decides by it yet.
//
// For each task the ledger has for the repository it computes the key the
// task would have for this commit now: the commit's git tree, and the tree
// of the main head of every repository the task last used beside it. The
// rows of that key give the verdict (store.EvidenceVerdict): green, red, or
// none. none is not red: nothing ran on these inputs, or only part of the
// repository was tested; the caller starts a CI run, it does not refuse.
// The commit's own rows are returned as well, whatever their key, so a
// commit that was green against an older main of a repository beside it
// shows as that.

type evidenceInput struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
}

type evidenceTask struct {
	Task     string              `json:"task"`
	Verdict  string              `json:"verdict"`
	InputKey string              `json:"input_key"`
	Inputs   json.RawMessage     `json:"inputs"`
	Beside   []evidenceInput     `json:"beside"`
	Rows     []store.EvidenceRow `json:"rows"`
}

type evidenceAnswer struct {
	Repo    string         `json:"repo"`
	Commit  string         `json:"commit"`
	Tree    string         `json:"tree"`
	Unit    string         `json:"unit"`
	Verdict string         `json:"verdict"`
	Reason  string         `json:"reason"`
	Tasks   []evidenceTask `json:"tasks"`
	// CommitRows is what was recorded for this commit, newest first, with
	// the inputs each run had then.
	CommitRows []store.EvidenceRow `json:"commit_rows"`
}

// commitEvidence reads the ledger for one commit. tree reads a commit's git
// tree and main a repository's main head; both are Gitea's.
func commitEvidence(ctx context.Context, db store.DB, tree func(context.Context, string, string) (string, error),
	main func(context.Context, string) (string, error), repo, commit string) (evidenceAnswer, error) {
	out := evidenceAnswer{Repo: repo, Commit: commit, Unit: store.EvidenceUnitRepo, Verdict: store.EvidenceNone, Tasks: []evidenceTask{}}
	var err error
	if out.Tree, err = tree(ctx, repo, commit); err != nil {
		return out, err
	}
	if out.CommitRows, err = store.EvidenceByCommit(ctx, db, repo, commit, 20); err != nil {
		return out, err
	}
	tasks, err := store.EvidenceTasks(ctx, db, repo, out.Unit)
	if err != nil {
		return out, err
	}
	if len(tasks) == 0 {
		out.Reason = "the ledger has no evidence for " + repo + " yet"
		return out, nil
	}
	heads := map[string]evidenceInput{}
	verdicts := map[string]int{}
	for _, last := range tasks {
		var doc struct {
			Beside map[string]string `json:"beside"`
		}
		if err = json.Unmarshal(last.Inputs, &doc); err != nil {
			return out, fmt.Errorf("evidence %d: inputs: %w", last.ID, err)
		}
		t := evidenceTask{Task: last.Task, Beside: []evidenceInput{}}
		var beside []store.RepoAt
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
			beside = append(beside, store.RepoAt{Repo: name, Tree: h.Tree})
		}
		sort.Slice(t.Beside, func(i, j int) bool { return t.Beside[i].Repo < t.Beside[j].Repo })
		inputs, key, err := store.EvidenceInputs(last.Task, repo, out.Tree, beside)
		if err != nil {
			return out, err
		}
		t.InputKey = key
		if t.Inputs, err = json.Marshal(inputs); err != nil {
			return out, err
		}
		if t.Rows, err = store.EvidenceByKey(ctx, db, repo, out.Unit, last.Task, key); err != nil {
			return out, err
		}
		t.Verdict = store.EvidenceVerdict(t.Rows)
		verdicts[t.Verdict]++
		out.Tasks = append(out.Tasks, t)
	}
	switch {
	case verdicts[store.EvidenceRed] > 0:
		out.Verdict, out.Reason = store.EvidenceRed, "a task failed on these inputs and none passed in full since"
	case verdicts[store.EvidenceGreen] == len(out.Tasks):
		out.Verdict, out.Reason = store.EvidenceGreen, "every task passed in full on these inputs"
	case len(out.CommitRows) > 0:
		out.Reason = "no full pass on these inputs; the commit's own rows were recorded with other inputs or a narrower scope (commit_rows)"
	default:
		out.Reason = "nothing ran on these inputs"
	}
	return out, nil
}

func (s *Server) handleEvidenceCommit(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	main := s.agentRuntimeMains()
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil || main == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	out, err := commitEvidence(ctx, s.agentRuntime.api.DB, giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second}), main, repo, commit)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, store.ErrInvalid) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
