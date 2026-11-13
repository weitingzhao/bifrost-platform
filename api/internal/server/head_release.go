package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The platform releases main's head by itself (internal/headrelease has the
// judgement and why; this file is its hands). What it reads is the source
// each time: main from Gitea, the verdict from the evidence ledger, the runs
// from the cluster. It keeps nothing of its own.
//
// HEAD_RELEASE is off (the default: nothing is started) or stg (the enabled
// rows of headrelease/deliverables.yaml are released). The route below answers
// whatever the switch says: it only reads.
const (
	headReleaseEnv       = "HEAD_RELEASE"
	headReleasePrincipal = "head-release"
	headReleasePass      = 90 * time.Second
)

func headReleaseOn() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(headReleaseEnv)), "stg")
}

// headWorld is headrelease.World on this process's Gitea access, ledger and
// delivery service.
type headWorld struct {
	head   func(ctx context.Context, repo string) (string, error)
	ci     delivery.CIReader
	db     func() store.DB
	runs   func(ctx context.Context) ([]unstructured.Unstructured, error)
	start  func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (delivery.PipelineRunView, string, error)
	listed []unstructured.Unstructured // one listing serves one pass
	have   bool
}

func (w *headWorld) Head(ctx context.Context, repo string) (string, error) { return w.head(ctx, repo) }

// CI is ledgerCI's verdict (the one the release gates read), and from the
// row of evidence that verdict rests on, the commit of each repository that
// CI run had beside it.
func (w *headWorld) CI(ctx context.Context, repo, commit string) (string, string, map[string]string, error) {
	status, run, err := w.ci(ctx, repo, commit)
	if err != nil || status != delivery.CISucceeded {
		return status, run, nil, err
	}
	d := w.db()
	if d == nil {
		return "", "", nil, errors.New("the evidence ledger is not connected")
	}
	rows, err := store.EvidenceCIByCommit(ctx, d, repo, commit, 50)
	if err != nil {
		return "", "", nil, err
	}
	// Only the commit's own row is read. A verdict that came by the commit's
	// tree rests on another commit's CI run, and what that run had beside it
	// can be older than what the environment runs: taking it would release
	// the siblings backwards (independent review 2026-11-13, finding 3).
	// Such a head has no beside here and is left to a person.
	beside, _ := besideOf(rows, repo, run)
	return status, run, beside, nil
}

// besideOf reads, from the row of the run, the commits its statement names
// for the other repositories.
func besideOf(rows []store.EvidenceRow, repo, run string) (map[string]string, bool) {
	for _, row := range rows {
		if row.RunName != run {
			continue
		}
		var st struct {
			Subject []struct {
				Name   string `json:"name"`
				Digest struct {
					Commit string `json:"gitCommit"`
				} `json:"digest"`
			} `json:"subject"`
		}
		if err := json.Unmarshal(row.Statement, &st); err != nil {
			return nil, true
		}
		out := map[string]string{}
		for _, s := range st.Subject {
			if s.Name != repo && commitID.MatchString(s.Digest.Commit) {
				out[s.Name] = s.Digest.Commit
			}
		}
		return out, true
	}
	return nil, false
}

// Runs lists the runs of the pipeline the cluster keeps, newest first.
func (w *headWorld) Runs(ctx context.Context, pipeline string) ([]headrelease.Run, error) {
	if !w.have {
		items, err := w.runs(ctx)
		if err != nil {
			return nil, err
		}
		w.listed, w.have = items, true
	}
	return headRuns(w.listed, pipeline), nil
}

func headRuns(items []unstructured.Unstructured, pipeline string) []headrelease.Run {
	var picked []unstructured.Unstructured
	for _, item := range items {
		if ref, _, _ := unstructured.NestedString(item.Object, "spec", "pipelineRef", "name"); ref == pipeline {
			picked = append(picked, item)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		a, b := picked[i].GetCreationTimestamp().Time, picked[j].GetCreationTimestamp().Time
		if !a.Equal(b) {
			return a.After(b)
		}
		return picked[i].GetName() > picked[j].GetName()
	})
	out := make([]headrelease.Run, 0, len(picked))
	for _, item := range picked {
		r := headrelease.Run{Name: item.GetName(), Params: map[string]string{}, Status: headrelease.RunRunning}
		params, _, _ := unstructured.NestedSlice(item.Object, "spec", "params")
		for _, value := range params {
			p, _ := value.(map[string]any)
			name, _ := p["name"].(string)
			if v, ok := p["value"].(string); ok && name != "" {
				r.Params[name] = v
			}
		}
		// A run with no verdict yet (just created, or its condition is
		// Unknown) is running.
		conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		for _, value := range conditions {
			c, _ := value.(map[string]any)
			if c["type"] != "Succeeded" {
				continue
			}
			switch c["status"] {
			case "True":
				r.Status = headrelease.RunSucceeded
			case "False":
				r.Status = headrelease.RunFailed
			}
		}
		out = append(out, r)
	}
	return out
}

