package headrelease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The platform releases main's head by itself (headrelease.go has the
// judgement and why; this file is its hands). What it reads is the source
// each time: main from Gitea, the verdict from the evidence ledger, the runs
// from the cluster. It keeps nothing of its own.
//
// HEAD_RELEASE is off (the default: nothing is started) or stg (the enabled
// rows of headrelease/deliverables.yaml are released). The route below answers
// whatever the switch says: it only reads.
const (
	Env         = "HEAD_RELEASE"
	Principal   = "head-release"
	PassTimeout = 90 * time.Second
)

func On() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(Env)), "stg")
}

// Live is World on this process's Gitea access, ledger and delivery service:
// what it reads is the source each time, and it keeps nothing of its own.
type Live struct {
	head func(ctx context.Context, repo string) (string, error)
	// ci is the ledger's verdict and, from the row that verdict rests on, the
	// commit of each repository that CI run had beside it.
	ci func(ctx context.Context, repo, commit string) (status, run string, beside map[string]string, err error)
	// runs is one pipeline's runs: the bus run cache when it is synced,
	// else the cluster's list of that pipeline.
	runs func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	// start starts the delivery: the run's name, or who holds what it needs.
	start func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (run, heldBy string, err error)
	// frozen reports a release freeze someone set, and its reason.
	frozen func(ctx context.Context) (bool, string)
	// approved names a release the Owner approved that has not run yet
	// ("" when there is none).
	approved func() (string, error)
}

// LiveDeps is what a Live stands on.
type LiveDeps struct {
	Head     func(ctx context.Context, repo string) (string, error)
	CI       func(ctx context.Context, repo, commit string) (status, run string, beside map[string]string, err error)
	Runs     func(ctx context.Context, pipeline string) ([]unstructured.Unstructured, error)
	Start    func(ctx context.Context, principal, pipeline, revision string, params map[string]string) (run, heldBy string, err error)
	Frozen   func(ctx context.Context) (bool, string)
	Approved func() (string, error)
}

// NewLive is the world of one pass.
func NewLive(d LiveDeps) *Live {
	return &Live{head: d.Head, ci: d.CI, runs: d.Runs, start: d.Start, frozen: d.Frozen, approved: d.Approved}
}

func (w *Live) Head(ctx context.Context, repo string) (string, error) { return w.head(ctx, repo) }

// CI is the ledger's verdict (the one the release gates read), and from the
// row of evidence that verdict rests on, the commit of each repository that
// CI run had beside it.
func (w *Live) CI(ctx context.Context, repo, commit string) (string, string, map[string]string, error) {
	return w.ci(ctx, repo, commit)
}

// Runs lists the runs of the pipeline the cluster keeps, newest first.
func (w *Live) Runs(ctx context.Context, pipeline string) ([]Run, error) {
	// Every call lists again: a step reads the runs a second time just
	// before it starts, and that reading must be the cluster's, not this
	// pass's memory of it.
	items, err := w.runs(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	return headRuns(items, pipeline), nil
}

func headRuns(items []unstructured.Unstructured, pipeline string) []Run {
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
	out := make([]Run, 0, len(picked))
	for _, item := range picked {
		r := Run{Name: item.GetName(), Params: map[string]string{}, Status: RunRunning}
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
				r.Status = RunSucceeded
			case "False":
				r.Status = RunFailed
			}
		}
		out = append(out, r)
	}
	return out
}

// Start starts the delivery, or says why not now: releases are frozen (the
// pipeline's freeze-check would fail the run, and a failed run of a head is
// not tried again: Codex review 2026-11-13, B3), a release window is held, or
// a run of the pipeline is under way. The parameter named revision is the
// pipeline's revision; the rest go beside it.
func (w *Live) Start(ctx context.Context, pipeline string, params map[string]string) (string, string, error) {
	if w.frozen != nil {
		if frozen, reason := w.frozen(ctx); frozen {
			return "", "releases are frozen: " + reason, nil
		}
	}
	// An approved release goes first. It waits for the window and tries
	// again at intervals longer than this loop's pass, so the loop would
	// take the window from under it each time main moved (#524, 2026-11-14:
	// an approved PROD release found the loop's STG run on the window).
	if w.approved != nil {
		waiting, err := w.approved()
		if err != nil {
			return "", "", err
		}
		if waiting != "" {
			return "", "an approved release goes first: " + waiting, nil
		}
	}
	extra := map[string]string{}
	for k, v := range params {
		if k != "revision" {
			extra[k] = v
		}
	}
	run, heldBy, err := w.start(ctx, Principal, pipeline, params["revision"], extra)
	if err != nil {
		return "", "", err
	}
	if heldBy != "" {
		return "", "not started: " + heldBy, nil
	}
	return run, "", nil
}

// DecisionRecord is the record of one decision of the loop (Owner 2026-11-13:
// "no card, but a trace ... auditable later, with the reason ... so it can be
// judged in hindsight"). One is written each time a deliverable's outcome
// changes, a start and a decision not to start alike: to the platform's audit
// log (GET /api/v1/audit, action head_release) and to the process log.
type DecisionRecord struct {
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
	PreviousRun string            `json:"previous_run,omitempty"`
	Previous    map[string]string `json:"previous,omitempty"`
	Brought     []BroughtCommit   `json:"brought,omitempty"`
	// BroughtNote says why Brought is not the whole list, when it is not.
	BroughtNote string `json:"brought_note,omitempty"`
}

type BroughtCommit struct {
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Session string `json:"session,omitempty"`
	Work    string `json:"work,omitempty"`
}