// Start starts the delivery unless a release window is held. The parameter
// named revision is the pipeline's revision; the rest go beside it.
func (w *headWorld) Start(ctx context.Context, pipeline string, params map[string]string) (string, bool, error) {
	extra := map[string]string{}
	for k, v := range params {
		if k != "revision" {
			extra[k] = v
		}
	}
	run, heldBy, err := w.start(ctx, headReleasePrincipal, pipeline, params["revision"], extra)
	if err != nil {
		return "", false, err
	}
	w.have = false // the listing is older than this run
	return run.Name, heldBy != "", nil
}

// headWorldOf builds the world of one pass, or nil when this process has no
// delivery service or no ledger.
func (s *Server) headWorldOf() *headWorld {
	if s.delivery == nil || s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		return nil
	}
	db := func() store.DB { return s.agentRuntime.api.DB }
	tree := giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})
	return &headWorld{
		head:  s.agentRuntimeMains(),
		ci:    ledgerCI(db, tree),
		db:    db,
		runs:  s.delivery.PipelineRunObservations,
		start: s.delivery.StartDeliveryIfFree,
	}
}

// headReleaseSource is the loop's pass, or nil when the switch is off.
func (s *Server) headReleaseSource() func(context.Context) error {
	if s.delivery == nil || !headReleaseOn() {
		return nil
	}
	table, path, err := headrelease.LoadTable()
	if err != nil {
		slog.Error("head release: the table does not load; nothing is released", "path", path, "error", err)
		return nil
	}
	slog.Info("head release: on", "path", path, "deliverables", len(table.Deliverables))
	var mu sync.Mutex
	last := map[string]string{} // the last outcome said, by deliverable
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, headReleasePass)
		defer cancel()
		var errs []error
		for _, d := range table.Deliverables {
			if !d.Enabled {
				continue
			}
			w := s.headWorldOf()
			if w == nil {
				return nil
			}
			out, err := headrelease.Step(ctx, w, d)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			said := out.Action + "|" + out.Why + "|" + out.Run + "|" + fmt.Sprint(out.Commits) + "|" + fmt.Sprint(out.Evidence)
			mu.Lock()
			changed := last[d.Name] != said
			last[d.Name] = said
			mu.Unlock()
			if changed {
				s.recordHeadDecision(ctx, d, out)
			}
		}
		return errors.Join(errs...)
	}
}

// headDecision is the record of one decision of the loop (Owner 2026-11-13:
// "no card, but a trace ... auditable later, with the reason ... so it can be
// judged in hindsight"). One is written each time a deliverable's outcome
// changes, a start and a decision not to start alike: to the platform's audit
// log (GET /api/v1/audit, action head_release) and to the process log.
type headDecision struct {
	Deliverable string `json:"deliverable"`
	Action      string `json:"action"` // started | delivered | waiting | refused
	Why         string `json:"why"`
	// Commits is the set judged, by pipeline parameter: the head, and the
	// commits of the repositories its CI ran beside.
	Commits map[string]string `json:"commits,omitempty"`
	// Evidence is the CI run each head was judged green or red by.
	Evidence map[string]string `json:"evidence,omitempty"`
	Run      string            `json:"run,omitempty"`
	// Previous is what the environment ran before, and Brought what a start
	// adds to it: every commit of each head's repository since then, with
	// the thread that made it (its Claude-Session trailer).
	PreviousRun string              `json:"previous_run,omitempty"`
	Previous    map[string]string   `json:"previous,omitempty"`
	Brought     []headBroughtCommit `json:"brought,omitempty"`
	// BroughtNote says why Brought is not the whole list, when it is not.
	BroughtNote string `json:"brought_note,omitempty"`
}

type headBroughtCommit struct {
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Session string `json:"session,omitempty"`
	Work    string `json:"work,omitempty"`
}

const headBroughtMax = 50

func (s *Server) recordHeadDecision(ctx context.Context, d headrelease.Deliverable, out headrelease.Outcome) {
	rec := headDecision{Deliverable: out.Deliverable, Action: out.Action, Why: out.Why, Commits: out.Commits,
		Evidence: out.Evidence, Run: out.Run, PreviousRun: out.PreviousRun, Previous: out.Previous}
	if out.Action == headrelease.Started {
		for _, c := range d.Commits {
			if c.TestedWith != "" {
				continue
			}
			brought, note := s.headBrought(ctx, c.Repo, out.Previous[c.Param], out.Commits[c.Param])
			rec.Brought = append(rec.Brought, brought...)
			if note != "" {
				rec.BroughtNote = strings.TrimSpace(rec.BroughtNote + " " + c.Repo + ": " + note)
			}
		}
	}
	detail, err := json.Marshal(rec)
	if err != nil {
		detail = []byte(out.Action + ": " + out.Why)
	}
	slog.Info("head release", "deliverable", out.Deliverable, "action", out.Action, "why", out.Why, "run", out.Run, "record", string(detail))
	s.audit.RecordDirect(headReleasePrincipal, actuation.RoleOperator, "head_release", "deliverable/"+out.Deliverable, out.Action, string(detail))
}

// headBrought lists the commits to has and from lacks, newest first, each
// with its lineage trailers, from Gitea. What it cannot read it says in note;
// the record is written all the same.
func (s *Server) headBrought(ctx context.Context, repo, from, to string) ([]headBroughtCommit, string) {
	if !commitID.MatchString(from) || !commitID.MatchString(to) {
		return nil, "no earlier delivery to compare with"
	}
	if from == to {
		return nil, ""
	}
	acc, err := s.delivery.GiteaAccess(ctx)
	if err != nil {
		return nil, "gitea access: " + err.Error()
	}
	q := url.Values{"sha": {to}, "not": {from}, "stat": {"false"}, "verification": {"false"}, "files": {"false"}, "limit": {strconv.Itoa(headBroughtMax)}}
	endpoint := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(acc.Org) + "/" + url.PathEscape(repo) + "/commits?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err.Error()
	}
	if acc.User != "" || acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, "gitea: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Sprintf("gitea: HTTP %d", resp.StatusCode)
	}
	var listed []headListed
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&listed); err != nil {
		return nil, "gitea: " + err.Error()
	}
	out, note := headBroughtOf(repo, listed), ""
	if total, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Total-Count"))); total > len(out) {
		note = fmt.Sprintf("the newest %d of %d commits", len(out), total)
	}
	return out, note
}

// headListed is one commit of Gitea's commit list.
type headListed struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

var headTrailer = regexp.MustCompile(`(?m)^(Claude-Session|Work):[ \t]*(\S.*?)[ \t]*$`)

// headBroughtOf reads each commit's subject and its Claude-Session and Work
// trailers (the first of each, as lineage.sh keeps the original).
func headBroughtOf(repo string, listed []headListed) []headBroughtCommit {
	out := make([]headBroughtCommit, 0, len(listed))
	for _, c := range listed {
		msg := strings.ReplaceAll(c.Commit.Message, "\r\n", "\n")
		b := headBroughtCommit{Repo: repo, Commit: c.SHA, Subject: strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])}
		for _, m := range headTrailer.FindAllStringSubmatch(msg, -1) {
			switch {
			case m[1] == "Claude-Session" && b.Session == "":
				b.Session = m[2]
			case m[1] == "Work" && b.Work == "":
				b.Work = m[2]
			}
		}
		out = append(out, b)
	}
	return out
}

// GET /api/v1/delivery/released?repo=…&commit=… says, for every deliverable
// of headrelease/deliverables.yaml that holds the repository, what its environment
// runs and whether that has the commit: so a line asks "is my commit
// released" instead of guessing a run's name (ci-feedback 2026-11-13).
// Read-only; viewer and above.
func (s *Server) handleReleased(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name, commit a 40-character lowercase SHA"})
		return
	}
	world := s.headWorldOf()
	if world == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the delivery service or the evidence ledger is not connected"})
		return
	}
	table, _, err := headrelease.LoadTable()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	facts := s.delivery.PolicyFacts()
	within := func(ctx context.Context, repo, ancestor, commit string) (bool, error) {
		if ancestor == commit {
			return true, nil
		}
		// ChangedFiles fails when ancestor is not in commit's history, and
		// when it cannot tell (the mirror is down, the walk is too long):
		// neither is "released".
		_, diffErr := facts.ChangedFiles(ctx, repo, ancestor, commit)
		return diffErr == nil, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	released, err := headrelease.WhereReleased(ctx, world, table, repo, commit, within)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if released == nil {
		released = []headrelease.Released{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": repo, "commit": commit, "head_release": headReleaseOn(), "released": released})
}