const broughtMax = 50

// RecordOf is the record of one outcome: for a start, the commits it brings
// (brought lists what to has and from lacks, with each commit's thread).
func RecordOf(ctx context.Context, d Deliverable, out Outcome, brought func(ctx context.Context, repo, from, to string) ([]BroughtCommit, int, string)) DecisionRecord {
	rec := DecisionRecord{Deliverable: out.Deliverable, Action: out.Action, Why: out.Why, Commits: out.Commits,
		Evidence: out.Evidence, Run: out.Run, PreviousRun: out.PreviousRun, Previous: out.Previous}
	if out.Action == Started {
		for _, c := range d.Commits {
			if c.TestedWith != "" {
				continue
			}
			b, _, note := brought(ctx, c.Repo, out.Previous[c.Param], out.Commits[c.Param])
			rec.Brought = append(rec.Brought, b...)
			if note != "" {
				rec.BroughtNote = strings.TrimSpace(rec.BroughtNote + " " + c.Repo + ": " + note)
			}
		}
	}
	return rec
}

// Brought lists the commits to has and from lacks, newest first, each
// with its lineage trailers, from Gitea: one request. total is how many there
// are, which can be more than it lists; -1 when it could not be read. What it
// cannot read it says in note; the record is written all the same.
func Brought(ctx context.Context, access func(context.Context) (gitread.Access, error), repo, from, to string) (brought []BroughtCommit, total int, note string) {
	if !gitread.CommitID.MatchString(from) || !gitread.CommitID.MatchString(to) {
		return nil, -1, "no earlier delivery to compare with"
	}
	if from == to {
		return nil, 0, ""
	}
	acc, err := access(ctx)
	if err != nil {
		return nil, -1, "gitea access: " + err.Error()
	}
	q := url.Values{"sha": {to}, "not": {from}, "stat": {"false"}, "verification": {"false"}, "files": {"false"}, "limit": {strconv.Itoa(broughtMax)}}
	endpoint := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(acc.Org) + "/" + url.PathEscape(repo) + "/commits?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, -1, err.Error()
	}
	if acc.User != "" || acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, -1, "gitea: " + err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, -1, fmt.Sprintf("gitea: HTTP %d", resp.StatusCode)
	}
	var listed []listed
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&listed); err != nil {
		return nil, -1, "gitea: " + err.Error()
	}
	out := broughtOf(repo, listed)
	total = len(out)
	if counted, _ := strconv.Atoi(strings.TrimSpace(resp.Header.Get("X-Total-Count"))); counted > len(out) {
		total, note = counted, fmt.Sprintf("the newest %d of %d commits", len(out), counted)
	}
	return out, total, note
}

// listed is one commit of Gitea's commit list.
type listed struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

var trailer = regexp.MustCompile(`(?m)^(Claude-Session|Work):[ \t]*(\S.*?)[ \t]*$`)

// broughtOf reads each commit's subject and its Claude-Session and Work
// trailers (the first of each, as lineage.sh keeps the original).
func broughtOf(repo string, listed []listed) []BroughtCommit {
	out := make([]BroughtCommit, 0, len(listed))
	for _, c := range listed {
		msg := strings.ReplaceAll(c.Commit.Message, "\r\n", "\n")
		b := BroughtCommit{Repo: repo, Commit: c.SHA, Subject: strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])}
		for _, m := range trailer.FindAllStringSubmatch(msg, -1) {
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

// PassDeps is what the loop's pass stands on.
type PassDeps struct {
	Table     Table
	Releasing bool
	// WorldOf builds the world of one pass, or nil when this process has no
	// delivery service or no ledger.
	WorldOf func() *Live
	// Record says an outcome where it outlives the process: the log, the
	// audit log and the action trace.
	Record func(ctx context.Context, d Deliverable, out Outcome)
	// Shadow is the shadow of PROD by batch; nil unless it is on. It records
	// and does nothing else.
	Shadow *ProdShadowLoop
}

// Pass is the loop's pass.
func Pass(deps PassDeps) func(context.Context) error {
	table, releasing := deps.Table, deps.Releasing
	shadow := deps.Shadow
	var mu sync.Mutex
	last := map[string]string{} // the last outcome said, by deliverable
	rows := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, PassTimeout)
		defer cancel()
		var errs []error
		for _, d := range table.Deliverables {
			if !d.Enabled || !releasing {
				continue
			}
			w := deps.WorldOf()
			if w == nil {
				return nil
			}
			out, err := Step(ctx, w, d)
			if err != nil {
				// What it could not read or do is a decision not to release
				// as well, and is recorded like one (Codex review, C1).
				errs = append(errs, err)
				out.Deliverable, out.Action, out.Why = d.Name, "error", err.Error()
			}
			said := out.Action + "|" + out.Why + "|" + out.Run + "|" + fmt.Sprint(out.Commits) + "|" + fmt.Sprint(out.Evidence)
			mu.Lock()
			changed := last[d.Name] != said
			last[d.Name] = said
			mu.Unlock()
			if changed {
				deps.Record(ctx, d, out)
			}
		}
		return errors.Join(errs...)
	}
	return func(ctx context.Context) error {
		err := rows(ctx)
		// After the rows, whatever they did, and on its own time: the shadow
		// neither delays a delivery nor fails the pass.
		if shadow != nil {
			sctx, cancel := context.WithTimeout(ctx, ProdShadowPassTimeout)
			shadow.Pass(sctx)
			cancel()
		}
		return err
	}
}
